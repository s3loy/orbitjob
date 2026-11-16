import { useSyncExternalStore } from 'react';
import { APIError } from './api/transport';
import type { AdministrationClient } from './api/administration';
import type { components } from './api/schema';

type Input = components['schemas']['CreateAPIKeyRequest'];
export type AdministrationCredential = {
  stage: 'idle' | 'pending' | 'revealed' | 'unknown' | 'rejected' | 'closed';
  tenant: string;
  key?: string;
  id?: string;
  prefix?: string;
  name?: string;
  error?: Error;
};
type Store = { snapshot: AdministrationCredential; listeners: Set<() => void> };
const stores = new WeakMap<object, Store>();
const closed = new WeakSet<object>();
const rejectedCredentials = new WeakMap<object, APIError>();
const idle: AdministrationCredential = Object.freeze({ stage: 'idle', tenant: '' });
const ended: AdministrationCredential = Object.freeze({ stage: 'closed', tenant: '' });
function store(client: object): Store {
  let value = stores.get(client);
  if (!value) {
    value = { snapshot: idle, listeners: new Set() };
    stores.set(client, value);
  }
  return value;
}
function publish(client: object, snapshot: AdministrationCredential) {
  if (closed.has(client)) return;
  const value = store(client);
  value.snapshot = Object.freeze(snapshot);
  value.listeners.forEach((listener) => listener());
}
export function administrationCredential(client: object | null): AdministrationCredential {
  return !client ? idle : closed.has(client) ? ended : store(client).snapshot;
}
export function subscribeAdministration(client: object | null, listener: () => void) {
  if (!client || closed.has(client)) return () => {};
  const value = store(client);
  value.listeners.add(listener);
  return () => {
    value.listeners.delete(listener);
  };
}
export function useAdministrationCredential(client: object | null) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationCredential(client),
    () => idle,
  );
}
export function administrationAuthenticationError(client: object | null) {
  return client && !closed.has(client) ? rejectedCredentials.get(client) : undefined;
}
export function rejectAdministrationCredential(client: object, error: Error) {
  if (
    closed.has(client) ||
    rejectedCredentials.has(client) ||
    !(error instanceof APIError) ||
    error.status !== 401
  )
    return;
  rejectedCredentials.set(client, error);
  store(client).listeners.forEach((listener) => listener());
}
export function useAdministrationAuthenticationError(client: object | null) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationAuthenticationError(client),
    () => undefined,
  );
}

// A single connection owns one pending or undiscarded key. This operation is
// deliberately outside query/mutation caches and never returns a credential.
export async function createAdministrationKey(
  client: AdministrationClient,
  tenant: string,
  input: Input,
): Promise<void> {
  const current = administrationCredential(client);
  if (
    administrationAuthenticationError(client) ||
    ['pending', 'revealed', 'unknown', 'closed'].includes(current.stage)
  )
    return;
  publish(client, { stage: 'pending', tenant });
  try {
    const result = await client.adminCreateKey(tenant, input);
    publish(client, {
      stage: 'revealed',
      tenant,
      key: result.key,
      id: result.id,
      prefix: result.key_prefix,
      name: result.name,
    });
  } catch (reason) {
    const error = reason instanceof Error ? reason : new Error('Key creation failed.');
    rejectAdministrationCredential(client, error);
    const unknown =
      !(error instanceof APIError) ||
      error.status === 0 ||
      error.status >= 500 ||
      error.code === 'INVALID_RESPONSE';
    publish(client, { stage: unknown ? 'unknown' : 'rejected', tenant, error });
  }
}
export function discardAdministrationKey(client: object) {
  const current = administrationCredential(client);
  if (current.stage === 'revealed' || current.stage === 'rejected') publish(client, idle);
}
export function reconcileAdministrationKey(client: object) {
  if (administrationCredential(client).stage === 'unknown') publish(client, idle);
}

