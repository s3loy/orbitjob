import { expect, test } from 'vitest';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from './list-state';
import { demoClient } from './api/demo';

test('page size accepts only the supported URL values and defaults to twenty', () => {
  for (const limit of [10, 20, 50, 100])
    expect(pageLimit(new URLSearchParams({ limit: String(limit) }))).toBe(limit);
  for (const raw of ['', '0', '-10', '21', '999', '10.0', ' 10', 'NaN', 'Infinity'])
    expect(pageLimit(new URLSearchParams({ limit: raw }))).toBe(20);
  expect(pageLimit(new URLSearchParams())).toBe(20);
});

test('page offsets and next-page URLs use the selected size without losing filters', () => {
  const params = new URLSearchParams('limit=50&page=3&search=billing&namespace=finance&offset=40');
  expect(pageOffset(params)).toBe(100);
  const next = pageParams(params, 150);
  expect(next.get('page')).toBe('4');
  expect(next.get('limit')).toBe('50');
  expect(next.get('search')).toBe('billing');
  expect(next.get('namespace')).toBe('finance');
  expect(next.has('offset')).toBe(false);
  expect(pageOffset(next)).toBe(150);
  expect(params.get('page')).toBe('3');
  expect(pageParams(params, 0).has('page')).toBe(false);
});

test('changing page size returns to the first page while retaining exact query context', () => {
  const params = new URLSearchParams({
    page: '8',
    offset: '140',
    limit: '20',
    status: 'active',
    search: 'API_%',
    tab: 'runs',
    created_from: '2026-10-19T00:00:00.000001Z',
    created_before: '2026-10-20T08:00:00+08:00',
  });
  const next = pageLimitParams(params, 100);
  expect(next.has('page')).toBe(false);
  expect(next.has('offset')).toBe(false);
  expect(next.get('limit')).toBe('100');
  for (const key of ['status', 'search', 'tab', 'created_from', 'created_before'])
    expect(next.get(key)).toBe(params.get(key));
  expect(pageOffset(next)).toBe(0);
  expect(pageLimitParams(next, 20).has('limit')).toBe(false);
  expect(pageLimitParams(next, 999).has('limit')).toBe(false);
  expect(params.get('page')).toBe('8');
});

test('invalid or overflowing page positions cannot create an unbounded offset', () => {
  for (const page of ['0', '-1', '1.5', 'bad', '9007199254740991'])
    expect(pageOffset(new URLSearchParams({ limit: '100', page }))).toBe(0);
  for (const offset of [-1, 15, Number.NaN, Number.POSITIVE_INFINITY, 2_147_483_650])
    expect(pageParams(new URLSearchParams('limit=10&page=8'), offset).has('page')).toBe(false);
});

test('fixture clients honor requested limits and offsets across collection families', async () => {
  const client = demoClient();
  try {
    expect((await client.jobs(1, '', { limit: 1 })).map((item) => item.id)).toEqual([18]);
    expect(await client.runs({ offset: 10, limit: 10 })).toHaveLength(10);
    expect((await client.functions({ offset: 1, limit: 1 })).map((item) => item.id)).toEqual([24]);
    expect((await client.workflows({ offset: 1, limit: 1 })).map((item) => item.id)).toEqual([405]);
    expect((await client.checks({ offset: 1, limit: 1 })).map((item) => item.id)).toEqual([2]);
    expect((await client.checkResults({ offset: 1, limit: 1 })).map((item) => item.id)).toEqual([
      701,
    ]);
    expect((await client.slos({ offset: 1, limit: 1 })).map((item) => item.id)).toEqual([22]);
    expect(await client.workflowRuns('412', { limit: 1 })).toHaveLength(1);
    expect(await client.functionRuns('27', { limit: 1 })).toHaveLength(1);
  } finally {
    client.dispose();
  }
});
