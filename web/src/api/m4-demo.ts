import { randomUUID } from '../uuid';
import { sampleWorkspace } from '../sample-workspace';
import { sampleSearchGroup } from './administration-demo';
import type {
  FunctionDefinition,
  FunctionRun,
  M4Client,
  M4Receipt,
  WorkflowDetail,
  WorkflowRunDetail,
  M4HistoryFilter,
  M4ListFilter,
} from './m4';
import { APIError } from './transport';
import { timestampNanos } from './query-window';
import type { RunDetail } from './contracts';
export function m4Demo(
  answer: <T>(value: T) => Promise<T>,
  ledger: RunDetail[],
  base: number,
): M4Client {
  const at = (time: number) => new Date(time).toISOString();
  const createdAt = at(base - 86_400_000);
  const workflowDefinitions: Pick<
    WorkflowDetail,
    'id' | 'name' | 'namespace' | 'schedule' | 'suspend' | 'tasks'
  >[] = [
    {
      id: 412,
      name: 'billing-close',
      namespace: 'finance-prod',
      schedule: '15 6 * * *',
      suspend: false,
      tasks: [
        { name: 'extract', job_ref: 'billing-extract', depends_on: [] },
        { name: 'validate', job_ref: 'billing-validate', depends_on: ['extract'] },
        { name: 'reconcile', job_ref: 'billing-reconciliation', depends_on: ['extract'] },
        {
          name: 'publish',
          job_ref: 'billing-publish',
          depends_on: ['validate', 'reconcile'],
          condition: {
            type: 'all',
            rules: [
              { task: 'validate', field: 'phase', operator: 'eq', values: ['Succeeded'] },
              { task: 'reconcile', field: 'phase', operator: 'eq', values: ['Succeeded'] },
            ],
          },
        },
      ],
    },
    {
      id: 405,
      name: 'warehouse-refresh',
      namespace: 'data-pipelines',
      schedule: '5 2 * * *',
      suspend: false,
      tasks: [
        { name: 'sync', job_ref: 'warehouse-sync', depends_on: [] },
        { name: 'reindex', job_ref: 'search-reindex', depends_on: ['sync'] },
      ],
    },
    {
      id: 401,
      name: 'daily-export',
      namespace: 'finance-prod',
      schedule: '30 7 * * *',
      suspend: true,
      tasks: [{ name: 'export', job_ref: 'invoice-export', depends_on: [] }],
    },
  ];
  const workflows: WorkflowDetail[] = workflowDefinitions.map((definition, index) => ({
    ...definition,
    source_uid: `16fa93bc-cffd-4e71-9066-${String(index + 1).padStart(12, '0')}`,
    generation: 3,
    time_zone: 'UTC',
    concurrency_policy: 'Forbid',
    fail_policy: 'fail_fast',
    timeout_seconds: 900,
    task_count: definition.tasks.length,
    created_at: definition.id === 412 ? at(base - 4 * 3_600_000) : createdAt,
  }));
  const functionDefinitions = [
    {
      id: 27,
      name: 'rebuild-search-index',
      description: 'Rebuild the product catalog after a search schema change.',
      image: 'registry.example.com/commerce/search-indexer:2.8.1',
      command: ['/app/indexer'],
      args: ['rebuild', '--catalog=storefront'],
      resource_group_id: sampleSearchGroup.id,
    },
    {
      id: 24,
      name: 'generate-statement',
      description: 'Generate the previous month’s merchant settlement statement.',
      image: 'registry.example.com/commerce/merchant-statements:1.7.3',
      command: ['/app/statements'],
      args: ['generate', '--period=previous-month'],
      resource_group_id: '',
    },
    {
      id: 21,
      name: 'cleanup-temp-files',
      description: 'Remove temporary export files older than seven days.',
      image: 'registry.example.com/commerce/export-maintenance:1.3.2',
      command: ['/app/export-maintenance'],
      args: ['prune', '--older-than=7d'],
      resource_group_id: '',
    },
  ];
  const functions: FunctionDefinition[] = functionDefinitions.map((definition) => ({
    ...definition,
    ready: definition.id !== 21,
    active_revision_version: 7,
    active_revision_id: definition.id,
    tenant_id: sampleWorkspace.id,
    status: definition.id === 21 ? 'paused' : 'active',
    timeout_seconds: 120,
    retry_limit: 2,
    history_success: 20,
    history_failed: 50,
    labels: {},
    version: 7,
    created_at: createdAt,
    updated_at: createdAt,
  }));
  function billingStep(id: number, task_name: string) {
    const step = ledger.find((record) => record.id === id)!;
    return {
      task_name,
      id: step.id,
      source_uid: step.source_uid,
      occurrence_key: step.occurrence_key,
      phase: step.phase,
      attempt: step.attempt,
      created_at: step.created_at,
      updated_at: step.updated_at,
    };
  }
  const billing = workflows[0];
  const billingRuns = [
    { id: 93, phase: 'Running', steps: [billingStep(1038, 'extract')] },
    {
      id: 92,
      phase: 'Succeeded',
      steps: [
        billingStep(1041, 'extract'),
        billingStep(1007, 'validate'),
        billingStep(1006, 'reconcile'),
        billingStep(1005, 'publish'),
      ],
    },
    { id: 94, phase: 'Failed', steps: [billingStep(1040, 'extract')] },
  ];
  const failedBillingDecisions: NonNullable<WorkflowRunDetail['task_decisions']> = {
    validate: { skipped: true, reason: 'fail_fast' },
    publish: { skipped: true, reason: 'fail_fast' },
  };
  const runs = billingRuns.map<WorkflowRunDetail>(({ id, phase, steps }) => ({
    id,
    workflow_source_uid: billing.source_uid,
    revision_id: id === 94 ? 398 : billing.id,
    occurrence_key: `${billing.name}-${id}`,
    trigger: 'Manual',
    actor: 'operations-console',
    phase,
    created_at: at(Date.parse(steps[0].created_at) - 2000),
    updated_at: steps.at(-1)!.updated_at,
    pinned_revision_available: true,
    tasks:
      id === 94
        ? [
            { name: 'extract', job_ref: 'billing-extract', depends_on: [] },
            { name: 'validate', job_ref: 'billing-validate-v2', depends_on: ['extract'] },
            { name: 'publish', job_ref: 'billing-publish-v2', depends_on: ['validate'] },
          ]
        : structuredClone(billing.tasks),
    task_decisions: id === 94 ? failedBillingDecisions : {},
    steps,
  }));
  const outcomes: FunctionRun[] = ['success', 'failed', 'canceled'].map((status, i) => {
    const triggered = base - i * 30 * 60_000;
    const started = status === 'canceled' ? null : triggered + 750;
    const duration = 2160 + i * 100;
    const finished = started === null ? triggered + 300 : started + duration;
    return {
      id: 80 - i,
      run_id: `019a0371-d232-7000-8000-${String(i + 1).padStart(12, '0')}`,
      function_id: 27,
      status,
      triggered_at: at(triggered),
      started_at: started === null ? null : at(started),
      finished_at: at(finished),
      duration_ms: started === null ? null : duration,
      attempts:
        started === null
          ? []
          : [
              {
                attempt_number: 1,
                phase: status === 'success' ? 'Succeeded' : 'Failed',
                started_at: at(started),
                completed_at: at(finished),
              },
            ],
      last_attempt: started === null ? null : 1,
      version: 1,
      created_at: at(finished),
    };
  });
  const createdFunctions = new Map<string, { input: string; id: string }>();
  const receipts = new Map<string, M4Receipt>();
  const accepted = new Map<string, number>();
  const canceled = new Map<string, number>();
  let sequence = 100;
  function missing(): never {
    throw new APIError('This sample record does not exist.', 404, 'NOT_FOUND');
  }
  function advance() {
    for (const [key, started] of accepted) {
      if (Date.now() - started < 3500) continue;
      const r = receipts.get(key)!;
      if (r.trigger === 'Function' && !outcomes.some((o) => o.run_id === r.occurrence_key))
        outcomes.unshift({
          id: sequence++,
          run_id: r.occurrence_key,
          function_id: Number(r.name.split('-')[1]),
          status: 'success',
          triggered_at: new Date(started).toISOString(),
          started_at: new Date(started).toISOString(),
          finished_at: new Date(started + 3000).toISOString(),
          duration_ms: 3000,
          attempts: [
            {
              attempt_number: 1,
              phase: 'Succeeded',
              started_at: new Date(started).toISOString(),
              completed_at: new Date(started + 3000).toISOString(),
            },
          ],
          last_attempt: 1,
          version: 1,
          created_at: new Date(started + 3000).toISOString(),
        });
    }
    for (const r of runs) {
      const at = canceled.get(String(r.id));
      if (at) {
        r.phase = Date.now() - at > 2500 ? 'Canceled' : 'CancelRequested';
        for (const step of r.steps) {
          const stored = ledger.find((candidate) => candidate.id === step.id);
          if (stored) {
            stored.phase = r.phase;
            stored.updated_at = new Date().toISOString();
            for (const attempt of stored.attempts)
              if (!attempt.completed_at) {
                attempt.phase = r.phase;
                if (r.phase === 'Canceled') attempt.completed_at = stored.updated_at;
              }
          }
        }
      }
      r.steps = r.steps.map((step) => {
        const stored = ledger.find((candidate) => candidate.id === step.id);
        return stored
          ? { ...step, phase: stored.phase, attempt: stored.attempt, updated_at: stored.updated_at }
          : step;
      });
    }
  }
  function publish(kind: 'Function' | 'Manual', id: string, key: string) {
    const existing = receipts.get(key);
    if (existing) return answer({ ...existing, created: false });
    const target =
      kind === 'Function'
        ? functions.find((f) => String(f.id) === id)
        : workflows.find((w) => String(w.id) === id);
    if (!target) missing();
    if ('status' in target && target.status === 'paused')
      throw new APIError('Definition is paused.', 409, 'CONFLICT');
    const receipt = {
      namespace: 'namespace' in target ? target.namespace : 'platform',
      name: kind === 'Function' ? `function-${id}-${sequence++}` : `${target.name}-${sequence++}`,
      occurrence_key: randomUUID(),
      trigger: kind,
      phase: '',
      created: true,
    };
    receipts.set(key, receipt);
    accepted.set(key, Date.now());
    if (kind === 'Manual')
      runs.unshift({
        id: sequence++,
        workflow_source_uid: (target as WorkflowDetail).source_uid,
        revision_id: target.id,
        occurrence_key: receipt.occurrence_key,
        trigger: 'Manual',
        actor: 'operations-console',
        phase: 'Running',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        steps: [],
        pinned_revision_available: true,
        tasks: structuredClone((target as WorkflowDetail).tasks),
        task_decisions: {},
      });
    return answer(receipt);
  }
  function page<T>(rows: T[], filter: number | M4ListFilter = 0, text: (row: T) => string) {
    const input = typeof filter === 'number' ? { offset: filter } : filter;
    return rows
      .filter(
        (row) => !input.search || text(row).toLowerCase().includes(input.search.toLowerCase()),
      )
      .slice(input.offset ?? 0, (input.offset ?? 0) + (input.limit ?? 20));
  }
  function history<T>(rows: T[], filter: M4HistoryFilter, time: (row: T) => string) {
    return rows
      .filter(
        (row) =>
          (!filter.created_from ||
            timestampNanos(time(row))! >= timestampNanos(filter.created_from)!) &&
          (!filter.created_before ||
            timestampNanos(time(row))! < timestampNanos(filter.created_before)!),
      )
      .slice(filter.offset ?? 0, (filter.offset ?? 0) + (filter.limit ?? 20));
  }
  return {
    previewWorkflowSchedule: async () => {
      throw new APIError(
        'Server-validated schedule previews are available in an installed console.',
        400,
        'DEMO_PREVIEW',
      );
    },
    workflows: (filter = 0) =>
      answer(page(workflows, filter, (row) => `${row.name} ${row.namespace}`)),
    workflow: (id) => answer(workflows.find((w) => String(w.id) === id) ?? missing()),
    workflowRuns: (id, filter = 0) => {
      advance();
      const rows = runs.filter(
        (row) => row.workflow_source_uid === workflows.find((w) => String(w.id) === id)?.source_uid,
      );
      return answer(
        history(
          rows,
          typeof filter === 'number' ? { offset: filter } : filter,
          (row) => row.created_at,
        ),
      );
    },
    workflowRun: (id, runId) => {
      advance();
      return answer(
        runs.find(
          (r) =>
            String(r.id) === runId &&
            r.workflow_source_uid === workflows.find((w) => String(w.id) === id)?.source_uid,
        ) ?? missing(),
      );
    },
    triggerWorkflow: (id, key) => publish('Manual', id, key),
    cancelWorkflow: async (id, runId) => {
      const r =
        runs.find(
          (r) =>
            String(r.id) === runId &&
            r.workflow_source_uid === workflows.find((w) => String(w.id) === id)?.source_uid,
        ) ?? missing();
      const phase = r.phase;
      if (!['Succeeded', 'Failed', 'Canceled'].includes(phase)) canceled.set(runId, Date.now());
      return answer({
        namespace: workflows.find((workflow) => workflow.source_uid === r.workflow_source_uid)!
          .namespace,
        name: `workflow-run-${runId}`,
        occurrence_key: r.occurrence_key,
        phase,
      });
    },
    createFunction: async (input, key) => {
      const existing = createdFunctions.get(key);
      if (existing && existing.input !== JSON.stringify(input))
        throw new APIError('This request identity belongs to another definition.', 409, 'CONFLICT');
      if (existing) return answer(functions.find((item) => String(item.id) === existing.id)!);
      const now = new Date().toISOString();
      const item = {
        ...input,
        id: sequence++,
        tenant_id: sampleWorkspace.id,
        resource_group_id: '',
        version: 1,
        created_at: now,
        updated_at: now,
        active_revision_id: sequence++,
        active_revision_version: 1,
        ready: input.status === 'active',
      };
      functions.unshift(item);
      createdFunctions.set(key, { input: JSON.stringify(input), id: String(item.id) });
      return answer(item);
    },
    updateFunction: async (id, input) => {
      const item = functions.find((item) => String(item.id) === id) ?? missing();
      if (item.version !== input.expected_version)
        throw new APIError('This definition changed. Reload it before saving.', 409, 'CONFLICT');
      const { expected_version, ...definition } = input;
      if (expected_version !== item.version)
        throw new APIError('Refresh this version before editing.', 409, 'CONFLICT');
      Object.assign(item, definition, {
        version: item.version + 1,
        active_revision_version: item.version + 1,
        ready: definition.status === 'active',
        updated_at: new Date().toISOString(),
      });
      return answer({
        ...item,
        active_revision_version: item.version,
        ready: item.status === 'active',
      });
    },
    functions: (filter = 0) =>
      answer(page(functions, filter, (row) => `${row.name} ${row.description}`)),
    functionDefinition: (id) => answer(functions.find((f) => String(f.id) === id) ?? missing()),
    functionRuns: (id, filter = 20) => {
      advance();
      const rows = outcomes.filter((row) => String(row.function_id) === id);
      return answer(
        history(
          rows,
          typeof filter === 'number' ? { limit: filter } : filter,
          (row) => row.triggered_at,
        ),
      );
    },
    functionRun: async (id, runId) =>
      answer(outcomes.find((o) => String(o.function_id) === id && o.run_id === runId) ?? missing()),
    invokeFunction: (id, key, expectedVersion) => {
      const item = functions.find((item) => String(item.id) === id) ?? missing();
      if (expectedVersion && item.version !== expectedVersion)
        throw new APIError('Reviewed version is no longer current.', 409, 'CONFLICT');
      return publish('Function', id, key);
    },
  };
}