// Root must call this before disposing/replacing a connection. Marking closed
// prevents even a late successful response from restoring a disconnected key.
export function clearAdministrationSession(client: object | null) {
  if (!client) return;
  closed.add(client);
  creations.delete(client);
  keyDrafts.delete(client);
  recordDrafts.delete(client);
  mutations.delete(client);
  pendingMutations.delete(client);
  rejectedCredentials.delete(client);
  const value = stores.get(client);
  if (value) {
    value.snapshot = ended;
    value.listeners.forEach((listener) => listener());
    value.listeners.clear();
    stores.delete(client);
  }
}
export async function copyAdministrationKey(
  key: string,
  clipboard?: Pick<Clipboard, 'writeText'>,
): Promise<string> {
  if (clipboard) {
    try {
      await clipboard.writeText(key);
      return 'Copied.';
    } catch {
      /* Keep the manual reveal available. */
    }
  }
  return 'Copy unavailable. Select the key above and copy manually.';
}

export type AdministrationCreationKind = 'tenants' | 'policies' | 'groups';
export type AdministrationCreation = {
  kind: AdministrationCreationKind;
  id: string;
  name: string;
  tenant?: string;
};
const emptyCreations: readonly AdministrationCreation[] = Object.freeze([]);
const creations = new WeakMap<object, readonly AdministrationCreation[]>();
export function administrationCreations(client: object | null): readonly AdministrationCreation[] {
  return !client || closed.has(client) ? emptyCreations : (creations.get(client) ?? emptyCreations);
}
export function useAdministrationCreations(client: object | null) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationCreations(client),
    () => emptyCreations,
  );
}
// Store only the non-secret identity from a confirmed create response. A list
// grant or a visible row is not required to retain this receipt.
export function recordAdministrationCreation(
  client: object,
  kind: AdministrationCreationKind,
  result: { id: string; name: string },
  tenant?: string,
) {
  if (closed.has(client)) return;
  const receipt = Object.freeze({
    kind,
    id: result.id,
    name: result.name,
    ...(tenant ? { tenant } : {}),
  });
  creations.set(client, Object.freeze([...administrationCreations(client), receipt]));
  store(client).listeners.forEach((listener) => listener());
}
export function dismissAdministrationCreation(
  client: object,
  kind: AdministrationCreationKind,
  id: string,
) {
  if (closed.has(client)) return;
  creations.set(
    client,
    Object.freeze(
      administrationCreations(client).filter((item) => item.kind !== kind || item.id !== id),
    ),
  );
  store(client).listeners.forEach((listener) => listener());
}

export type AdministrationKeyDraft = {
  name: string;
  policies: string[];
  boundary: string;
  group: string;
};
const emptyDraft: AdministrationKeyDraft = Object.freeze({
  name: '',
  policies: [],
  boundary: '',
  group: '',
});
const keyDrafts = new WeakMap<object, Map<string, AdministrationKeyDraft>>();
export function administrationKeyDraft(client: object, tenant: string): AdministrationKeyDraft {
  return closed.has(client) ? emptyDraft : (keyDrafts.get(client)?.get(tenant) ?? emptyDraft);
}
export function updateAdministrationKeyDraft(
  client: object,
  tenant: string,
  patch: Partial<AdministrationKeyDraft>,
) {
  if (closed.has(client)) return;
  const drafts = keyDrafts.get(client) ?? new Map<string, AdministrationKeyDraft>();
  drafts.set(tenant, Object.freeze({ ...administrationKeyDraft(client, tenant), ...patch }));
  keyDrafts.set(client, drafts);
  store(client).listeners.forEach((listener) => listener());
}
export function useAdministrationKeyDraft(client: object, tenant: string) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationKeyDraft(client, tenant),
    () => emptyDraft,
  );
}

export type AdministrationRecordDraft = {
  name: string;
  slug: string;
  status: string;
  description: string;
  document?: string;
  advanced: boolean;
};
type RecordPanel = AdministrationCreationKind | 'keys';
const emptyRecordDraft: AdministrationRecordDraft = Object.freeze({
  name: '',
  slug: '',
  status: 'active',
  description: '',
  advanced: false,
});
const recordDrafts = new WeakMap<object, Map<string, AdministrationRecordDraft>>();
function recordDraftScope(panel: RecordPanel, tenant: string) {
  return panel === 'tenants' ? panel : `${panel}:${tenant}`;
}
export function administrationRecordDraft(client: object, panel: RecordPanel, tenant: string) {
  return closed.has(client)
    ? emptyRecordDraft
    : (recordDrafts.get(client)?.get(recordDraftScope(panel, tenant)) ?? emptyRecordDraft);
}
export function updateAdministrationRecordDraft(
  client: object,
  panel: RecordPanel,
  tenant: string,
  patch: Partial<AdministrationRecordDraft>,
) {
  if (closed.has(client)) return;
  const drafts = recordDrafts.get(client) ?? new Map<string, AdministrationRecordDraft>();
  drafts.set(
    recordDraftScope(panel, tenant),
    Object.freeze({
      ...administrationRecordDraft(client, panel, tenant),
      ...patch,
    }),
  );
  recordDrafts.set(client, drafts);
  store(client).listeners.forEach((listener) => listener());
}
export function resetAdministrationRecordDraft(client: object, panel: RecordPanel, tenant: string) {
  if (closed.has(client)) return;
  recordDrafts.get(client)?.delete(recordDraftScope(panel, tenant));
  store(client).listeners.forEach((listener) => listener());
}
export function useAdministrationRecordDraft(client: object, panel: RecordPanel, tenant: string) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationRecordDraft(client, panel, tenant),
    () => emptyRecordDraft,
  );
}

