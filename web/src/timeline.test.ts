import { describe, expect, it } from 'vitest';
import { attemptTiming, timelineModel, type Attempt } from './timeline';
import type { RunDetail } from './api/contracts';
const attempt = (
  n: number,
  start: string | null,
  end: string | null,
  phase = 'Failed',
): Attempt => ({
  id: n,
  run_id: 1,
  attempt_number: n,
  phase,
  kubernetes_job_name: `attempt-${n}`,
  created_at: '2026-10-13T06:20:00Z',
  started_at: start,
  completed_at: end,
});
const run = (attempts: Attempt[]): RunDetail => ({
  id: 1,
  tenant_id: 'tenant',
  source_uid: 'source',
  revision_id: 17,
  occurrence_key: 'occurrence',
  trigger: 'Manual',
  actor: 'test',
  phase: 'RetryWaiting',
  attempt: attempts.length,
  max_attempts: 3,
  created_at: '2026-10-13T06:20:00Z',
  updated_at: '2026-10-13T06:21:23Z',
  attempts,
});
describe('recorded attempt timing', () => {
  it('represents execution time and intervening gap on one scale', () => {
    const model = timelineModel(
      run([
        attempt(2, '2026-10-13T06:20:58Z', '2026-10-13T06:21:22Z'),
        attempt(1, '2026-10-13T06:20:04Z', '2026-10-13T06:20:28Z'),
      ]),
    );
    expect(model.end! - model.start!).toBe(78_000);
    expect(model.spans.map((span) => span.duration)).toEqual([24_000, 24_000]);
    expect(model.spans[1].left).toBeCloseTo((54 / 78) * 100);
    expect(model.gaps).toEqual([{ from: 1, to: 2, milliseconds: 30_000 }]);
  });
  it('does not manufacture durations from missing or inverted completed timestamps', () => {
    expect(attemptTiming(attempt(1, null, null), run([]).updated_at)).toBeNull();
    expect(
      attemptTiming(attempt(1, '2026-10-13T06:21:00Z', '2026-10-13T06:20:00Z'), run([]).updated_at),
    ).toBeNull();
    expect(attemptTiming(attempt(1, '2026-10-13T06:20:00Z', null), run([]).updated_at)).toBeNull();
  });
  it('uses the last observation for an active attempt rather than wall-clock now', () => {
    const timing = attemptTiming(
      attempt(1, '2026-10-13T06:20:00Z', null, 'Running'),
      '2026-10-13T06:20:24Z',
    );
    expect(timing).toMatchObject({ active: true, duration: 24_000 });
  });
  it('preserves overlaps and handles empty and zero-length timelines', () => {
    expect(timelineModel(run([])).start).toBeNull();
    const model = timelineModel(run([attempt(1, '2026-10-13T06:20:00Z', '2026-10-13T06:20:00Z')]));
    expect(model.spans[0]).toMatchObject({ width: 0, left: 0 });
    expect(
      timelineModel(
        run([
          attempt(1, '2026-10-13T06:20:00Z', '2026-10-13T06:20:30Z'),
          attempt(2, '2026-10-13T06:20:20Z', '2026-10-13T06:20:40Z'),
        ]),
      ).gaps[0].milliseconds,
    ).toBe(-10_000);
  });
});
