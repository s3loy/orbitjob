import { randomUUID } from './uuid';
import { useSyncExternalStore } from 'react';
import type { Definition, DefinitionKind, DefinitionResult } from './api/definitions';
import type { Client } from './api/transport';
import { APIError } from './api/transport';
import { administrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';
import { mergeDefinition, mergeResolved, type DefinitionMerge } from './definition-merge';
import { executionCommandInputError, type ExecutionCommandInput } from './execution-command';

export type DefinitionDraft = {
  namespace: string;
  name: string;
  spec: string;
  commandInput?: ExecutionCommandInput;
  loaded?: Definition;
  checked?: DefinitionResult;
  receipt?: DefinitionResult;
  requestID?: string;
  busy?: 'load' | 'refresh' | 'check' | 'apply' | 'reconcile';
  unknown?: boolean;
  error?: Error;
  message?: string;
  conflict?: Definition;
  merge?: DefinitionMerge;
};
type Store = {
  closed: boolean;
  values: Partial<Record<DefinitionKind, DefinitionDraft>>;
  listeners: Set<() => void>;
};
const stores = new WeakMap<Client, Store>();
function store(client: Client) {
  let value = stores.get(client);
  if (!value) {
    value = { closed: false, values: {}, listeners: new Set() };
    stores.set(client, value);
  }
  return value;
}
const empty: DefinitionDraft = { namespace: '', name: '', spec: '{}' };
export function getDefinitionDraft(client: Client, kind: DefinitionKind) {
  return store(client).values[kind] ?? empty;
}
export function useDefinitionDraft(client: Client, kind: DefinitionKind) {
  const state = store(client);
  return useSyncExternalStore(
    (listener) => {
      state.listeners.add(listener);
      return () => state.listeners.delete(listener);
    },
    () => state.values[kind] ?? empty,
  );
}
export function setDefinitionDraft(client: Client, kind: DefinitionKind, draft: DefinitionDraft) {
  const state = store(client);
  if (state.closed) return;
  state.values = { ...state.values, [kind]: draft };
  state.listeners.forEach((listener) => listener());
}
export function clearDefinitionSession(client: Client) {
  const state = store(client);
  state.closed = true;
  state.values = {};
  state.listeners.forEach((listener) => listener());
}
export function seedDefinitionDraft(client: Client, kind: DefinitionKind, draft: DefinitionDraft) {
  const existing = store(client).values[kind];
  if (existing && (existing.name || existing.namespace || existing.spec !== '{}')) {
    setDefinitionDraft(client, kind, {
      ...existing,
      message:
        'Your existing online draft and request identity were retained. Discard it explicitly before opening a different draft.',
    });
    return;
  }
  setDefinitionDraft(client, kind, draft);
}
export function unresolved(draft: DefinitionDraft) {
  return Boolean(draft.busy || draft.unknown);
}
export function definitionCommandInputError(draft: DefinitionDraft) {
  try {
    const template = JSON.parse(draft.spec)?.jobTemplate;
    return executionCommandInputError(draft.commandInput, {
      command: Array.isArray(template?.command) ? template.command.map(String) : [],
      args: Array.isArray(template?.args) ? template.args.map(String) : [],
    });
  } catch {
    return '';
  }
}
export function keepDraftAgainstLive(client: Client, kind: DefinitionKind) {
  const draft = getDefinitionDraft(client, kind);
  if (
    unresolved(draft) ||
    !draft.conflict ||
    !draft.loaded ||
    draft.loaded.uid !== draft.conflict.uid
  )
    return;
  try {
    const merge = mergeDefinition(draft.loaded.spec_json, draft.spec, draft.conflict.spec_json);
    setDefinitionDraft(client, kind, {
      ...draft,
      merge,
      error: undefined,
      message: 'Review the merged draft and resolve each conflicting field before checking again.',
    });
  } catch (error) {
    setDefinitionDraft(client, kind, {
      ...draft,
      error: error instanceof Error ? error : new Error('The configuration could not be compared.'),
    });
  }
}
export function chooseDefinitionMerge(
  client: Client,
  kind: DefinitionKind,
  path: string,
  choice: 'local' | 'live',
) {
  const draft = getDefinitionDraft(client, kind);
  if (
    unresolved(draft) ||
    !draft.merge ||
    !draft.merge.conflicts.some((field) => field.path === path)
  )
    return;
  const m = draft.merge;
  setDefinitionDraft(client, kind, {
    ...draft,
    merge: mergeDefinition(m.base, m.local, m.live, { ...m.choices, [path]: choice }),
  });
}
export function acceptDefinitionMerge(client: Client, kind: DefinitionKind) {
  const draft = getDefinitionDraft(client, kind);
  if (
    unresolved(draft) ||
    !draft.merge ||
    !mergeResolved(draft.merge) ||
    !draft.conflict ||
    draft.loaded?.uid !== draft.conflict.uid
  )
    return;
  setDefinitionDraft(client, kind, {
    ...draft,
    spec: draft.merge.spec,
    commandInput: undefined,
    loaded: draft.conflict,
    conflict: undefined,
    merge: undefined,
    checked: undefined,
    receipt: undefined,
    requestID: undefined,
    error: undefined,
    message: 'Merged draft ready. Check and review it before saving.',
  });
}
export async function definitionOperation(
  client: Client,
  kind: DefinitionKind,
  action: 'load' | 'refresh' | 'check' | 'apply' | 'reconcile',
) {
  if (administrationAuthenticationError(client)) return;
  const state = store(client);
  const d = state.values[kind] ?? empty;
  if (
    kind === 'jobs' &&
    (action === 'check' || action === 'apply') &&
    definitionCommandInputError(d)
  )
    return;
  if (
    d.busy ||
    (d.unknown && action !== 'reconcile') ||
    (action === 'apply' && d.error && !d.unknown)
  )
    return;
  if ((d.conflict || d.merge) && (action === 'check' || action === 'apply')) return;
  const write = (patch: Partial<DefinitionDraft>) =>
    setDefinitionDraft(client, kind, { ...d, ...patch });
  write({ busy: action, error: undefined, message: undefined });
  try {
    if (action === 'load' || action === 'refresh' || action === 'reconcile') {
      if (action === 'reconcile' && client.definitionOperation) {
        try {
          const operation = await withCredential(client, () =>
            client.definitionOperation!(kind, d.namespace, d.name, d.requestID!),
          );
          if (
            operation.request_id !== d.requestID ||
            operation.kind !== kind ||
            operation.namespace !== d.namespace ||
            operation.name !== d.name
          )
            throw new APIError(
              'The operation response has a different identity.',
              200,
              'INVALID_RESPONSE',
            );
          const result = operation.result;
          if (
            operation.state === 'accepted' &&
            result &&
            Boolean(result.uid) &&
            (result.state === 'accepted' || result.state === 'reconciled') &&
            result.request_id === d.requestID &&
            result.kind === kind &&
            result.namespace === d.namespace &&
            result.name === d.name &&
            result.manifest_digest === d.checked?.manifest_digest &&
            result.spec_json === d.checked?.spec_json &&
            (!d.loaded?.uid || result.uid === d.loaded.uid)
          ) {
            write({
              busy: undefined,
              unknown: false,
              receipt: { ...result, state: 'reconciled' },
              loaded: result,
              error: undefined,
              message: 'This save was confirmed. The live definition may have changed since then.',
            });
            return;
          }
          if (operation.state === 'rejected') {
            write({
              busy: undefined,
              unknown: false,
              checked: undefined,
              error: new APIError(
                'The original save was rejected. Refresh and review before preparing another save.',
                409,
                'CONFLICT',
              ),
              message: 'The server retained a rejection for this operation.',
            });
            return;
          }
          write({
            busy: undefined,
            unknown: true,
            message:
              'The server has not confirmed this save. Keep its request identity and check again; no write was retried.',
          });
          return;
        } catch (error) {
          // Older installations can only prove an update against its known UID.
          if (!(error instanceof APIError) || error.status !== 404 || !d.loaded?.uid) throw error;
        }
      }
      const resource = await withCredential(client, () =>
        client.definition(kind, d.namespace, d.name),
      );
      if (action === 'refresh') {
        const sameResource = Boolean(d.loaded && resource.uid === d.loaded.uid);
        const sameSpec =
          sameResource &&
          Boolean(
            resource.manifest_digest && resource.manifest_digest === d.loaded!.manifest_digest,
          );
        write({
          busy: undefined,
          merge: undefined,
          checked: undefined,
          receipt: undefined,
          requestID: undefined,
          ...(sameSpec ? { loaded: resource, conflict: undefined } : { conflict: resource }),
          error: undefined,
          message: sameSpec
            ? 'Live version refreshed. Your edits were preserved because the saved spec is unchanged. Check against the cluster again before applying.'
            : sameResource
              ? 'The live spec changed. Your edits are preserved below. Compare the live spec before choosing how to continue.'
              : 'This name now belongs to a different resource. Your draft is preserved; it cannot overwrite the replacement. Review its UID and live spec.',
        });
      } else if (action === 'reconcile') {
        const matches =
          Boolean(d.loaded?.uid && resource.uid === d.loaded.uid) &&
          resource.last_request_id === d.requestID &&
          resource.last_manifest_digest === d.checked?.manifest_digest &&
          resource.manifest_digest === d.checked?.manifest_digest;
        write({
          busy: undefined,
          conflict: undefined,
          unknown: !matches,
          ...(matches
            ? {
                receipt: { ...resource, request_id: d.requestID!, state: 'reconciled' as const },
                loaded: resource,
                error: undefined,
              }
            : {}),
          message: matches
            ? 'Write confirmed by its request ID and manifest digest.'
            : 'The current definition does not confirm this write. It may have been changed later. Keep this request identity for review; no write has been retried.',
        });
      } else {
        write({
          loaded: resource,
          conflict: undefined,
          spec: resource.spec_json,
          commandInput: undefined,
          checked: undefined,
          receipt: undefined,
          requestID: undefined,
          unknown: false,
          busy: undefined,
          error: undefined,
          message: undefined,
        });
      }
    } else {
      if (action === 'apply' && !d.checked)
        throw new Error('Check the definition against the cluster before applying.');
      const requestID = action === 'check' ? randomUUID() : d.requestID!;
      if (action === 'check')
        setDefinitionDraft(client, kind, { ...d, busy: action, requestID, error: undefined });
      const result = await withCredential(client, () =>
        client.applyDefinition(kind, d.namespace, d.name, {
          request_id: requestID,
          expected_uid: d.loaded?.uid ?? '',
          expected_resource_version: d.loaded?.resource_version ?? '',
          spec_json: action === 'apply' ? d.checked!.spec_json : d.spec,
          dry_run: action === 'check',
          ...(action === 'apply' ? { manifest_digest: d.checked!.manifest_digest } : {}),
        }),
      );
      if (
        result.request_id !== requestID ||
        result.kind !== kind ||
        result.namespace !== d.namespace ||
        result.name !== d.name ||
        (action === 'check'
          ? result.state !== 'validated'
          : (result.state !== 'accepted' && result.state !== 'reconciled') ||
            !result.uid ||
            result.manifest_digest !== d.checked?.manifest_digest ||
            (Boolean(d.loaded?.uid) && result.uid !== d.loaded?.uid))
      ) {
        throw new APIError(
          'The definition response does not confirm this request and target.',
          200,
          'INVALID_RESPONSE',
        );
      }
      write(
        action === 'check'
          ? {
              busy: undefined,
              requestID,
              checked: result,
              error: undefined,
              receipt: undefined,
              message: 'Cluster dry-run passed. No definition has been saved.',
            }
          : {
              busy: undefined,
              receipt: result,
              loaded: result,
              checked: undefined,
              spec: result.spec_json,
              commandInput: undefined,
              error: undefined,
              unknown: false,
              message:
                'Definition write accepted. Operator projection and execution are observed separately.',
            },
      );
    }
  } catch (error) {
    const e = error instanceof Error ? error : new Error('Definition request failed.');
    const unknown =
      action === 'apply' &&
      (!(e instanceof APIError) ||
        e.status === 0 ||
        e.status >= 500 ||
        e.code === 'INVALID_RESPONSE');
    write({
      busy: undefined,
      error: e,
      unknown: d.unknown || unknown,
    });
  }
}
