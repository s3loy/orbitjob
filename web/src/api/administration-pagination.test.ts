import { expect, test, vi } from 'vitest';
import { administrationClient } from './administration';
import { administrationDemo } from './administration-demo';
import type { Request } from './monitoring';

test('workspace pages send selected limits while directory pickers keep the default', async () => {
  const request = vi.fn(async () => ({ items: [], has_more: false })) as unknown as Request;
  const client = administrationClient(request, 20);
  await client.workspaceDirectory(50, ' finance ', 50);
  await client.workspaceDirectory(20, 'finance');
  expect(vi.mocked(request).mock.calls.map((call) => call[0])).toEqual([
    '/api/v1/workspaces?limit=50&offset=50&search=finance',
    '/api/v1/workspaces?limit=20&offset=20&search=finance',
  ]);
});

test('workspace fixture slicing and has_more use the requested page size', async () => {
  const client = administrationDemo(async (value) => value);
  for (let i = 0; i < 20; i++)
    await client.adminCreateTenant({
      name: `Finance ${String(i).padStart(2, '0')}`,
      slug: `f-${i}`,
    });
  const first = await client.workspaceDirectory(0, 'finance', 10);
  const last = await client.workspaceDirectory(10, 'finance', 10);
  expect(first.items).toHaveLength(10);
  expect(first.has_more).toBe(true);
  expect(last.items).toHaveLength(10);
  expect(last.has_more).toBe(false);
  expect(new Set([...first.items, ...last.items].map((row) => row.id)).size).toBe(20);
  expect(await client.workspaceDirectory(0, 'finance', 50)).toMatchObject({ has_more: false });
});
