import { expect, test, vi } from 'vitest';
import type { AdministrationClient } from './api/administration';
import { APIError } from './api/transport';
import {
  administrationCredential,
  clearAdministrationSession,
  copyAdministrationKey,
  createAdministrationKey,
  discardAdministrationKey,
  reconcileAdministrationKey,
  subscribeAdministration,
  administrationCreations,
  recordAdministrationCreation,
  dismissAdministrationCreation,
  administrationKeyDraft,
  updateAdministrationKeyDraft,
  administrationMutation,
  runAdministrationMutation,
  reconcileAdministrationMutation,
  administrationScopeLocked,
  administrationAuthenticationError,
  rejectAdministrationCredential,
} from './administration-state';
const result = {
  id: 'key-id',
  key: 'one-time-test-secret',
  key_prefix: 'prefix',
  created_at: '2026-10-19T00:00:00Z',
};
function deferred() {
  let resolve!: (value: typeof result) => void;
  const promise = new Promise<typeof result>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function fixture() {
  const request = deferred();
  const create = vi.fn(() => request.promise);
  const client = { adminCreateKey: create } as unknown as AdministrationClient;
  return { request, create, client };
}
test('401 remains rejected across subscribers without erasing a reveal or confirmed receipt', async () => {
  const { client, request, create } = fixture();
  const pending = createAdministrationKey(client, 'tenant-a', {});
  request.resolve(result);
  await pending;
  recordAdministrationCreation(client, 'policies', { id: 'policy-a', name: 'Operator' });
  rejectAdministrationCredential(client, new APIError('List denied', 403));
  expect(administrationAuthenticationError(client)).toBeUndefined();
  const rejected = new APIError('Revoked credential', 401);
  rejectAdministrationCredential(client, rejected);
  const stop = subscribeAdministration(client, vi.fn());
  stop();
  expect(administrationAuthenticationError(client)).toBe(rejected);
  expect(administrationCredential(client).key).toBe(result.key);
  expect(administrationCreations(client)).toHaveLength(1);
  discardAdministrationKey(client);
  await createAdministrationKey(client, 'tenant-a', {});
  expect(create).toHaveBeenCalledTimes(1);
  expect(administrationAuthenticationError(client)).toBe(rejected);
  clearAdministrationSession(client);
  expect(administrationAuthenticationError(client)).toBeUndefined();
  expect(administrationAuthenticationError(fixture().client)).toBeUndefined();
});
test('a 401 read does not discard a pending key or its later confirmed one-time secret', async () => {
  const { client, request, create } = fixture();
  const pending = createAdministrationKey(client, 'tenant-a', {});
  rejectAdministrationCredential(client, new APIError('Revoked credential', 401));
  expect(administrationCredential(client).stage).toBe('pending');
  request.resolve(result);
  await pending;
  expect(administrationCredential(client)).toMatchObject({ stage: 'revealed', key: result.key });
  expect(create).toHaveBeenCalledTimes(1);
});
test('401 retains unknown outcomes and blocks new writes even after manual reconciliation', async () => {
  const client = {};
  const operation = vi.fn().mockRejectedValue(new APIError('Unknown outcome', 503));
  await runAdministrationMutation(client, 'policies:tenant-a', operation);
  const unknown = administrationMutation(client, 'policies:tenant-a');
  rejectAdministrationCredential(client, new APIError('Revoked credential', 401));
  expect(administrationMutation(client, 'policies:tenant-a')).toBe(unknown);
  expect(unknown.stage).toBe('unknown');
  reconcileAdministrationMutation(client, 'policies:tenant-a');
  await runAdministrationMutation(client, 'policies:tenant-a', operation);
  expect(operation).toHaveBeenCalledTimes(1);
});
test('pending and undiscarded result survive route subscriber unmount and remount', async () => {
  const { client, request, create } = fixture();
  const first = vi.fn();
  const unsubscribe = subscribeAdministration(client, first);
  const running = createAdministrationKey(client, 'tenant-a', {});
  expect(administrationCredential(client)).toMatchObject({ stage: 'pending', tenant: 'tenant-a' });
  unsubscribe();
  request.resolve(result);
  await running;
  expect(first).toHaveBeenCalledTimes(1);
  const returned = vi.fn();
  const stop = subscribeAdministration(client, returned);
  expect(administrationCredential(client)).toMatchObject({
    stage: 'revealed',
    tenant: 'tenant-a',
    key: result.key,
  });
  await createAdministrationKey(client, 'tenant-b', {});
  expect(create).toHaveBeenCalledTimes(1);
  discardAdministrationKey(client);
  expect(administrationCredential(client).stage).toBe('idle');
  expect(returned).toHaveBeenCalledTimes(1);
  stop();
});
test('disconnect erases reveal and permanently suppresses late response', async () => {
  const { client, request, create } = fixture();
  const running = createAdministrationKey(client, 'tenant', {});
  clearAdministrationSession(client);
  request.resolve(result);
  await running;
  expect(administrationCredential(client)).toEqual({ stage: 'closed', tenant: '' });
  await createAdministrationKey(client, 'other', {});
  expect(create).toHaveBeenCalledTimes(1);
  const second = fixture();
  const done = createAdministrationKey(second.client, 'tenant', {});
  second.request.resolve(result);
  await done;
  clearAdministrationSession(second.client);
  expect(administrationCredential(second.client)).not.toHaveProperty('key');
});
test('unknown outcome requires explicit reconciliation and does not invent a secret', async () => {
  const create = vi
    .fn()
    .mockRejectedValue(new APIError('Unreadable response', 201, 'INVALID_RESPONSE'));
  const client = { adminCreateKey: create } as unknown as AdministrationClient;
  await createAdministrationKey(client, 'tenant', {});
  expect(administrationCredential(client)).toMatchObject({ stage: 'unknown', tenant: 'tenant' });
  expect(administrationCredential(client)).not.toHaveProperty('key');
  discardAdministrationKey(client);
  await createAdministrationKey(client, 'tenant', {});
  expect(create).toHaveBeenCalledTimes(1);
  reconcileAdministrationKey(client);
  expect(administrationCredential(client).stage).toBe('idle');
});
test('pending cannot be discarded or reconciled into a duplicate request', async () => {
  const { client, request, create } = fixture();
  const running = createAdministrationKey(client, 'tenant', {});
  discardAdministrationKey(client);
  reconcileAdministrationKey(client);
  await createAdministrationKey(client, 'tenant', {});
  expect(create).toHaveBeenCalledTimes(1);
  request.resolve(result);
  await running;
});
test('copy failure preserves manual fallback and never discards state', async () => {
  const { client, request } = fixture();
  const running = createAdministrationKey(client, 'tenant', {});
  request.resolve(result);
  await running;
  expect(await copyAdministrationKey(result.key)).toContain('copy manually');
  expect(
    await copyAdministrationKey(result.key, {
      writeText: vi.fn().mockRejectedValue(new Error('denied')),
    }),
  ).toContain('copy manually');
  expect(administrationCredential(client).key).toBe(result.key);
  expect(
    await copyAdministrationKey(result.key, { writeText: vi.fn().mockResolvedValue(undefined) }),
  ).toBe('Copied.');
});

test('confirmed non-secret create identities survive panel and route unsubscribe', () => {
  const client = {};
  const stop = subscribeAdministration(client, vi.fn());
  stop();
  const response = {
    id: '00000000000000000000000001',
    name: 'Invisible new tenant',
    key: 'must-not-be-retained',
  };
  recordAdministrationCreation(client, 'tenants', response);
  expect(administrationCreations(client)).toEqual([
    { kind: 'tenants', id: response.id, name: response.name },
  ]);
  const back = subscribeAdministration(client, vi.fn());
  expect(administrationCreations(client)[0].id).toBe(response.id);
  back();
});

test('disconnect clears non-secret receipts and suppresses a delayed create completion', async () => {
  const client = {};
  recordAdministrationCreation(client, 'groups', { id: 'earlier-id', name: 'Earlier' });
  let resolve!: (value: { id: string; name: string }) => void;
  const request = new Promise<{ id: string; name: string }>((done) => {
    resolve = done;
  });
  const pending = request.then((result) =>
    recordAdministrationCreation(client, 'policies', result),
  );
  clearAdministrationSession(client);
  expect(administrationCreations(client)).toEqual([]);
  resolve({ id: 'late-id', name: 'Late' });
  await pending;
  expect(administrationCreations(client)).toEqual([]);
});

test('dismissing a non-secret receipt preserves other identities and the one-time key', async () => {
  const { client, request } = fixture();
  const pending = createAdministrationKey(client, 'tenant', {});
  request.resolve(result);
  await pending;
  recordAdministrationCreation(client, 'tenants', { id: 'tenant-id', name: 'Tenant' });
  recordAdministrationCreation(client, 'groups', { id: 'group-id', name: 'Group' });
  dismissAdministrationCreation(client, 'tenants', 'tenant-id');
  expect(administrationCreations(client)).toEqual([
    { kind: 'groups', id: 'group-id', name: 'Group' },
  ]);
  expect(administrationCredential(client)).toMatchObject({ stage: 'revealed', key: result.key });
  clearAdministrationSession(client);
  expect(administrationCreations(client)).toEqual([]);
  expect(administrationCredential(client).stage).toBe('closed');
});

test('key drafts accumulate prerequisite choices, remain scope-local and clear on disconnect', () => {
  const client = {};
  updateAdministrationKeyDraft(client, 'tenant-a', {
    name: 'Deploy service',
    policies: ['reader'],
  });
  const draft = administrationKeyDraft(client, 'tenant-a');
  updateAdministrationKeyDraft(client, 'tenant-a', { policies: [...draft.policies, 'operator'] });
  updateAdministrationKeyDraft(client, 'tenant-a', { group: 'services', boundary: 'boundary' });
  expect(administrationKeyDraft(client, 'tenant-a')).toEqual({
    name: 'Deploy service',
    policies: ['reader', 'operator'],
    group: 'services',
    boundary: 'boundary',
  });
  expect(administrationKeyDraft(client, 'tenant-b')).toMatchObject({
    name: '',
    policies: [],
    group: '',
  });
  expect(administrationKeyDraft({}, 'tenant-a').policies).toEqual([]);
  clearAdministrationSession(client);
  expect(administrationKeyDraft(client, 'tenant-a').name).toBe('');
  updateAdministrationKeyDraft(client, 'tenant-a', { name: 'Late update' });
  expect(administrationKeyDraft(client, 'tenant-a').name).toBe('');
});

test('a definite mutation rejection after unsubscribe is recoverable without unknown reconciliation', async () => {
  const client = {};
  let reject!: (error: Error) => void;
  const operation = vi.fn(
    () =>
      new Promise<void>((_resolve, fail) => {
        reject = fail;
      }),
  );
  const stop = subscribeAdministration(client, vi.fn());
  const pending = runAdministrationMutation(client, 'policies', operation);
  stop();
  expect(administrationScopeLocked(client)).toBe(true);
  reject(new APIError('Create permission required.', 403, 'FORBIDDEN'));
  expect(await pending).toBe(false);
  expect(administrationMutation(client, 'policies')).toMatchObject({ stage: 'rejected' });
  expect(administrationScopeLocked(client)).toBe(false);
  expect(await runAdministrationMutation(client, 'policies', async () => {})).toBe(true);
  expect(operation).toHaveBeenCalledTimes(1);
});

test('pending and unknown mutations do not retry, and require reconciliation only after completion', async () => {
  const client = {};
  let reject!: (error: Error) => void;
  const operation = vi.fn(
    () =>
      new Promise<void>((_resolve, fail) => {
        reject = fail;
      }),
  );
  const pending = runAdministrationMutation(client, 'groups', operation);
  reconcileAdministrationMutation(client, 'groups');
  expect(await runAdministrationMutation(client, 'groups', operation)).toBe(false);
  expect(administrationMutation(client, 'groups').stage).toBe('pending');
  reject(new APIError('Unknown write outcome.', 503, 'UNAVAILABLE'));
  await pending;
  expect(administrationMutation(client, 'groups').stage).toBe('unknown');
  expect(await runAdministrationMutation(client, 'groups', operation)).toBe(false);
  expect(operation).toHaveBeenCalledTimes(1);
  reconcileAdministrationMutation(client, 'groups');
  expect(administrationMutation(client, 'groups').stage).toBe('idle');
});

test('disconnect suppresses delayed non-key mutation state and does not unlock the old connection', async () => {
  const client = {};
  let resolve!: () => void;
  const operation = vi.fn(
    () =>
      new Promise<void>((done) => {
        resolve = done;
      }),
  );
  const pending = runAdministrationMutation(client, 'tenants', operation);
  clearAdministrationSession(client);
  resolve();
  expect(await pending).toBe(false);
  expect(administrationMutation(client, 'tenants').stage).toBe('idle');
  expect(await runAdministrationMutation(client, 'tenants', operation)).toBe(false);
  expect(operation).toHaveBeenCalledTimes(1);
});
