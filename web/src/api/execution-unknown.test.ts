import { expect, it } from 'vitest';
import { demoClient } from './demo';
import { phases, runDetailSchema, terminal } from './contracts';
import { attemptTiming } from '../timeline';

it('retains unknown execution evidence without inventing a completed outcome', async () => {
  const client = demoClient();
  try {
    const original = await client.run('1042');
    const run = runDetailSchema.parse({
      ...original,
      phase: 'ExecutionUnknown',
      attempts: original.attempts.map((attempt) => ({
        ...attempt,
        phase: 'ExecutionUnknown',
        completed_at: null,
        owner_run_uid: 'original-run-uid',
        execution_unknown_since: original.updated_at,
        execution_unknown_reason: 'JobNotFound',
      })),
    });
    expect(phases).toContain('ExecutionUnknown');
    expect(terminal(run.phase)).toBe(false);
    expect(run.attempts.at(-1)).toMatchObject({
      owner_run_uid: 'original-run-uid',
      execution_unknown_since: original.updated_at,
      execution_unknown_reason: 'JobNotFound',
      completed_at: null,
    });
    expect(attemptTiming(run.attempts.at(-1)!, run.updated_at)).toBeNull();
    expect(attemptTiming({ ...run.attempts.at(-1)!, phase: 'Running' }, run.updated_at)).toBeNull();
    expect(runDetailSchema.safeParse(original).success).toBe(true);
  } finally {
    client.dispose();
  }
});
