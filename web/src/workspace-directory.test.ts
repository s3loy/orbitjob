import { QueryClient } from '@tanstack/react-query';
import { expect, test } from 'vitest';
import { cachedWorkspace, workspaceDirectoryKey } from './workspace-directory';

test('a workspace name is reused only from successful directory data in the current connection', async () => {
  const cache = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const row = {
    id: 'workspace',
    name: '\u8d22\u52a1\u56e2\u961f',
    slug: 'finance',
    status: 'active',
  };
  const key = workspaceDirectoryKey(7, ' \u8d22\u52a1 ', 20);
  expect(key).toEqual(['workspaces', 7, '\u8d22\u52a1', 20]);
  cache.setQueryData(key, { items: [row], has_more: false });
  expect(cachedWorkspace(cache, 7, row.id)).toEqual(row);
  expect(cachedWorkspace(cache, 8, row.id)).toBeUndefined();
  await expect(
    cache.fetchQuery({
      queryKey: key,
      queryFn: async () => {
        throw new Error('Directory denied');
      },
    }),
  ).rejects.toThrow('Directory denied');
  expect(cache.getQueryData(key)).toBeDefined();
  expect(cachedWorkspace(cache, 7, row.id)).toBeUndefined();
  cache.clear();
});

test('management navigation reuses an authorized directory label without borrowing other account data', async () => {
  const cache = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const row = { id: 'foreign-workspace', name: 'Finance', slug: 'finance', status: 'active' };
  const key = ['administration', 7, 'tenants', 'current', 20, 20, 'Finance', 'none'];
  cache.setQueryData(key, { items: [row], has_more: false });
  expect(cachedWorkspace(cache, 7, row.id)).toEqual(row);
  expect(cachedWorkspace(cache, 8, row.id)).toBeUndefined();
  await expect(
    cache.fetchQuery({
      queryKey: key,
      queryFn: async () => {
        throw new Error('Directory permission revoked');
      },
    }),
  ).rejects.toThrow('Directory permission revoked');
  expect(cache.getQueryData(key)).toBeDefined();
  expect(cachedWorkspace(cache, 7, row.id)).toBeUndefined();
  cache.setQueryData(['administration', 7, 'keys', row.id], { items: [row] });
  expect(cachedWorkspace(cache, 7, row.id)).toBeUndefined();
  cache.clear();
});
