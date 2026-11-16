import { afterEach, expect, test, vi } from 'vitest';
import { liveClient } from './transport';
afterEach(() => vi.unstubAllGlobals());
test('M4 server filters encode exact literal search and UTC history bounds', async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response('{"items":[]}'));
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('memory-only-test');
  await client.workflows({ offset: 20, search: '50% _ + billing' });
  await client.workflowRuns('412', {
    offset: 40,
    created_from: '2026-10-19T00:00:00Z',
    created_before: '2026-10-20T00:00:00Z',
  });
  await client.functions({ offset: 20, search: 'index' });
  await client.functionRuns('27', { offset: 20, created_from: '2026-10-19T00:00:00Z' });
  const urls = fetcher.mock.calls.map((call) => new URL(String(call[0]), 'http://fixture'));
  expect(urls[0].searchParams.get('search')).toBe('50% _ + billing');
  expect(urls[0].searchParams.get('offset')).toBe('20');
  expect(urls[1].searchParams.get('created_before')).toBe('2026-10-20T00:00:00Z');
  expect(urls[2].searchParams.get('search')).toBe('index');
  expect(urls[3].searchParams.get('offset')).toBe('20');
  client.dispose();
});
