import { expect, it, vi } from 'vitest';
import type { ArchivePage, ArchiveRecord } from './api/archive';
import { prepareArchiveExport } from './archive-export';

const record = (sequence: number, text: string): ArchiveRecord => ({
  id: `row-${sequence}`,
  sequence,
  time: '2026-11-17T00:00:00Z',
  observed_at: '2026-11-17T00:00:00Z',
  kind: 'stdout',
  stage: 'running',
  attempt: 1,
  pod_uid: 'pod-1',
  container: 'task',
  restart: 0,
  text,
});
const page = (change: Partial<ArchivePage> = {}): ArchivePage => ({
  state: 'sealed',
  reason: 'finished',
  outcome: 'Succeeded',
  records: [],
  gaps: [],
  next: 0,
  has_more: false,
  output_bytes: 0,
  retained_bytes: 0,
  limit_bytes: 1024,
  ...change,
});

it('includes later collection changes and gaps, deduplicating repeated page evidence', async () => {
  const gap = { from: 2, through: 3, reason: 'window rotated' };
  const read = vi
    .fn<(after: number) => Promise<ArchivePage>>()
    .mockResolvedValueOnce(
      page({
        state: 'collecting',
        reason: 'running',
        records: [record(1, 'first\n')],
        next: 1,
        has_more: true,
      }),
    )
    .mockResolvedValueOnce(
      page({
        state: 'partial',
        reason: 'rotation',
        gaps: [gap],
        records: [record(1, 'first\n'), record(4, 'last\n')],
        next: 4,
        has_more: true,
      }),
    )
    .mockResolvedValueOnce(page({ state: 'partial', reason: 'rotation', gaps: [gap], next: 4 }));
  const result = await prepareArchiveExport('instance/77', read);
  const text = result.chunks.join('');
  expect(read.mock.calls).toEqual([[0], [1], [4]]);
  expect(text).toContain('# Collection: collecting — running');
  expect(text).toContain('# Collection: partial — rotation');
  expect(text.match(/# Collection: partial/g)).toHaveLength(1);
  expect(text.match(/# OUTPUT GAP 2-3: window rotated/g)).toHaveLength(1);
  expect(text.match(/first\n/g)).toHaveLength(1);
  expect(text).toContain('last\n');
  expect(text).toContain('# END OF RETAINED LOG EXPORT');
  expect(result.notice).toContain('Reported collection changes');
});

it('records a changed reason even when the collection state stays the same', async () => {
  const read = vi
    .fn<(after: number) => Promise<ArchivePage>>()
    .mockResolvedValueOnce(
      page({ state: 'partial', reason: 'collector loss', next: 1, has_more: true }),
    )
    .mockResolvedValueOnce(page({ state: 'partial', reason: 'storage rotated', next: 1 }));
  const result = await prepareArchiveExport('instance/77', read);
  expect(result.chunks.join('')).toContain('# Collection: partial — storage rotated');
});

it('preserves unknown gap ranges and deduplicates them independently of known ranges', async () => {
  const gaps = [
    { from: 0, through: 0, reason: 'exact missing source range is unavailable' },
    { from: 2, through: 9, reason: 'retained segment rotated' },
  ];
  const read = vi
    .fn<(after: number) => Promise<ArchivePage>>()
    .mockResolvedValueOnce(page({ state: 'partial', gaps, next: 1, has_more: true }))
    .mockResolvedValueOnce(page({ state: 'partial', gaps, next: 1 }));
  const result = await prepareArchiveExport('instance/77', read);
  const text = result.chunks.join('');
  expect(
    text.match(/# OUTPUT GAP \(range unknown\): exact missing source range is unavailable/g),
  ).toHaveLength(1);
  expect(text.match(/# OUTPUT GAP 2-9: retained segment rotated/g)).toHaveLength(1);
  expect(text).not.toContain('# OUTPUT GAP 0-0:');
  expect(text).toContain('# END OF RETAINED LOG EXPORT');
});

it.each(['expired', 'deleted', 'unavailable'] as const)(
  'labels an initially %s archive as incomplete',
  async (state) => {
    const read = vi.fn(async () => page({ state, reason: 'archive lost' }));
    const result = await prepareArchiveExport('instance/77', read);
    const text = result.chunks.join('');
    expect(text).toContain(`# Collection: ${state} — archive lost`);
    expect(text).toContain(`# EXPORT INCOMPLETE: logs ${state}`);
    expect(text).not.toContain('# END OF RETAINED LOG EXPORT');
    expect(result.notice).toContain(`Download incomplete: logs ${state}`);
    expect(read).toHaveBeenCalledTimes(1);
  },
);

it.each(['expired', 'deleted', 'unavailable'] as const)(
  'labels a later %s transition as incomplete and preserves already fetched output',
  async (state) => {
    const read = vi
      .fn<(after: number) => Promise<ArchivePage>>()
      .mockResolvedValueOnce(
        page({ records: [record(1, 'retained before loss\n')], next: 1, has_more: true }),
      )
      .mockResolvedValueOnce(
        page({
          state,
          reason: 'archive lost',
          next: 1,
          has_more: true,
          gaps: [{ from: 2, through: 9, reason: 'lost' }],
        }),
      );
    const result = await prepareArchiveExport('instance/77', read);
    const text = result.chunks.join('');
    expect(text).toContain('retained before loss');
    expect(text).toContain(`# Collection: ${state} — archive lost`);
    expect(text).toContain('# OUTPUT GAP 2-9: lost');
    expect(text).toContain(`# EXPORT INCOMPLETE: logs ${state}`);
    expect(text).not.toContain('# END OF RETAINED LOG EXPORT');
    expect(result.notice).toContain(`Download incomplete: logs ${state}`);
    expect(read).toHaveBeenCalledTimes(2);
  },
);

it('describes a collecting archive as a retained snapshot', async () => {
  const result = await prepareArchiveExport('instance/77', async () =>
    page({ state: 'collecting' }),
  );
  expect(result.chunks.join('')).toContain('# Collection is still in progress');
  expect(result.notice).toContain('Collection is still in progress');
});

it('rejects nonadvancing cursors without declaring completion', async () => {
  await expect(
    prepareArchiveExport('instance/77', async () => page({ has_more: true })),
  ).rejects.toThrow('Log export cursor did not advance');
});

it('includes gap metadata in the browser download size bound', async () => {
  await expect(
    prepareArchiveExport('instance/77', async () =>
      page({ gaps: [{ from: 1, through: 2, reason: 'x'.repeat(40 * 1024 * 1024) }] }),
    ),
  ).rejects.toThrow('bounded download size');
});
