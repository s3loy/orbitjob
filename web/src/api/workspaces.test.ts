import { afterEach, expect, test, vi } from 'vitest';
import { liveClient } from './transport';
import { administrationDemo } from './administration-demo';

afterEach(() => vi.unstubAllGlobals());

test('workspace discovery searches the server without inheriting the business target', async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () => new Response(JSON.stringify({ items: [], has_more: false })),
  );
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('fixture-only', 'business-target');
  await client.workspaceDirectory(20, ' \u8d22\u52a1 %_\\ ');
  const url = new URL(String(fetcher.mock.calls[0][0]), 'http://console.invalid');
  expect(url.pathname).toBe('/api/v1/workspaces');
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: '20',
    offset: '20',
    search: '\u8d22\u52a1 %_\\',
  });
  client.dispose();
});

test('workspace discovery rejects a response without an explicit pagination decision', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{"items":[]}')),
  );
  const client = liveClient('fixture-only');
  await expect(client.workspaceDirectory()).rejects.toMatchObject({ code: 'INVALID_RESPONSE' });
  client.dispose();
});

test('demo discovery searches all names and slugs before pagination and finds newly created workspaces', async () => {
  const client = administrationDemo(async (value) => value);
  for (let index = 0; index < 21; index++)
    await client.adminCreateTenant({ name: `Workspace ${index}`, slug: `workspace-${index}` });
  const created = await client.adminCreateTenant({
    name: '\u8d22\u52a1\u56e2\u961f',
    slug: 'finance-team',
  });
  expect((await client.workspaceDirectory()).has_more).toBe(true);
  expect(await client.workspaceDirectory(0, '\u8d22\u52a1')).toEqual({
    items: [created],
    has_more: false,
  });
  expect((await client.workspaceDirectory(0, 'FINANCE')).items).toEqual([created]);
});
