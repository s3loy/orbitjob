import type { RunDetail } from './api/contracts';
export type Attempt = RunDetail['attempts'][number];
export function timestamp(value?: string | null) {
  if (!value) return null;
  const number = Date.parse(value);
  return Number.isFinite(number) ? number : null;
}
export function duration(milliseconds: number) {
  const seconds = Math.floor(Math.max(0, milliseconds) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  return minutes < 60
    ? `${minutes}m ${seconds % 60}s`
    : `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}
export function attemptTiming(attempt: Attempt, observedAt: string) {
  const start = timestamp(attempt.started_at);
  const complete = timestamp(attempt.completed_at);
  const active =
    !complete &&
    !attempt.execution_unknown_since &&
    ['Running', 'CreatingAttempt'].includes(attempt.phase);
  const end = complete ?? (active ? timestamp(observedAt) : null);
  return start !== null && end !== null && end >= start
    ? { start, end, active, duration: end - start }
    : null;
}
export function timelineModel(run: RunDetail) {
  const attempts = [...run.attempts].sort((a, b) => a.attempt_number - b.attempt_number);
  const spans = attempts.flatMap((attempt) => {
    const timing = attemptTiming(attempt, run.updated_at);
    return timing ? [{ attempt, ...timing }] : [];
  });
  const start = spans.length ? Math.min(...spans.map((span) => span.start)) : null;
  const end = spans.length ? Math.max(...spans.map((span) => span.end)) : null;
  const extent = start !== null && end !== null ? Math.max(1, end - start) : 1;
  const gaps = attempts.slice(1).flatMap((attempt, index) => {
    const prior = attempts[index];
    const previousEnd = timestamp(prior.completed_at);
    const nextStart = timestamp(attempt.started_at);
    return previousEnd !== null && nextStart !== null
      ? [
          {
            from: prior.attempt_number,
            to: attempt.attempt_number,
            milliseconds: nextStart - previousEnd,
          },
        ]
      : [];
  });
  return {
    attempts,
    start,
    end,
    gaps,
    spans: spans.map((span) => ({
      ...span,
      left: ((span.start - start!) / extent) * 100,
      width: ((span.end - span.start) / extent) * 100,
    })),
  };
}
