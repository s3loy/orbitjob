import { afterEach, expect, it, vi } from 'vitest';
import { APIError, liveClient } from './transport';
import {
  keepSourceControlIntent,
  readSourceControlIntent,
  sourceControlValuesEqual,
  sourceControlWriteUnknown,
  sourceControlIntentKey,
  type SourceControl,
} from './source-controls';

afterEach(() => vi.unstubAllGlobals());
const current: SourceControl = {
  source_mode: 'function',
  source_uid: 'function-7',
  version: 1,
  schedule_paused: false,
  execution_paused: true,
  tombstoned: false,
  can_manage: true,
};

it('keeps target tenant and exact int64 CAS on the shared credential transport', async () => {
  const fetcher = vi
    .fn()
    .mockImplementation(() => Promise.resolve(new Response(JSON.stringify(current))));
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('fixture_credential_not_real', 'finance-tenant');
  await client.updateSourceControls!('functions', '7', {
    version: '9007199254740993',
    schedule_paused: false,
    execution_paused: true,
  });
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(fetcher.mock.calls[0][0]).toBe(
    '/api/v1/functions/7/execution-controls?tenant_id=finance-tenant',
  );
  expect(fetcher.mock.calls[0][1]).toMatchObject({
    method: 'PATCH',
    credentials: 'omit',
    redirect: 'error',
  });
  expect(fetcher.mock.calls[0][1].body).toContain('"version":9007199254740993');
  client.dispose();
  expect(fetcher.mock.calls[0][1].signal.aborted).toBe(true);
});

it('never retries an uncertain PATCH and reads current state only on an explicit request', async () => {
  const fetcher = vi
    .fn()
    .mockRejectedValueOnce(new TypeError('connection closed'))
    .mockResolvedValueOnce(new Response(JSON.stringify(current)));
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('fixture_credential_not_real');
  let failure: unknown;
  try {
    await client.updateSourceControls!('functions', '7', {
      version: 0,
      schedule_paused: false,
      execution_paused: true,
    });
  } catch (error) {
    failure = error;
  }
  expect(sourceControlWriteUnknown(failure)).toBe(true);
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(await client.sourceControls!('functions', '7')).toEqual(current);
  expect(fetcher.mock.calls.map((call) => call[1].method)).toEqual(['PATCH', 'GET']);
  client.dispose();
});

it('separates rejected CAS from unknown responses without treating equal state as a receipt', () => {
  expect(sourceControlWriteUnknown(new APIError('version changed', 409, 'CONFLICT'))).toBe(false);
  expect(sourceControlWriteUnknown(new APIError('unavailable', 503, 'UNAVAILABLE'))).toBe(true);
  expect(sourceControlWriteUnknown(new APIError('invalid reply', 200, 'INVALID_RESPONSE'))).toBe(
    true,
  );
  expect(
    sourceControlValuesEqual(current, {
      version: 0,
      schedule_paused: false,
      execution_paused: true,
    }),
  ).toBe(true);
});

it('retains only scoped source intent across refresh and removes it explicitly', () => {
  const values = new Map<string, string>();
  const store = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
  const intent = {
    source_uid: 'function-7',
    source_mode: 'function' as const,
    version: 0,
    schedule_paused: false,
    execution_paused: true,
  };
  keepSourceControlIntent('principal-a:tenant-a:functions:7', intent, store);
  expect(readSourceControlIntent('principal-a:tenant-a:functions:7', store)).toEqual(intent);
  expect(readSourceControlIntent('principal-b:tenant-a:functions:7', store)).toBeUndefined();
  expect(values.values().next().value).not.toContain('credential');
  keepSourceControlIntent('principal-a:tenant-a:functions:7', undefined, store);
  expect(readSourceControlIntent('principal-a:tenant-a:functions:7', store)).toBeUndefined();
});

it('partitions retained intent by stable human identity and API-key subject', () => {
  const alice = { subject_type: 'user', user_id: 'alice', key_id: '' };
  const bob = { subject_type: 'user', user_id: 'bob', key_id: '' };
  const scope = ['workspace-a', 'functions', '7'] as const;
  const original = sourceControlIntentKey(alice, ...scope);
  expect(original).toBe(sourceControlIntentKey({ ...alice }, ...scope));
  expect(original).not.toBe(sourceControlIntentKey(bob, ...scope));
  expect(original).not.toBe(
    sourceControlIntentKey({ subject_type: 'api_key', key_id: 'alice' }, ...scope),
  );
  expect(original).not.toBe(sourceControlIntentKey(alice, 'workspace-b', 'functions', '7'));
  expect(sourceControlIntentKey({ subject_type: 'user', key_id: '' }, ...scope)).toBeUndefined();
  expect(sourceControlIntentKey(undefined, ...scope)).toBeUndefined();
});
