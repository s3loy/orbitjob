import { afterEach, expect, it, vi } from 'vitest';
import { liveClient } from './transport';
afterEach(() => vi.unstubAllGlobals());
const result = {
  key_id: 'key',
  kind: 'platform',
  tenant_id: '',
  target_tenant_id: 'TENANT',
  group_id: '',
  boundary_policy_id: '',
  capabilities: [],
};
it('pairs capability probes and explicitly scopes platform data without altering management paths', async () => {
  const fetcher = vi.fn(
    async (path: string) =>
      new Response(JSON.stringify(path.includes('identity') ? result : { items: [] })),
  );
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('fixture-not-a-real-key', 'TENANT');
  await client.identity([{ action: 'job:Get', resource: 'orbitjob:TENANT:*:job/17' }]);
  await client.jobs(20, '%_literal');
  await client.adminKeys('OWNER');
  await client.adminTenants();
  const paths = fetcher.mock.calls.map(([path]) => new URL(path, 'http://console.invalid'));
  expect(paths[0].searchParams.get('tenant_id')).toBe('TENANT');
  expect(paths[0].searchParams.getAll('action')).toEqual(['job:Get']);
  expect(paths[0].searchParams.getAll('resource')).toEqual(['orbitjob:TENANT:*:job/17']);
  expect(paths[1].searchParams.get('search')).toBe('%_literal');
  expect(paths[1].searchParams.get('offset')).toBe('20');
  expect(paths[2].pathname).toBe('/api/v1/tenants/OWNER/api_keys');
  expect(paths[2].searchParams.has('tenant_id')).toBe(false);
  expect(paths[3].searchParams.get('tenant_id')).toBe('TENANT');
  expect(paths.every((p) => p.origin === 'http://console.invalid')).toBe(true);
  client.dispose();
});
it('counts the entire selected ledger without passing page bounds', async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () => new Response('{"total":9007199254740993,"phases":{"Succeeded":9007199254740993}}'),
  );
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('fixture');
  const summary = await client.runSummary({
    phase: 'Succeeded',
    offset: 20,
    limit: 20,
    created_from: '2026-10-01T00:00:00Z',
  });
  expect(summary.total).toBe('9007199254740993');
  const query = new URL(String(fetcher.mock.calls[0][0]), 'http://console.invalid').searchParams;
  expect(query.has('offset')).toBe(false);
  expect(query.get('phase')).toBe('Succeeded');
  client.dispose();
});
