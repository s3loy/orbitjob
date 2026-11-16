import { z } from 'zod';
import type { components } from './schema';
import { id, receiptSchema } from './contracts';
import type { Request } from './monitoring';

type S = components['schemas'];
export type Workflow = S['WorkflowListItem'] & { time_zone?: string; concurrency_policy?: string };
export type WorkflowDetail = S['WorkflowGetItem'] & {
  time_zone?: string;
  concurrency_policy?: string;
};
export type WorkflowRun = S['WorkflowRunItem'] & { scheduled_for?: string | null; reason?: string };
type HistoricalFields = 'tasks' | 'task_decisions' | 'pinned_revision_available';
export type WorkflowTaskSnapshot = {
  source_uid: string;
  revision_id: z.infer<typeof id>;
  namespace: string;
  name: string;
  image: string;
};
export type WorkflowRunDetail = Omit<
  S['WorkflowRunGetItem'],
  HistoricalFields | 'snapshot_version' | 'task_snapshots'
> &
  Partial<Pick<S['WorkflowRunGetItem'], HistoricalFields>> & {
    snapshot_version?: number;
    task_snapshots?: Record<string, WorkflowTaskSnapshot>;
  };
export type FunctionDefinition = S['FunctionItem'];
export type FunctionInput = Pick<
  FunctionDefinition,
  | 'name'
  | 'description'
  | 'status'
  | 'image'
  | 'command'
  | 'args'
  | 'timeout_seconds'
  | 'retry_limit'
  | 'execution_policy_version'
  | 'retry_safe'
  | 'resources'
  | 'history_success'
  | 'history_failed'
  | 'labels'
