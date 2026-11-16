import { expect, test } from 'vitest';
import {
  collectionSearch,
  collectionSearchParams,
  historyOffset,
  historyPageParams,
  queryWindow,
  queryWindowParams,
} from './query-window';
test('range preserves exact timestamps and compares inclusive/exclusive bounds across offsets', () => {
  const params = new URLSearchParams({
    created_from: '2026-10-19T08:00:00.000001+08:00',
    created_before: '2026-10-19T00:00:00.000002Z',
  });
  expect(queryWindow(params)).toEqual({
    created_from: params.get('created_from'),
    created_before: params.get('created_before'),
  });
  params.set('created_before', '2026-10-19T00:00:00.000001Z');
  expect(queryWindow(params).error).toContain('after From');
  for (const raw of ['bad', '2026-10-19T00:00:00', '2026-02-30T00:00:00Z', '0000-01-01T00:00:00Z'])
    expect(queryWindow(new URLSearchParams({ created_from: raw })).error).toBeTruthy();
});
test('changing ranges resets both pagination encodings while retaining other navigation filters', () => {
  const params = new URLSearchParams('tab=invocations&limit=50&offset=100&page=3&phase=Running');
  const next = queryWindowParams(params, ' 2026-10-19T00:00:00Z ', '');
  expect(next.get('created_from')).toBe('2026-10-19T00:00:00Z');
  expect(next.has('offset')).toBe(false);
  expect(next.has('page')).toBe(false);
  expect(next.get('limit')).toBe('50');
  expect(next.get('tab')).toBe('invocations');
  expect(next.get('phase')).toBe('Running');
  expect(queryWindowParams(next, '', '').has('created_from')).toBe(false);
  expect(params.get('offset')).toBe('100');
});
test('history pagination retains range and respects selected page size', () => {
  const params = new URLSearchParams('limit=50&page=3&created_from=2026-10-19T00%3A00%3A00Z');
  expect(historyOffset(params, 50)).toBe(100);
  const next = historyPageParams(params, 150);
  expect(next.get('offset')).toBe('150');
  expect(next.has('page')).toBe(false);
  expect(next.get('created_from')).toBe(params.get('created_from'));
  expect(historyOffset(next, 50)).toBe(150);
  expect(historyOffset(new URLSearchParams('offset=-1'), 20)).toBe(0);
});
test('collection search changes reset pages and retain literal characters', () => {
  const params = new URLSearchParams('page=3&offset=40&other=keep');
  const next = collectionSearchParams(params, 'API_%');
  expect(collectionSearch(next)).toEqual({ search: 'API_%', error: undefined });
  expect(next.has('page')).toBe(false);
  expect(next.has('offset')).toBe(false);
  expect(next.get('other')).toBe('keep');
  expect(
    collectionSearch(new URLSearchParams({ search: '\u754c'.repeat(129) })).error,
  ).toBeTruthy();
});

test('RFC3339 requires seconds and rejects precision the Go boundary would truncate', () => {
  expect(queryWindow(new URLSearchParams({ created_from: '2026-10-19T00:00Z' })).error).toContain(
    'with seconds',
  );
  expect(
    queryWindow(new URLSearchParams({ created_from: '2026-10-19T00:00:00.0000000001Z' })).error,
  ).toContain('at most 9');
  expect(
    queryWindow(
      new URLSearchParams({
        created_from: '2026-10-19T00:00:00.000000001Z',
        created_before: '2026-10-19T00:00:00.000000002Z',
      }),
    ).error,
  ).toBeUndefined();
});