type Mutation = {
  stage: 'idle' | 'pending' | 'succeeded' | 'rejected' | 'unknown';
  error?: Error;
};
const idleMutation: Mutation = Object.freeze({ stage: 'idle' });
const mutations = new WeakMap<object, Map<string, Mutation>>();
type PendingMutation = { scope: string; stage: 'pending' | 'unknown' };
const emptyPendingMutations: readonly PendingMutation[] = Object.freeze([]);
const pendingMutations = new WeakMap<object, readonly PendingMutation[]>();
export function administrationPendingMutations(client: object) {
  return closed.has(client)
    ? emptyPendingMutations
    : (pendingMutations.get(client) ?? emptyPendingMutations);
}
export function useAdministrationPendingMutations(client: object) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationPendingMutations(client),
    () => emptyPendingMutations,
  );
}
export function administrationMutation(client: object, panel: string): Mutation {
  return closed.has(client) ? idleMutation : (mutations.get(client)?.get(panel) ?? idleMutation);
}
function publishMutation(client: object, panel: string, value: Mutation) {
  if (closed.has(client)) return;
  const current = mutations.get(client) ?? new Map<string, Mutation>();
  current.set(panel, Object.freeze(value));
  mutations.set(client, current);
  pendingMutations.set(
    client,
    Object.freeze(
      [...current].flatMap(([scope, item]) =>
        item.stage === 'pending' || item.stage === 'unknown' ? [{ scope, stage: item.stage }] : [],
      ),
    ),
  );
  store(client).listeners.forEach((listener) => listener());
}
export function useAdministrationMutation(client: object, panel: string) {
  return useSyncExternalStore(
    (listener) => subscribeAdministration(client, listener),
    () => administrationMutation(client, panel),
    () => idleMutation,
  );
}
export async function runAdministrationMutation(
  client: object,
  panel: string,
  operation: () => Promise<void>,
): Promise<boolean> {
  if (
    closed.has(client) ||
    administrationAuthenticationError(client) ||
    ['pending', 'unknown'].includes(administrationMutation(client, panel).stage)
  )
    return false;
  publishMutation(client, panel, { stage: 'pending' });
  try {
    await operation();
    publishMutation(client, panel, { stage: 'succeeded' });
    return !closed.has(client);
  } catch (reason) {
    const error = reason instanceof Error ? reason : new Error('Request failed.');
    rejectAdministrationCredential(client, error);
    const unknown =
      !(error instanceof APIError) ||
      error.status === 0 ||
      error.status >= 500 ||
      error.code === 'INVALID_RESPONSE';
    publishMutation(client, panel, { stage: unknown ? 'unknown' : 'rejected', error });
    return false;
  }
}
export function reconcileAdministrationMutation(client: object, panel: string) {
  if (administrationMutation(client, panel).stage === 'unknown')
    publishMutation(client, panel, idleMutation);
}
export function dismissAdministrationMutationError(client: object, panel: string) {
  if (administrationMutation(client, panel).stage === 'rejected')
    publishMutation(client, panel, idleMutation);
}
export function administrationScopeLocked(client: object): boolean {
  return (
    ['pending', 'revealed', 'unknown', 'closed'].includes(administrationCredential(client).stage) ||
    [...(mutations.get(client)?.values() ?? [])].some(
      (value) => value.stage === 'pending' || value.stage === 'unknown',
    )
  );
}