>;
export type FunctionRun = S['FunctionRunItem'];
export type M4Receipt = S['FunctionInvokeResult'];
export type CancelReceipt = S['WorkflowCancelResult'];
export type M4ListFilter = { offset?: number; search?: string; limit?: number };
export type M4HistoryFilter = {
  offset?: number;
  limit?: number;
  created_from?: string;
  created_before?: string;
};
export interface M4Client {
  workflows(filter?: number | M4ListFilter): Promise<Workflow[]>;
  previewWorkflowSchedule(input: {
    schedule: string;
    time_zone: string;
    count?: number;
  }): Promise<{ time_zone: string; upcoming: string[] }>;
  workflow(id: string): Promise<WorkflowDetail>;
  workflowRuns(id: string, filter?: number | M4HistoryFilter): Promise<WorkflowRun[]>;
  workflowRun(id: string, runId: string): Promise<WorkflowRunDetail>;
  triggerWorkflow(id: string, key: string): Promise<M4Receipt>;
  cancelWorkflow(id: string, runId: string): Promise<CancelReceipt>;
  functions(filter?: number | M4ListFilter): Promise<FunctionDefinition[]>;
  functionDefinition(id: string): Promise<FunctionDefinition>;
  functionRuns(id: string, filter?: number | M4HistoryFilter): Promise<FunctionRun[]>;
  functionRun(id: string, runId: string): Promise<FunctionRun>;
  invokeFunction(id: string, key: string, expectedVersion?: number): Promise<M4Receipt>;
  createFunction(input: FunctionInput, key: string): Promise<FunctionDefinition>;
  updateFunction(
    id: string,
    input: FunctionInput & { expected_version: number },
  ): Promise<FunctionDefinition>;
}
const integer = z.number().int().refine(Number.isSafeInteger);
const date = z.string().datetime({ offset: true });
export const workflowSchema = z.object({
  id,
  name: z.string(),
  namespace: z.string(),
  source_uid: z.string(),
  generation: id,
  schedule: z.string(),
  time_zone: z.string().default('UTC'),
  concurrency_policy: z.string().default('Allow'),
  suspend: z.boolean(),
  fail_policy: z.string(),
  timeout_seconds: integer,
  task_count: integer,
  created_at: date,
}) satisfies z.ZodType<Workflow>;
export const workflowTaskSchema = z.object({
  name: z.string(),
  job_ref: z.string(),
  depends_on: z.array(z.string()),
  condition: z
    .object({
      type: z.string(),
      rules: z.array(
        z.object({
          task: z.string(),
          field: z.string(),
          operator: z.string(),
          values: z.array(z.string()).optional(),
          number: z.union([integer, z.string().regex(/^-?\d+$/)]).optional(),
        }),
      ),
    })
    .optional(),
});
export const workflowDetailSchema = workflowSchema.extend({
  tasks: z.array(workflowTaskSchema),
}) satisfies z.ZodType<WorkflowDetail>;
export const workflowRunSchema = z.object({
  id,
  workflow_source_uid: z.string(),
  revision_id: id,
  occurrence_key: z.string(),
  trigger: z.string(),
  actor: z.string(),
  phase: z.string(),
  scheduled_for: z.string().nullable().optional(),
  reason: z.string().optional(),
  created_at: date,
  updated_at: date,
}) satisfies z.ZodType<WorkflowRun>;
export const workflowRunDetailSchema = workflowRunSchema.extend({
  snapshot_version: integer.optional(),
  task_snapshots: z
    .record(
      z.string(),
      z.object({
        source_uid: z.string(),
        revision_id: id,
        namespace: z.string(),
        name: z.string(),
        image: z.string(),
      }),
    )
    .optional(),
  tasks: z.array(workflowTaskSchema).nullable().optional(),
  task_decisions: z
    .record(z.string(), z.object({ skipped: z.boolean(), reason: z.string().optional() }))
    .optional(),
  pinned_revision_available: z.boolean().optional(),
  steps: z.array(
    z.object({
      id,
      source_uid: z.string(),
      task_name: z.string().optional(),
      occurrence_key: z.string(),
      phase: z.string(),
      attempt: integer,
      created_at: date,
      updated_at: date,
    }),
  ),
}) satisfies z.ZodType<WorkflowRunDetail>;
export const functionSchema = z.object({
  id,
  tenant_id: z.string(),
  resource_group_id: z.string(),
  name: z.string(),
  description: z.string(),
  status: z.string(),
  image: z.string(),
  command: z.array(z.string()),
  args: z.array(z.string()),
  timeout_seconds: integer,
  retry_limit: integer,
  execution_policy_version: integer.optional(),
  retry_safe: z.boolean().optional(),
  resources: z
    .object({
      requests: z.record(z.string(), z.string()).optional(),
      limits: z.record(z.string(), z.string()).optional(),
    })
    .optional(),
  history_success: integer,
  history_failed: integer,
  labels: z.record(z.string(), z.unknown()),
  version: integer,
  created_at: date,
  updated_at: date,
  active_revision_id: id.nullable().optional(),
  active_revision_version: integer.nullable().optional(),
  ready: z.boolean().default(false),
}) satisfies z.ZodType<FunctionDefinition>;
export const functionRunSchema = z.object({
  id,
  run_id: z.string(),
  occurrence_key: z.string().nullable().optional(),
  function_id: id,
  status: z.string(),
  triggered_at: date,
  started_at: date.nullable().optional(),
  finished_at: date.nullable().optional(),
  duration_ms: integer.nullable().optional(),
  attempts: z
    .array(
      z.object({
        attempt_number: integer.positive(),
        phase: z.string(),
        started_at: date.nullable(),
        completed_at: date.nullable(),
      }),
    )
    .nullable()
    .default(null),
  last_attempt: integer.positive().nullable().default(null),
  version: integer,
  created_at: date,
}) satisfies z.ZodType<FunctionRun>;
const cancelSchema = z.object({
  name: z.string(),
  namespace: z.string(),
  occurrence_key: z.string(),
  phase: z.string(),
}) satisfies z.ZodType<CancelReceipt>;
export function m4Client(request: Request, pageSize: number): M4Client {
  const part = encodeURIComponent;
  const wf = (id: string) => `/api/v1/workflows/${part(id)}`;
  const fn = (id: string) => `/api/v1/functions/${part(id)}`;
  const list = async <T>(
    path: string,
    schema: z.ZodType<T>,
    filter: number | M4ListFilter | M4HistoryFilter = 0,
  ) => {
    const input = typeof filter === 'number' ? { offset: filter } : filter;
    const params = new URLSearchParams({
      limit: String(('limit' in input && input.limit) || pageSize),
      offset: String(input.offset ?? 0),
    });
    if ('search' in input && input.search) params.set('search', input.search);
    if ('created_from' in input && input.created_from)
      params.set('created_from', input.created_from);
    if ('created_before' in input && input.created_before)
      params.set('created_before', input.created_before);
    return (await request(`${path}?${params}`, z.object({ items: z.array(schema) }))).items;
  };
  return {
    previewWorkflowSchedule: (input) =>
      request(
        '/api/v1/workflows/schedule-preview',
        z.object({ time_zone: z.string(), upcoming: z.array(date) }),
        'POST',
        undefined,
        input,
      ),
    workflows: (offset) => list('/api/v1/workflows', workflowSchema, offset),
    workflow: (id) => request(wf(id), workflowDetailSchema),
    workflowRuns: (id, offset) => list(`${wf(id)}/runs`, workflowRunSchema, offset),
    workflowRun: (id, runId) => request(`${wf(id)}/runs/${part(runId)}`, workflowRunDetailSchema),
    triggerWorkflow: (id, key) => request(`${wf(id)}/runs`, receiptSchema, 'POST', key),
    cancelWorkflow: (id, runId) =>
      request(`${wf(id)}/runs/${part(runId)}/cancel`, cancelSchema, 'POST'),
    functions: (offset) => list('/api/v1/functions', functionSchema, offset),
    functionDefinition: (id) => request(fn(id), functionSchema),
    functionRuns: async (id, filter = 20) => {
      if (typeof filter !== 'number') return list(`${fn(id)}/runs`, functionRunSchema, filter);
      return (
        await request(
          `${fn(id)}/runs?limit=${filter}`,
          z.object({ items: z.array(functionRunSchema) }),
        )
      ).items;
    },
    functionRun: (id, runId) => request(`${fn(id)}/runs/${part(runId)}`, functionRunSchema),
    invokeFunction: (id, key, expectedVersion) =>
      request(
        `${fn(id)}/invoke?wait_seconds=0${expectedVersion ? `&expected_version=${expectedVersion}` : ''}`,
        receiptSchema,
        'POST',
        key,
      ),
    createFunction: (input, key) =>
      request('/api/v1/functions', functionSchema, 'POST', key, input),
    updateFunction: (id, input) => request(fn(id), functionSchema, 'PUT', undefined, input),
  };
}
