import { randomUUID } from './uuid';
import { useSyncExternalStore } from 'react';
import type { Client } from './api/transport';
import type { FunctionDefinition, FunctionInput } from './api/m4';
import { uncertain } from './m4-state';
import { withCredential } from './credential-requests';

export type FunctionDraft = {
  input: FunctionInput;
  labelsText?: string;
  expectedVersion?: number;
  requestID: string;
  pending?: boolean;
  unknown?: boolean;
  error?: Error;
  saved?: FunctionDefinition;
};
type Store = { values: Map<string, FunctionDraft>; listeners: Set<() => void> };
const stores = new WeakMap<Client, Store>();
function store(client: Client) {
  let value = stores.get(client);
  if (!value) {
    value = { values: new Map(), listeners: new Set() };
    stores.set(client, value);
  }
  return value;
}
export function functionInput(item?: FunctionDefinition): FunctionInput {
  return {
    name: item?.name ?? '',
    description: item?.description ?? '',
    image: item?.image ?? '',
    status: item?.status ?? 'active',
    command: item?.command ?? [],
    args: item?.args ?? [],
    timeout_seconds: item?.timeout_seconds ?? 60,
    retry_limit: item?.retry_limit ?? 0,
    execution_policy_version: item?.execution_policy_version ?? (item ? 0 : 1),
    retry_safe: item?.retry_safe ?? false,
    resources: item?.resources,
    history_success: item?.history_success ?? 3,
    history_failed: item?.history_failed ?? 3,
    labels: item?.labels ?? {},
  };
}
export function getFunctionDraft(client: Client, key: string): FunctionDraft {
  const value = store(client);
  let draft = value.values.get(key);
  if (!draft) {
    draft = { input: functionInput(), requestID: randomUUID() };
    value.values.set(key, draft);
  }
  return draft;
}
export function setFunctionDraft(client: Client, key: string, draft: FunctionDraft) {
  const value = store(client);
  value.values.set(key, draft);
  value.listeners.forEach((listener) => listener());
}
export function useFunctionDraft(client: Client, key: string) {
  const value = store(client);
  return useSyncExternalStore(
    (listener) => {
      value.listeners.add(listener);
      return () => value.listeners.delete(listener);
    },
    () => getFunctionDraft(client, key),
  );
}
export async function saveFunctionDraft(client: Client, key: string) {
  let draft = getFunctionDraft(client, key);
  if (draft.pending) return;
  draft = {
    ...draft,
    input: { ...draft.input, name: draft.input.name.trim(), image: draft.input.image.trim() },
  };
  setFunctionDraft(client, key, { ...draft, pending: true, error: undefined });
  try {
    const saved =
      key === 'new'
        ? await withCredential(client, () => client.createFunction(draft.input, draft.requestID))
        : await withCredential(client, () =>
            client.updateFunction(key, {
              ...draft.input,
              expected_version: draft.expectedVersion!,
            }),
          );
    setFunctionDraft(client, key, {
      ...draft,
      pending: false,
      unknown: false,
      error: undefined,
      saved,
    });
    return saved;
  } catch (cause) {
    const error = cause instanceof Error ? cause : new Error('Unable to save this Function.');
    setFunctionDraft(client, key, {
      ...draft,
      pending: false,
      unknown: draft.unknown || uncertain(error),
      error,
    });
  }
}
export async function reconcileFunctionDraft(client: Client, key: string) {
  const draft = getFunctionDraft(client, key);
  if (draft.pending || key === 'new') return;
  setFunctionDraft(client, key, { ...draft, pending: true, error: undefined });
  try {
    const current = await withCredential(client, () => client.functionDefinition(key));
    const matches =
      current.version === (draft.expectedVersion ?? 0) + 1 &&
      Object.entries(draft.input).every(
        ([field, value]) =>
          canonical(current[field as keyof FunctionDefinition]) === canonical(value),
      );
    setFunctionDraft(client, key, {
      ...draft,
      pending: false,
      unknown: !matches,
      ...(matches
        ? { saved: current, error: undefined }
        : {
            error: new Error(
              'The stored configuration does not confirm this save. Review the current definition before making another change.',
            ),
          }),
    });
    if (matches) return current;
  } catch (cause) {
    setFunctionDraft(client, key, {
      ...draft,
      pending: false,
      error: cause instanceof Error ? cause : new Error('Unable to check the stored definition.'),
    });
  }
}

function canonical(value: unknown): string {
  if (Array.isArray(value)) return JSON.stringify(value.map(canonical));
  if (value && typeof value === 'object')
    return JSON.stringify(
      Object.entries(value)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, entry]) => [key, canonical(entry)]),
    );
  return JSON.stringify(value);
}
