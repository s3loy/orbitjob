import { afterEach, expect, it, vi } from 'vitest';
import { demoClient } from './demo';

afterEach(() => vi.useRealTimers());
async function answer<T>(pending: Promise<T>) {
  await vi.advanceTimersByTimeAsync(200);
  return pending;
}

it('keeps Unknown at the last known attempt and only cancels active execution', async () => {
  vi.useFakeTimers();
  const api = demoClient();
  try {
    const unknown = (await answer(api.runs({ phase: 'CancelUnknown' })))[0];
    const detail = await answer(api.run(unknown.id));
    expect(detail.attempts.at(-1)).toMatchObject({ phase: 'Running', completed_at: null });
    const active = (await answer(api.runs({ phase: 'Running' })))[0];
    await answer(api.cancel(active.id));
    await vi.advanceTimersByTimeAsync(3600);
    const stopped = await answer(api.run(active.id));
    expect(stopped.phase).toBe('Canceled');
    expect(stopped.attempts.at(-1)?.phase).toBe('Canceled');
    expect(stopped.attempts.at(-1)?.completed_at).toBeTruthy();
    const waiting = (await answer(api.runs({ phase: 'RetryWaiting' })))[0];
    const prior = await answer(api.run(waiting.id));
    await answer(api.cancel(waiting.id));
    await vi.advanceTimersByTimeAsync(3600);
    const canceled = await answer(api.run(waiting.id));
    expect(canceled.phase).toBe('Canceled');
    expect(canceled.attempts).toEqual(prior.attempts);
  } finally {
    api.dispose();
  }
});
