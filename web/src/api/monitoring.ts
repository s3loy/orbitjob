import { z } from 'zod';
import type { components } from './schema';
import { id } from './contracts';

type S = components['schemas'];
export type Check = S['CheckListItem'];
export type CheckDetail = S['GetResult'];
export type CheckResult = S['CheckrunGetResult'];
export type SLI = S['SliGetItem'];
export type SLO = S['SloGetItem'];
export type Budget = S['SlobudgetListItem'];
export type Alert = S['SloalertGetItem'];
export type Resource = 'checks' | 'slis' | 'slos';
export type Filter = {
  offset?: number;
  limit?: number;
  status?: string;
  check_id?: string;
  slo_id?: string;
  search?: string;
};
export type CreateInputs = {
  checks: S['CreateCheckRequest'];
  slis: S['CreateSLIRequest'];
  slos: S['CreateSLORequest'];
};
export interface MonitoringClient {
  checks(filter?: Filter): Promise<Check[]>;
  check(id: string): Promise<CheckDetail>;
  checkResults(filter?: Filter): Promise<S['CheckrunListItem'][]>;
  checkResult(id: string): Promise<CheckResult>;
  slis(filter?: Filter): Promise<S['SliListItem'][]>;
  sli(id: string): Promise<SLI>;
  slos(filter?: Filter): Promise<S['SloListItem'][]>;
  slo(id: string): Promise<SLO>;
  budget(id: string): Promise<Budget>;
  budgets(id: string, filter?: Filter): Promise<Budget[]>;
  alerts(filter?: Filter): Promise<S['SloalertListItem'][]>;
  alert(id: string): Promise<Alert>;
  create<K extends Resource>(resource: K, input: CreateInputs[K]): Promise<{ id: number | string }>;
  change(
    resource: 'checks' | 'slos',
    id: string,
    action: 'pause' | 'resume',
    version: number,
  ): Promise<void>;
  remove(resource: Resource, id: string, version: number): Promise<void>;
}
const json = z.record(z.string(), z.unknown());
const version = z.number().int().positive().refine(Number.isSafeInteger);
const nullableDate = z.string().nullable().optional();
export const checkSchema = z.object({
  id,
  tenant_id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  check_type: z.string(),
  execution_policy_version: z.number().int().nonnegative(),
  schedule_type: z.string(),
  status: z.string(),
  next_run_at: nullableDate,
  version,
  created_at: z.string(),
}) satisfies z.ZodType<Check>;
export const checkDetailSchema = checkSchema.extend({
  check_config: json,
  assertion_rules: z.array(
    z.object({
      metric: z.string(),
      operator: z.string(),
      severity: z.string(),
      threshold: z.number(),
    }),
  ),
  labels: json,
  cron_expr: nullableDate,
  interval_sec: z.number().nullable().optional(),
  timezone: z.string(),
  timeout_sec: z.number(),
  retry_limit: z.number(),
  priority: z.number(),
  updated_at: z.string(),
}) satisfies z.ZodType<CheckDetail>;
export const resultSchema = z.object({
  id,
  check_id: id,
  run_id: z.string(),
  status: z.string(),
  severity: z.string().nullable().optional(),
  duration_ms: z.number().nullable().optional(),
  created_at: z.string(),
});
export const resultDetailSchema = resultSchema.extend({
  tenant_id: z.string(),
  version,
  scheduled_at: z.string(),
  started_at: nullableDate,
  finished_at: nullableDate,
  output: json,
  evaluation_result: json,
}) satisfies z.ZodType<CheckResult>;
export const sliSchema = z.object({
  id,
  name: z.string(),
  sli_type: z.string(),
  source_type: z.string(),
  aggregation: z.string(),
  version,
  created_at: z.string(),
});
export const sliDetailSchema = sliSchema.extend({
  description: z.string().nullable().optional(),
  source_config: json,
  good_event_criteria: json,
  updated_at: z.string(),
}) satisfies z.ZodType<SLI>;
export const budgetSchema = z.object({
  budget_total: z.number(),
  budget_consumed: z.number(),
  budget_remaining: z.number(),
  burn_rate: z.number(),
  status: z.string(),
  window_start: z.string(),
  window_end: z.string(),
}) satisfies z.ZodType<Budget>;
export const sloSchema = z.object({
  id,
  name: z.string(),
  sli_id: id,
  status: z.string(),
  target: z.number(),
  window_type: z.string(),
  window_duration: z.string(),
  alert_fast_burn_rate: z.number(),
  alert_slow_burn_rate: z.number(),
  version,
  created_at: z.string(),
});
export const sloDetailSchema = sloSchema.extend({
  description: z.string().nullable().optional(),
  current_budget: budgetSchema.optional(),
  updated_at: z.string(),
}) satisfies z.ZodType<SLO>;
export const alertSchema = z.object({
  id,
  slo_id: id,
  alert_type: z.string(),
  status: z.string(),
  burn_rate: z.number(),
  triggered_at: z.string(),
});
export const alertDetailSchema = alertSchema.extend({
  budget_id: id,
  resolved_at: nullableDate,
}) satisfies z.ZodType<Alert>;
export type Request = <T>(
  path: string,
  schema: z.ZodType<T>,
  method?: string,
  intent?: string,
  body?: unknown,
) => Promise<T>;
export function monitoringClient(request: Request, pageSize: number): MonitoringClient {
  function path(resource: string, filter: Filter = {}) {
    const params = new URLSearchParams({
      limit: String(pageSize),
      offset: String(filter.offset ?? 0),
    });
    for (const [k, v] of Object.entries(filter))
      if (v !== undefined && v !== '') params.set(k, String(v));
    return `/api/v1/${resource}?${params}`;
  }
  const list = async <T>(resource: string, schema: z.ZodType<T>, filter?: Filter) =>
    (await request(path(resource, filter), z.object({ items: z.array(schema) }))).items;
  return {
    checks: (f) => list('checks', checkSchema, f),
    check: (id) => request(`/api/v1/checks/${id}`, checkDetailSchema),
    checkResults: (f) => list('check-runs', resultSchema, f),
    checkResult: (id) => request(`/api/v1/check-runs/${id}`, resultDetailSchema),
    slis: (f) => list('slis', sliSchema, f),
    sli: (id) => request(`/api/v1/slis/${id}`, sliDetailSchema),
    slos: (f) => list('slos', sloSchema, f),
    slo: (id) => request(`/api/v1/slos/${id}`, sloDetailSchema),
    budget: (id) => request(`/api/v1/slos/${id}/budget`, budgetSchema),
    budgets: (id, f) => list(`slos/${id}/budgets`, budgetSchema, f),
    alerts: (f) => list('slo-alerts', alertSchema, f),
    alert: (id) => request(`/api/v1/slo-alerts/${id}`, alertDetailSchema),
    create: (resource, input) =>
      request(`/api/v1/${resource}`, z.object({ id }), 'POST', undefined, input),
    change: async (resource, id, action, version) => {
      await request(
        `/api/v1/${resource}/${id}/${action}`,
        z.object({ id: checkSchema.shape.id, status: z.string() }),
        'POST',
        undefined,
        { version },
      );
    },
    remove: async (resource, id, version) => {
      await request(`/api/v1/${resource}/${id}`, z.unknown(), 'DELETE', undefined, { version });
    },
  };
}
