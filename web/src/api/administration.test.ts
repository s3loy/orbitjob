import { isValid } from 'ulid';
import { expect, test, vi } from 'vitest';
import { administrationClient } from './administration';
import type { Request } from './monitoring';
import { administrationDemo } from './administration-demo';
import { liveClient } from './transport';
test('administration paths preserve opaque identifiers and only tenants are paginated', async () => {
  const request = vi.fn(async () => ({ items: [] })) as unknown as Request;
  const client = administrationClient(request, 20);
  await client.adminTenants(40);
  await client.adminKeys('a/b?c');
  await client.adminPolicies();
  await client.adminGroups();
  expect(vi.mocked(request).mock.calls.map((c) => c[0])).toEqual([
    '/api/v1/tenants?limit=20&offset=40',
    '/api/v1/tenants/a%2Fb%3Fc/api_keys',
    '/api/v1/policies',
    '/api/v1/resource_groups',
  ]);
});
test('explicit management directories and writes retain their target even on another workload connection', async () => {
  const fetch = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(async () => new Response(JSON.stringify({ items: [] }), { status: 200 }));
  const client = liveClient('unit-test-credential', 'tenant-a');
  try {
    await client.adminPolicies('tenant-b');
    await client.adminGroups('tenant-b');
    expect(fetch.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/tenants/tenant-b/policies',
      '/api/v1/tenants/tenant-b/resource_groups',
    ]);
  } finally {
    client.dispose();
    fetch.mockRestore();
  }
});
test('management creates target the same explicit workspace as their catalogs', async () => {
  const request = vi.fn(async () => ({
    id: 'id',
    name: 'Created',
    description: '',
    slug: 'created',
  })) as unknown as Request;
  const client = administrationClient(request, 20);
  await client.adminCreatePolicy(
    { name: 'Created', document: { version: '1', statement: [] } },
    'b/c',
  );
  await client.adminCreateGroup({ name: 'Created', slug: 'created' }, 'b/c');
  expect(vi.mocked(request).mock.calls.map((call) => [call[0], call[2]])).toEqual([
    ['/api/v1/tenants/b%2Fc/policies', 'POST'],
    ['/api/v1/tenants/b%2Fc/resource_groups', 'POST'],
  ]);
});
test('policy input is a JSON document and delete has no invented version', async () => {
  const request = vi.fn(async () => ({
    id: 'opaque',
    name: 'Read',
    description: '',
  })) as unknown as Request;
  const client = administrationClient(request, 20);
  const input = {
    name: 'Read',
    document: {
      version: '1',
      statement: [{ effect: 'Allow', action: ['job:Get'], resource: ['orbitjob:T1:ci:job/*'] }],
    },
  };
  await client.adminCreatePolicy(input);
  await client.adminDeletePolicy('opaque');
  expect(vi.mocked(request).mock.calls[0].slice(2)).toEqual(['POST', undefined, input]);
  expect(vi.mocked(request).mock.calls[1].slice(2)).toEqual(['DELETE']);
});
test('key reveal is not present in demo key list and revoke is observable', async () => {
  const client = administrationDemo(async (value) => value);
  const tenant = (await client.adminTenants())[0];
  const result = await client.adminCreateKey(tenant.id, {});
  expect(result.key).toMatch(/^demo-only-/);
  expect(await client.adminKeys(tenant.id)).toEqual([
    { id: result.id, name: '', key_prefix: result.key_prefix, created_at: result.created_at },
  ]);
  await client.adminRevokeKey(result.id);
  expect((await client.adminKeys(tenant.id))[0].revoked_at).toBeTruthy();
});
test('demo key rename and revoke honor an explicit management workspace', async () => {
  const client = administrationDemo(async (value) => value);
  const tenant = (await client.adminTenants())[0];
  const other = await client.adminCreateTenant({
    name: 'Other workspace',
    slug: 'other-workspace',
  });
  const key = await client.adminCreateKey(tenant.id, { name: 'Original service' });
  await expect(client.adminRenameKey(key.id, 'Wrong workspace', other.id)).rejects.toMatchObject({
    status: 404,
  });
  await expect(client.adminRevokeKey(key.id, other.id)).rejects.toMatchObject({ status: 404 });
  expect((await client.adminKeys(tenant.id))[0]).toMatchObject({ name: 'Original service' });
  expect((await client.adminKeys(tenant.id))[0].revoked_at).toBeUndefined();
  await client.adminRenameKey(key.id, 'Renamed service', tenant.id);
  expect((await client.adminKeys(tenant.id))[0].name).toBe('Renamed service');
  await client.adminRevokeKey(key.id, tenant.id);
  expect((await client.adminKeys(tenant.id))[0].revoked_at).toBeTruthy();
});
test('a rejected create is invoked exactly once', async () => {
  const request = vi.fn(async () => {
    throw new Error('unknown response');
  }) as unknown as Request;
  const client = administrationClient(request, 20);
  await expect(client.adminCreateKey('tenant', {})).rejects.toThrow('unknown response');
  expect(request).toHaveBeenCalledTimes(1);
});

test('demo creates unique standard ULIDs for stored administrative resources', async () => {
  const client = administrationDemo(async (value) => value);
  const tenant = await client.adminCreateTenant({ name: 'Generated tenant', slug: 'generated' });
  const group = await client.adminCreateGroup({ name: 'Generated group', slug: 'generated-group' });
  const policy = await client.adminCreatePolicy({
    name: 'Generated policy',
    document: { version: '1', statement: [] },
  });
  const key = await client.adminCreateKey(tenant.id, {
    resource_group_id: group.id,
    policies: [policy.id],
  });
  const ids = [tenant.id, group.id, policy.id, key.id];
  expect(ids.every(isValid)).toBe(true);
  expect(new Set(ids).size).toBe(ids.length);
  expect((await client.adminTenant(tenant.id)).id).toBe(tenant.id);
  expect((await client.adminGroups()).some((row) => row.id === group.id)).toBe(true);
  expect((await client.adminPolicy(policy.id)).id).toBe(policy.id);
  expect((await client.adminKeys(tenant.id))[0].id).toBe(key.id);
});
