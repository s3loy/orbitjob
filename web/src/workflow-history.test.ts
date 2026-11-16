import { expect, test } from 'vitest';
import { demoClient } from './api/demo';
import { workflowRunDetailSchema } from './api/m4';
import {
  hasPinnedGraph,
  pinnedTaskLayers,
  stepsForTask,
  workflowDecisionReason,
  workflowOutcomeConflict,
} from './workflow-history-state';

test('failure decisions explain missing job references and deadline without losing unknown evidence', () => {
  expect(workflowDecisionReason('missing_job_reference')).toBe('Job reference missing');
  expect(workflowDecisionReason('deadline')).toBe('Workflow deadline exceeded');
  expect(workflowDecisionReason('future_reason')).toBe('future_reason');
  expect(workflowDecisionReason()).toBe('Reason not recorded');
});
test('historical success conflicting with a failure decision is flagged without changing the recorded phase', async () => {
  const client = demoClient();
  const run = await client.workflowRun('412', '94');
  run.phase = 'Succeeded';
  run.task_decisions = { skipped: { skipped: true, reason: 'deadline' } };
  expect(workflowOutcomeConflict(run)).toBe(true);
  expect(run.phase).toBe('Succeeded');
  run.task_decisions = { skipped: { skipped: true, reason: 'condition' } };
  expect(workflowOutcomeConflict(run)).toBe(false);
  run.phase = 'Failed';
  run.task_decisions = { skipped: { skipped: true, reason: 'missing_job_reference' } };
  expect(workflowOutcomeConflict(run)).toBe(false);
  client.dispose();
});

test('historical schema accepts old responses without inventing pinned evidence', async () => {
  const client = demoClient();
  const modern = await client.workflowRun('412', '94');
  const old = structuredClone(modern);
  delete old.tasks;
  delete old.task_decisions;
  delete old.pinned_revision_available;
  old.steps = old.steps.map((step) => {
    const legacy = { ...step };
    delete legacy.task_name;
    return legacy;
  });
  const legacy = workflowRunDetailSchema.parse(old);
  expect(hasPinnedGraph(legacy)).toBe(false);
  expect(legacy.tasks).toBeUndefined();
  expect(legacy.task_decisions).toBeUndefined();
  expect(stepsForTask(legacy, 'extract')).toEqual([]);
  client.dispose();
});

test('pinned demo history differs from current definition and preserves skip reasons', async () => {
  const client = demoClient();
  const current = await client.workflow('412');
  const run = workflowRunDetailSchema.parse(await client.workflowRun('412', '94'));
  expect(run.revision_id).toBe(398);
  expect(hasPinnedGraph(run)).toBe(true);
  expect(run.tasks?.map((task) => task.name)).toEqual(['extract', 'validate', 'publish']);
  expect(current.tasks.map((task) => task.name)).toContain('reconcile');
  expect(run.task_decisions?.publish).toEqual({ skipped: true, reason: 'fail_fast' });
  expect(pinnedTaskLayers(run.tasks!)).toEqual(run.tasks!.map((task) => [task]));
  client.dispose();
});

test('task associations require exact recorded task_name despite shared source and array ordering', async () => {
  const client = demoClient();
  const run = workflowRunDetailSchema.parse(await client.workflowRun('412', '94'));
  const sample = run.steps[0];
  const reversed = {
    ...run,
    steps: [
      { ...sample, id: 2, task_name: 'publish' },
      { ...sample, id: 3, task_name: undefined },
      { ...sample, id: 1, task_name: 'extract' },
    ],
  };
  expect(stepsForTask(reversed, 'extract').map((step) => step.id)).toEqual([1]);
  expect(stepsForTask(reversed, 'publish').map((step) => step.id)).toEqual([2]);
  expect(stepsForTask(reversed, 'validate')).toEqual([]);
  const unavailable = workflowRunDetailSchema.parse({
    ...run,
    tasks: null,
    pinned_revision_available: false,
  });
  expect(hasPinnedGraph(unavailable)).toBe(false);
  expect(unavailable.task_decisions?.publish.reason).toBe('fail_fast');
  client.dispose();
});

test('demo server search and exclusive history window filter before pagination', async () => {
  const client = demoClient();
  expect(
    (await client.workflows({ search: 'WAREHOUSE', offset: 0 })).map((item) => item.name),
  ).toEqual(['warehouse-refresh']);
  const history = await client.workflowRuns('412');
  const oldest = history.at(-1)!.created_at;
  const newest = history[0].created_at;
  expect(await client.workflowRuns('412', { created_before: oldest })).toEqual([]);
  expect(
    await client.workflowRuns('412', { created_before: `${newest.slice(0, -1)}000001Z` }),
  ).toHaveLength(3);
  expect(
    (
      await client.workflowRuns('412', {
        created_from: oldest,
        offset: 1,
        limit: 1,
      })
    ).map((item) => item.id),
  ).toEqual([92]);
  expect(
    (await client.functions({ search: 'search', offset: 0 })).map((item) => item.name),
  ).toEqual(['rebuild-search-index']);
  const invocations = await client.functionRuns('27');
  expect(
    await client.functionRuns('27', { created_before: invocations.at(-1)!.triggered_at }),
  ).toEqual([]);
  client.dispose();
});

test('sample workflow step sources and attempts agree with the retained execution', async () => {
  const client = demoClient();
  try {
    const [jobs, workflows, failed, completed, invocations] = await Promise.all([
      client.jobs(),
      client.workflows(),
      client.workflowRun('412', '94'),
      client.workflowRun('412', '92'),
      client.functionRuns('27'),
    ]);
    const definitions = await Promise.all(
      workflows.map((workflow) => client.workflow(String(workflow.id))),
    );
    for (const workflow of definitions)
      for (const task of workflow.tasks)
        expect(
          jobs.some((job) => job.namespace === workflow.namespace && job.name === task.job_ref),
        ).toBe(true);
    for (const history of [failed, completed]) {
      const steps = await Promise.all(history.steps.map((step) => client.run(step.id)));
      for (const [index, recorded] of history.steps.entries()) {
        const expectedJob = history.tasks?.find(
          (task) => task.name === recorded.task_name,
        )?.job_ref;
        expect(jobs.find((job) => job.source_uid === steps[index].source_uid)?.name).toBe(
          expectedJob,
        );
        expect(steps[index].trigger).toBe('Workflow');
        expect(recorded.created_at >= history.created_at).toBe(true);
        expect(recorded.updated_at <= history.updated_at).toBe(true);
      }
    }
    expect(completed.steps).toHaveLength(completed.tasks!.length);
    const failedLog = await client.attemptLogs({ kind: 'instance', run: '1040' }, 1, 100);
    expect(failedLog.text).toContain('billing-extract attempt 1');
    expect(failedLog.text).toContain('status 1');
    expect(invocations[0].duration_ms).toBe(
      Date.parse(invocations[0].finished_at!) - Date.parse(invocations[0].started_at!),
    );
  } finally {
    client.dispose();
  }
});
