import type { SourceControl, SourceControlKind } from './source-controls';
import { ulid } from 'ulid';
import { sampleWorkspace } from '../sample-workspace';
import { definitionsDemo } from './definitions-demo';
import { timestampNanos } from './query-window';
import { administrationDemo } from './administration-demo';
import { monitoringDemo } from './monitoring-demo';
import { m4Demo } from './m4-demo';
import { type JobDetail, type RunDetail, type ID } from './contracts';
import { APIError, type Client, type RunFilter, PAGE_SIZE } from './transport';
const scheduledWork = [
  ['billing-reconciliation', 'finance-prod', '0 * * * *', 'Every hour', 'Forbid'],
  ['warehouse-sync', 'data-pipelines', '*/15 * * * *', 'Every 15 minutes', 'Forbid'],
  ['daily-digest', 'notifications', '0 8 * * *', 'Daily at 08:00', 'Allow'],
  ['session-cleanup', 'platform', '*/30 * * * *', 'Every 30 minutes', 'Replace'],
  ['inventory-snapshot', 'finance-prod', '', 'Manual only', 'Forbid'],
  ['backup-verification', 'platform', '0 3 * * *', 'Daily at 03:00', 'Forbid'],
  ['invoice-export', 'finance-prod', '0 6 * * *', 'Daily at 06:00', 'Forbid'],
  ['search-reindex', 'data-pipelines', '', 'Manual only', 'Forbid'],
];
const workflowJobs = ['billing-extract', 'billing-validate', 'billing-publish'];
const tenant = sampleWorkspace.id;
const at = (time: number) => new Date(time).toISOString();
const hour = 3_600_000;

