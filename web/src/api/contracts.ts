import { z } from 'zod';
import type { components } from './schema';

export type Job = components['schemas']['ListItem'];
export type JobDetail = components['schemas']['GetItem'];
export type Run = components['schemas']['RunListItem'];
export type RunDetail = components['schemas']['RunGetItem'];
export type Receipt = components['schemas']['TriggerResult'];
export type ID = Job['id'];

export const id = z.union([
  z.number().int().positive().refine(Number.isSafeInteger),
  z.string().regex(/^[1-9]\d*$/),
]);
const date = z.string().datetime({ offset: true });
export const jobSchema = z.object({
  id,
  tenant_id: z.string(),
  source_uid: z.string(),
  generation: id,
  name: z.string(),
  namespace: z.string(),
  actor: z.string(),
  schedule: z.string(),
  schedule_summary: z.string(),
  source_kind: z.string().optional(),
  suspend: z.boolean(),
  concurrency_policy: z.string(),
  misfire_policy: z.string(),
  created_at: date,
}) satisfies z.ZodType<Job>;
export const jobDetailSchema: z.ZodType<JobDetail> = z.object({
  ...jobSchema.shape,
  source_mode: z.string(),
  spec_hash: z.string(),
  timeout_seconds: z.number(),
  retry_max_attempts: z.number(),
  job_template: z.object({
    image: z.string(),
    command: z.array(z.string()).optional(),
    args: z.array(z.string()).optional(),
    backoff_limit: z.number(),
  }),
});
export const runSchema = z.object({
  id,
  tenant_id: z.string(),
  source_uid: z.string(),
  revision_id: id,
  source_name: z.string().nullable().optional(),
  source_namespace: z.string().nullable().optional(),
  source_kind: z.string().nullable().optional(),
  occurrence_key: z.string(),
  trigger: z.string(),
  actor: z.string(),
  phase: z.string(),
  attempt: z.number().int(),
  max_attempts: z.number().int(),
  created_at: date,
  updated_at: date,
}) satisfies z.ZodType<Run>;
export const runDetailSchema: z.ZodType<RunDetail> = z.object({
  ...runSchema.shape,
  attempts: z.array(
    z.object({
      id,
      run_id: id,
      attempt_number: z.number().int(),
      phase: z.string(),
      kubernetes_job_name: z.string(),
      kubernetes_job_uid: z.string().nullable().optional(),
      observed_resource_version: z.string().nullable().optional(),
      owner_run_uid: z.string().nullable().optional(),
      execution_unknown_since: date.nullable().optional(),
      execution_unknown_reason: z.string().nullable().optional(),
      started_at: date.nullable().optional(),
      completed_at: date.nullable().optional(),
      created_at: date,
    }),
  ),
});
export const receiptSchema: z.ZodType<Receipt> = z.object({
  namespace: z.string(),
  name: z.string(),
  occurrence_key: z.string(),
  trigger: z.string(),
  phase: z.string(),
  created: z.boolean(),
});
export const jobsSchema = z.object({ items: z.array(jobSchema) });
export const runsSchema = z.object({ items: z.array(runSchema) });

export const phases = [
  'Pending',
  'CreatingAttempt',
  'Running',
  'RetryWaiting',
  'Succeeded',
  'Failed',
  'CancelRequested',
  'Canceled',
  'CancelUnknown',
  'ExecutionUnknown',
] as const;
export const terminal = (phase: string) => ['Succeeded', 'Failed', 'Canceled'].includes(phase);

export function correlate(rows: Run[], source: string, occurrence: string): Run | undefined {
  if (
    rows.some((row) => row.source_uid !== source || row.occurrence_key !== occurrence) ||
    rows.length > 1
  ) {
    throw new Error('The API did not return one exact occurrence. Automatic linking is disabled.');
  }
  return rows[0];
}

export const cancelSchema: z.ZodType<components['schemas']['CancelResult']> = z.object({
  namespace: z.string(),
  name: z.string(),
  occurrence_key: z.string(),
  phase: z.string(),
});
