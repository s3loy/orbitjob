import type {
  MonitoringClient,
  CheckDetail,
  CheckResult,
  SLI,
  SLO,
  Alert,
  Budget,
  Resource,
} from './monitoring';
import { APIError } from './transport';
import { sampleWorkspace } from '../sample-workspace';

export function monitoringDemo(answer: <T>(value: T) => Promise<T>): MonitoringClient {
  let next = 100;
  const minute = 60_000;
  const week = 7 * 24 * 60;
  const now = Math.floor(Date.now() / minute) * minute;
  const at = (minutes: number, ms = 0) => new Date(now + minutes * minute + ms).toISOString();
  const date = at(0);
  const opened = at(-week - 24 * 60);
  const cutover = at(-18);
  const checkDefinitions = [
    ['api-health', 'Probes the payments API used by billing reconciliation.', opened],
    ['checkout-ready', 'Probes the checkout service before it takes orders.', opened],
    ['gateway-health', 'Probes the replacement edge gateway before cutover.', cutover],
  ];
  const checks: CheckDetail[] = checkDefinitions.map(([name, description, createdAt], i) => ({
    id: i + 1,
    name,
    description,
    tenant_id: sampleWorkspace.id,
    check_type: 'http_health',
    execution_policy_version: 1,
    schedule_type: 'interval',
    interval_sec: 60,
    status: i === 2 ? 'paused' : 'active',
    version: 1,
    created_at: createdAt,
    updated_at: createdAt,
    check_config: { url: `https://${name}.example/health`, method: 'GET', expected_status: 200 },
    assertion_rules: [],
    labels: {},
    timezone: 'UTC',
    timeout_sec: 10,
    retry_limit: 2,
    priority: 0,
    next_run_at: i === 2 ? null : at(1),
  }));
  const resultSamples = [
    { id: 702, check: 1, offset: -1, status: 'success', duration: 240 },
    { id: 701, check: 1, offset: -2, status: 'failed', duration: 480 },
    { id: 700, check: 1, offset: -3, status: 'success', duration: 280 },
    { id: 699, check: 2, offset: -4, status: 'success', duration: 160 },
  ];
  const results: CheckResult[] = resultSamples.map(({ id, check, offset, status, duration }) => ({
    id,
    check_id: check,
    run_id: `7f82af00-9460-4dc0-8e38-${String(id).padStart(12, '0')}`,
    tenant_id: sampleWorkspace.id,
    status,
    duration_ms: duration,
    created_at: at(offset),
    scheduled_at: at(offset),
    started_at: at(offset, 150),
    finished_at: at(offset, 150 + duration),
    output: {},
    evaluation_result: {},
    version: 1,
  }));
  const indicators = [
    { id: 11, name: 'Payments API readiness', check: 1, createdAt: opened },
    { id: 12, name: 'Checkout successful probes', check: 2, createdAt: opened },
    { id: 13, name: 'Gateway successful probes', check: 3, createdAt: cutover },
  ];
  const slis: SLI[] = indicators.map(({ id, name, check, createdAt }) => ({
    id,
    name,
    sli_type: 'availability',
    source_type: 'job_run',
    source_config: { source_uid: `check-${check}` },
    aggregation: 'ratio',
    good_event_criteria: { status: 'success' },
    created_at: createdAt,
    updated_at: createdAt,
    version: 1,
  }));
  const objectives = [
    { id: 21, name: 'Payments API availability', indicator: 11, target: 0.9999, createdAt: opened },
    { id: 22, name: 'Checkout readiness', indicator: 12, target: 0.999, createdAt: opened },
    { id: 23, name: 'Checkout release gate', indicator: 12, target: 1, createdAt: opened },
    { id: 24, name: 'Gateway cutover readiness', indicator: 13, target: 0.999, createdAt: cutover },
  ];
  const slos: SLO[] = objectives.map(({ id, name, indicator, target, createdAt }) => ({
    id,
    name,
    sli_id: indicator,
    status: 'active',
    target,
    window_type: 'rolling',
    window_duration: '168h',
    alert_fast_burn_rate: 14.4,
    alert_slow_burn_rate: 6,
    created_at: createdAt,
    updated_at: createdAt,
    version: 1,
  }));
  // Each evaluated check recorded 10,000 probes in the seven-day window.
  const budgets = new Map<string, Budget>([
    [
      '21',
      {
        budget_total: 1,
        budget_consumed: 24,
        budget_remaining: -23,
        burn_rate: 24,
        status: 'exhausted',
        window_start: at(-week),
        window_end: date,
      },
    ],
    [
      '22',
      {
        budget_total: 10,
        budget_consumed: 3,
        budget_remaining: 7,
        burn_rate: 0.3,
        status: 'healthy',
        window_start: at(-week),
        window_end: date,
      },
    ],
    [
      '23',
      {
        budget_total: 0,
        budget_consumed: 3,
        budget_remaining: -3,
        burn_rate: 999999,
        status: 'exhausted',
        window_start: at(-week),
        window_end: date,
      },
    ],
  ]);
  const alerts: Alert[] = [
    {
      id: 31,
      slo_id: 21,
      budget_id: 41,
      status: 'active',
      alert_type: 'fast_burn',
      burn_rate: 15,
      triggered_at: at(-32),
    },
    {
      id: 32,
      slo_id: 21,
      budget_id: 40,
      status: 'active',
      alert_type: 'slow_burn',
      burn_rate: 7,
      triggered_at: at(-58),
    },
  ];
  const missing = (): never => {
    throw new APIError('This definition is unavailable.', 404, 'NOT_FOUND');
  };
  const find = <T extends { id: number | string }>(rows: T[], id: string): T =>
    rows.find((r) => String(r.id) === id) ?? missing();
  const rows = (r: Resource): (CheckDetail | SLI | SLO)[] =>
    r === 'checks' ? checks : r === 'slis' ? slis : slos;
  return {
    checks: (f = {}) =>
      answer(
        checks
          .filter(
            (r) =>
              (!f.status || r.status === f.status) &&
              (!f.search ||
                `${r.name} ${r.description ?? ''}`.toLowerCase().includes(f.search.toLowerCase())),
          )
          .slice(f.offset ?? 0, (f.offset ?? 0) + (f.limit ?? 20)),
      ),
    check: (id) => answer(find(checks, id)),
    checkResults: (f = {}) =>
      answer(
        results
          .filter(
            (r) =>
              (!f.check_id || String(r.check_id) === f.check_id) &&
              (!f.status || r.status === f.status),
          )
          .slice(f.offset ?? 0, (f.offset ?? 0) + (f.limit ?? 20)),
      ),
    checkResult: (id) => answer(find(results, id)),
    slis: (f = {}) =>
      answer(
        slis
          .filter(
            (item) =>
              !f.search ||
              `${item.name} ${item.description || ''}`
                .toLowerCase()
                .includes(f.search.toLowerCase()),
          )
          .slice(f.offset ?? 0, (f.offset ?? 0) + (f.limit ?? 20)),
      ),
    sli: (id) => answer(find(slis, id)),
    slos: (f = {}) =>
      answer(
        slos
          .filter(
            (r) =>
              (!f.status || r.status === f.status) &&
              (!f.search ||
                `${r.name} ${r.description ?? ''}`.toLowerCase().includes(f.search.toLowerCase())),
          )
          .slice(f.offset ?? 0, (f.offset ?? 0) + (f.limit ?? 20)),
      ),
    slo: (id) => answer(find(slos, id)),
    budget: (id) => {
      find(slos, id);
      return answer(
        budgets.get(id) ?? {
          budget_total: 0,
          budget_consumed: 0,
          budget_remaining: 0,
          burn_rate: 0,
          status: 'no_data',
          window_start: '',
          window_end: '',
        },
      );
    },
    budgets: (id, f = {}) => {
      find(slos, id);
      return answer(
        (budgets.has(id) ? [budgets.get(id)!] : []).slice(
          f.offset ?? 0,
          (f.offset ?? 0) + (f.limit ?? 20),
        ),
      );
    },
    alerts: (f = {}) =>
      answer(
        alerts
          .filter(
            (a) =>
              (!f.slo_id || String(a.slo_id) === f.slo_id) && (!f.status || a.status === f.status),
          )
          .slice(f.offset ?? 0, (f.offset ?? 0) + (f.limit ?? 20)),
      ),
    alert: (id) => answer(find(alerts, id)),
    create: async (resource, input) => {
      const common = {
        ...structuredClone(input),
        id: next++,
        version: 1,
        created_at: date,
        updated_at: date,
      };
      if (resource === 'checks')
        checks.unshift({
          tenant_id: sampleWorkspace.id,
          check_type: 'http_health',
          schedule_type: 'interval',
          status: 'active',
          check_config: {},
          labels: {},
          assertion_rules: [],
          timeout_sec: 10,
          retry_limit: 2,
          priority: 0,
          timezone: 'UTC',
          ...common,
          execution_policy_version: 1,
        } as CheckDetail);
      else if (resource === 'slis')
        slis.unshift({
          source_type: 'job_run',
          source_config: {},
          aggregation: 'ratio',
          good_event_criteria: {},
          ...common,
        } as SLI);
      else {
        find(slis, String((input as { sli_id: number | string }).sli_id));
        slos.unshift({
          status: 'active',
          alert_fast_burn_rate: 14.4,
          alert_slow_burn_rate: 6,
          window_type: 'rolling',
          ...common,
        } as SLO);
      }
      return answer({ id: common.id });
    },
    change: async (r, id, action, version) => {
      const item = find(rows(r), id);
      if (item.version !== version) throw new APIError('The definition changed.', 409, 'CONFLICT');
      if ('status' in item) item.status = action === 'pause' ? 'paused' : 'active';
      item.version++;
      await answer(undefined);
    },
    remove: async (r, id, version) => {
      const items = rows(r);
      const item = find(items, id);
      if (item.version !== version) throw new APIError('The definition changed.', 409, 'CONFLICT');
      items.splice(
        items.findIndex((i) => i === item),
        1,
      );
      await answer(undefined);
    },
  };
}