export function demoClient(): Client {
  const keyID = ulid();
  const base = Date.now() - 120_000;
  let disposed = false;
  let next = 1043;
  const intents = new Map<string, RunDetail>();
  const controls = new Map<string, SourceControl>();
  const created = new Map<string, number>();
  const cancellations = new Map<string, number>();
  const jobs: JobDetail[] = [
    ...scheduledWork,
    ...workflowJobs.map((name) => [name, 'finance-prod', '', 'Manual only', 'Forbid']),
  ].map(([name, namespace, schedule, summary, policy], i) => ({
    id: i + 17,
    tenant_id: tenant,
    name,
    namespace,
    source_uid: `8ed1a9a7-1be2-4e79-aebc-${String(i + 1).padStart(12, '0')}`,
    generation: 1,
    source_mode: 'kubernetes',
    spec_hash: `revision-${i + 17}-1`,
    actor: 'system:serviceaccount:platform:gitops',
    created_at: at(base - 7 * 24 * hour),
    schedule,
    schedule_summary: summary,
    suspend: i === 5,
    concurrency_policy: policy,
    misfire_policy: 'Skip',
    timeout_seconds: 300,
    retry_max_attempts: 3,
    job_template: {
      image: `registry.example.com/commerce/${name}:1.4.2`,
      command: [`/app/${name}`],
      args: [],
      backoff_limit: 0,
    },
  }));
  function recordedRun(
    job: JobDetail,
    id: ID,
    trigger: string,
    phase: string,
    count: number,
    start: number,
    attemptBase: number,
  ): RunDetail {
    const active = phase === 'Running' || phase === 'CancelUnknown';
    return {
      id,
      tenant_id: tenant,
      source_uid: job.source_uid,
      revision_id: job.id,
      occurrence_key: `${job.name}-${id}`,
      trigger,
      actor: trigger === 'Manual' ? 'operations-console' : `operator:${trigger.toLowerCase()}`,
      phase,
      attempt: count,
      max_attempts: 3,
      created_at: at(start),
      updated_at: at(start + (active ? 20_000 : 83_000)),
      attempts: Array.from({ length: count }, (_, n) => ({
        id: attemptBase + n,
        run_id: id,
        attempt_number: n + 1,
        phase: n < count - 1 || phase === 'RetryWaiting' ? 'Failed' : active ? 'Running' : phase,
        kubernetes_job_name: `oj-${job.name.slice(0, 18)}-${id}-a${n + 1}`,
        kubernetes_job_uid: `45d901b7-f39d-4aa3-b126-${String(attemptBase + n).padStart(12, '0')}`,
        created_at: at(start + 4000 + n * 54_000),
        started_at: at(start + 4000 + n * 54_000),
        completed_at: active ? null : at(start + 28_000 + n * 54_000),
      })),
    };
  }
  const billingExtract = jobs[scheduledWork.length];
  const completedBillingStart = base - 2 * hour;
  const billingStepStarts = new Map<ID, number>([
    [1041, completedBillingStart],
    [1040, base - 6 * hour],
    [1038, base - 60_000],
  ]);
  const runs: RunDetail[] = Array.from({ length: 36 }, (_, i) => {
    const id = i === 35 ? '9007199254740993' : 1042 - i;
    const workflowStart = billingStepStarts.get(id);
    const workflow = workflowStart !== undefined;
    const job = workflow ? billingExtract : jobs[i % scheduledWork.length];
    const phase =
      i === 0
        ? 'RetryWaiting'
        : i === 2 || i === 11
          ? 'Failed'
          : i === 4
            ? 'Running'
            : i === 5
              ? 'CancelUnknown'
              : 'Succeeded';
    const count = i === 0 || i === 11 ? 2 : 1;
    const start = workflowStart ?? base - i * hour;
    const trigger = workflow ? 'Workflow' : i % 5 === 0 || !job.schedule ? 'Manual' : 'Schedule';
    return recordedRun(job, id, trigger, phase, count, start, 2000 + i * 3);
  });
  const completedBillingJobs = [
    jobs[scheduledWork.length + 1],
    jobs[0],
    jobs[scheduledWork.length + 2],
  ];
  completedBillingJobs.forEach((job, index) => {
    const id = 1007 - index;
    const start = completedBillingStart + (index === 2 ? 64_000 : 32_000);
    runs.push(recordedRun(job, id, 'Workflow', 'Succeeded', 1, start, 2200 + index));
  });
  runs.sort((a, b) => b.created_at.localeCompare(a.created_at));
  async function answer<T>(value: T): Promise<T> {
    await new Promise((resolve) => setTimeout(resolve, 180));
    if (disposed) throw new DOMException('Connection closed', 'AbortError');
    return structuredClone(value);
  }
  function advance() {
    for (const candidate of intents.values()) {
      const elapsed = Date.now() - created.get(String(candidate.id))!;
      if (elapsed < 1200 || cancellations.has(String(candidate.id))) continue;
      if (!runs.includes(candidate)) runs.unshift(candidate);
      candidate.phase =
        elapsed >= 10_000
          ? 'Succeeded'
          : elapsed >= 7000
            ? 'Running'
            : elapsed >= 4000
              ? 'RetryWaiting'
              : 'Running';
      candidate.attempt = elapsed >= 7000 ? 2 : 1;
      const start = Date.parse(candidate.created_at);
      candidate.updated_at = at(Math.min(Date.now(), start + 10_000));
      candidate.attempts = Array.from({ length: candidate.attempt }, (_, n) => ({
        id: Number(candidate.id) * 10 + n,
        run_id: candidate.id,
        attempt_number: n + 1,
        phase: n === 0 && elapsed >= 4000 ? 'Failed' : candidate.phase,
        kubernetes_job_name: `oj-${candidate.revision_id}-${candidate.id}-a${n + 1}`,
        created_at: at(start + (n ? 7000 : 1000)),
        started_at: at(start + (n ? 7000 : 1000)),
        completed_at:
          n === 0 && elapsed >= 4000
            ? at(start + 4000)
            : candidate.phase === 'Succeeded'
              ? at(start + 10_000)
              : null,
      }));
    }
    for (const [id, requested] of cancellations) {
      const run = runs.find((run) => String(run.id) === id);
      if (!run) continue;
      const elapsed = Date.now() - requested;
      if (elapsed >= 3500) {
        run.phase = 'Canceled';
        for (const attempt of run.attempts) {
          if (!attempt.completed_at && ['Running', 'CreatingAttempt'].includes(attempt.phase)) {
            attempt.phase = 'Canceled';
            attempt.completed_at = at(requested + 3500);
          }
        }
      } else if (elapsed >= 1000) run.phase = 'CancelRequested';
      if (elapsed >= 1000) run.updated_at = at(requested + Math.min(elapsed, 3500));
    }
  }
  function attachSource(run: RunDetail) {
    const owner = jobs.find((job) => job.source_uid === run.source_uid);
    if (owner)
      Object.assign(run, {
        source_name: owner.name,
        source_namespace: owner.namespace,
        source_kind: 'ScheduledJob',
      });
  }
  runs.forEach(attachSource);
  const m4 = m4Demo(answer, runs, base);
  function control(kind: SourceControlKind, id: string): SourceControl {
    return (
      controls.get(`${kind}:${id}`) ?? {
        source_mode:
          kind === 'jobs'
            ? 'kubernetes'
            : kind === 'workflows'
              ? 'workflow'
              : kind === 'functions'
                ? 'function'
                : 'check',
        source_uid:
          kind === 'jobs'
            ? (jobs.find((job) => String(job.id) === id)?.source_uid ?? id)
            : `${kind.slice(0, -1)}-${id}`,
        version: 0,
        schedule_paused: false,
        execution_paused: false,
        tombstoned: false,
        can_manage: true,
      }
    );
  }
  return {
    sourceControls: (kind, id) => answer(control(kind, id)),
    updateSourceControls: (kind, id, change) => {
      const current = control(kind, id);
      if (String(current.version) !== String(change.version))
        throw new APIError('Controls changed. Refresh current controls.', 409, 'CONFLICT');
      const updated: SourceControl = {
        ...current,
        ...change,
        version: Number(current.version) + 1,
        actor: keyID,
        updated_at: at(Date.now()),
      };
      controls.set(`${kind}:${id}`, updated);
      return answer(updated);
    },
    ...administrationDemo(answer),
    ...definitionsDemo(answer, jobs, runs, advance, m4),
    identity: (probes = []) =>
      answer({
        key_id: keyID,
        subject_type: 'demo',
        installation_admin: false,
        kind: 'tenant',
        tenant_id: tenant,
        group_id: '',
        boundary_policy_id: '',
        target_tenant_id: tenant,
        tenant_name: sampleWorkspace.name,
        tenant_slug: sampleWorkspace.slug,
        workload_namespaces: [...new Set(jobs.map((job) => job.namespace)), 'orbitjob'],
        namespace_metadata_available: true,
        capabilities: probes.map((probe) => ({ ...probe, allowed: true })),
      }),
    ...monitoringDemo(answer),
    ...m4,
    jobs: (offset = 0, search = '', filter = {}) =>
      answer(
        jobs
          .filter(
            (j) =>
              `${j.name} ${j.namespace}`.toLowerCase().includes(search.toLowerCase()) &&
              (!filter.namespace || filter.namespace === j.namespace) &&
              (!filter.source_uid || filter.source_uid === j.source_uid),
          )
          .slice(offset, offset + (filter.limit ?? PAGE_SIZE)),
      ),
    job: (id: ID) => answer(jobs.find((job) => String(job.id) === String(id)) ?? missing()),
    runs: (filter: RunFilter = {}) => {
      advance();
      const items = runs.filter(
        (run) =>
          (!filter.created_from ||
            timestampNanos(run.created_at)! >= timestampNanos(filter.created_from)!) &&
          (!filter.created_before ||
            timestampNanos(run.created_at)! < timestampNanos(filter.created_before)!) &&
          (!filter.phase || run.phase === filter.phase) &&
          (!filter.source_uid || run.source_uid === filter.source_uid) &&
          (!filter.occurrence_key || run.occurrence_key === filter.occurrence_key),
      );
      return answer(
        items.slice(filter.offset ?? 0, (filter.offset ?? 0) + (filter.limit ?? PAGE_SIZE)),
      );
    },
    runSummary: (filter = {}) => {
      advance();
      const rows = runs.filter(
        (r) =>
          (!filter.phase || r.phase === filter.phase) &&
          (!filter.source_uid || r.source_uid === filter.source_uid) &&
          (!filter.occurrence_key || r.occurrence_key === filter.occurrence_key) &&
          (!filter.created_from ||
            timestampNanos(r.created_at)! >= timestampNanos(filter.created_from)!) &&
          (!filter.created_before ||
            timestampNanos(r.created_at)! < timestampNanos(filter.created_before)!),
      );
      const phases: Record<string, number> = {};
      for (const r of rows) phases[r.phase] = (phases[r.phase] ?? 0) + 1;
      return answer({ total: rows.length, phases });
    },
    run: (id) => {
      advance();
      return answer(runs.find((run) => String(run.id) === String(id)) ?? missing());
    },
    trigger: async (id, key) => {
      const job = jobs.find((item) => String(item.id) === String(id)) ?? missing();
      if (job.suspend) throw new APIError('This definition is suspended.', 409, 'CONFLICT');
      let run = intents.get(key);
      const existed = Boolean(run);
      if (!run) {
        run = {
          id: next++,
          tenant_id: tenant,
          source_uid: job.source_uid,
          revision_id: job.id,
          occurrence_key: key,
          trigger: 'Manual',
          actor: 'operations-console',
          phase: 'Pending',
          attempt: 0,
          max_attempts: 3,
          created_at: at(Date.now()),
          updated_at: at(Date.now()),
          attempts: [],
        };
        attachSource(run);
        intents.set(key, run);
        created.set(String(run.id), Date.now());
      }
      return answer({
        namespace: job.namespace,
        name: `${job.name}-${run.id}`,
        occurrence_key: key,
        trigger: 'Manual',
        phase: '',
        created: !existed,
      });
    },
    cancel: async (id) => {
      const run = runs.find((item) => String(item.id) === String(id)) ?? missing();
      if (
        !['Succeeded', 'Failed', 'Canceled'].includes(run.phase) &&
        !cancellations.has(String(id))
      )
        cancellations.set(String(id), Date.now());
      await answer(undefined);
    },
    supportsCorrelation: () => answer(true),
    dispose: () => {
      disposed = true;
    },
  };
}
function missing(): never {
  throw new Error('This sample record does not exist.');
}
