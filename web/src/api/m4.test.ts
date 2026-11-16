import { afterEach, expect, test, vi } from 'vitest';
import { liveClient } from './transport';
import { demoClient } from './demo';
import {
  functionSchema,
  functionRunSchema,
  workflowDetailSchema,
  workflowRunDetailSchema,
} from './m4';
afterEach(() => vi.unstubAllGlobals());
test('Function attempt metadata preserves recovered attempts and distinguishes missing data from no attempts', async () => {
  const client = demoClient();
  const row = (await client.functionRuns('27'))[0];
  const parsed = functionRunSchema.parse({
    ...row,
    attempts: [
      { attempt_number: 1, phase: 'Failed', started_at: null, completed_at: null },
      { attempt_number: 2, phase: 'Succeeded', started_at: null, completed_at: null },
    ],
    last_attempt: 2,
  });
  expect(parsed.attempts?.map((attempt) => attempt.attempt_number)).toEqual([1, 2]);
  expect(parsed.last_attempt).toBe(2);
  expect(functionRunSchema.parse({ ...row, attempts: [], last_attempt: null }).attempts).toEqual(
    [],
  );
  const legacy = { ...row } as Record<string, unknown>;
  delete legacy.attempts;
  delete legacy.last_attempt;
  expect(functionRunSchema.parse(legacy)).toMatchObject({ attempts: null, last_attempt: null });
  expect(() => functionRunSchema.parse({ ...row, last_attempt: 0 })).toThrow();
  client.dispose();
});
test('M4 schemas retain promoted identities, numeric zero and lossless integer bounds', async () => {
  const client = demoClient();
  const workflow = await client.workflow('405');
  const [sync, reindex] = workflow.tasks;
  const reindexWithoutAttempts = {
    ...reindex,
    condition: {
      type: 'all',
      rules: [{ task: sync.name, field: 'attempts', operator: 'le', number: 0 }],
    },
  };
  const parsed = workflowDetailSchema.parse({ ...workflow, tasks: [sync, reindexWithoutAttempts] });
  expect(parsed.tasks[1].condition?.rules[0].number).toBe(0);
  expect(workflowDetailSchema.parse({ ...workflow, id: '9007199254740993' }).id).toBe(
    '9007199254740993',
  );
  const historical = workflowRunDetailSchema.parse(await client.workflowRun('412', '94'));
  expect(historical.revision_id).toBe(398);
  expect(historical.phase).toBe('Failed');
  client.dispose();
});
test('M4 paths keep Function outcome UUID separate and history has no offset', async () => {
  const fetcher = vi.fn().mockImplementation(async () => new Response('{"items":[]}'));
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('memory-key');
  await client.functionRuns('9007199254740993', 100);
  expect(fetcher.mock.calls[0][0]).toBe('/api/v1/functions/9007199254740993/runs?limit=100');
  await client.workflowRuns('412', 20);
  expect(fetcher.mock.calls[1][0]).toBe('/api/v1/workflows/412/runs?limit=20&offset=20');
  client.dispose();
});
test('M4 parameterless writes retain one intent identity and no invented ledger ID', async () => {
  const fetcher = vi.fn().mockImplementation(
    async () =>
      new Response(
        JSON.stringify({
          name: 'function-27-key',
          namespace: 'orbitjob',
          occurrence_key: 'server-key',
          phase: '',
          trigger: 'Function',
          created: true,
        }),
      ),
  );
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('memory-key');
  const receipt = await client.invokeFunction('27', 'client-key');
  expect(receipt).not.toHaveProperty('id');
  expect(fetcher.mock.calls[0][0]).toBe('/api/v1/functions/27/invoke?wait_seconds=0');
  expect(fetcher.mock.calls[0][1].headers['X-OrbitJob-Idempotency-Key']).toBe('client-key');
  client.dispose();
});
test('Function canceled before start preserves absent timing', async () => {
  const client = demoClient();
  const run = (await client.functionRuns('27')).find((r) => r.status === 'canceled')!;
  expect(functionRunSchema.parse(run)).toMatchObject({ started_at: null, duration_ms: null });
  client.dispose();
});

test('Demo Workflow steps and ordinary Runs share recorded identity and phase', async () => {
  const client = demoClient();
  for (const id of ['94', '93', '92']) {
    const workflow = await client.workflowRun('412', id);
    for (const step of workflow.steps) {
      const run = await client.run(step.id);
      expect(run).toMatchObject({
        phase: step.phase,
        source_uid: step.source_uid,
        occurrence_key: step.occurrence_key,
        attempt: step.attempt,
      });
    }
  }
  await client.cancelWorkflow('412', '93');
  const workflow = await client.workflowRun('412', '93');
  expect((await client.run(workflow.steps[0].id)).phase).toBe(workflow.steps[0].phase);
  client.dispose();
});

test('a saved Function without an execution projection retains nullable revision identities', async () => {
  const client = demoClient();
  const base = await client.functionDefinition('27');
  const pending = functionSchema.parse({
    ...base,
    active_revision_id: null,
    active_revision_version: null,
    ready: false,
  });
  expect(pending.active_revision_id).toBeNull();
  expect(pending.ready).toBe(false);
  client.dispose();
});
