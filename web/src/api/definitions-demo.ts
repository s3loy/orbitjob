import { randomUUID } from '../uuid';
import { parse, stringify } from 'lossless-json';
import type { Definition, DefinitionClient } from './definitions';
import type { JobDetail, RunDetail } from './contracts';
import { APIError } from './transport';
import type { M4Client } from './m4';

export function definitionsDemo(
  answer: <T>(value: T) => Promise<T>,
  jobs: JobDetail[],
  runs: RunDetail[],
  advance: () => void,
  workloads: Pick<M4Client, 'workflows' | 'workflow' | 'functionRun'>,
): DefinitionClient {
  const definitions = new Map<string, Definition>();
  for (const job of jobs)
    definitions.set(`jobs/${job.namespace}/${job.name}`, {
      kind: 'jobs',
      name: job.name,
      namespace: job.namespace,
      uid: job.source_uid,
      resource_version: '100',
      generation: job.generation,
      spec_json: JSON.stringify(
        {
          schedule: job.schedule,
          suspend: job.suspend,
          concurrencyPolicy: job.concurrency_policy,
          misfirePolicy: job.misfire_policy,
          timeoutSeconds: job.timeout_seconds,
          history: { successfulRuns: 3, failedRuns: 3 },
          retryPolicy: { maxAttempts: job.retry_max_attempts },
          jobTemplate: {
            image: job.job_template.image,
            command: job.job_template.command ?? [],
            args: job.job_template.args ?? [],
            backoffLimit: job.job_template.backoff_limit,
          },
        },
        null,
        2,
      ),
      manifest_digest: job.spec_hash,
      last_request_id: '',
      last_manifest_digest: '',
    });
  return {
    definition: async (kind, namespace, name) => {
      const key = `${kind}/${namespace}/${name}`;
      if (kind === 'workflows' && !definitions.has(key)) {
        const matches = await workloads.workflows({ search: name, limit: 100 });
        const match = matches.find((item) => item.namespace === namespace && item.name === name);
        if (match) {
          const item = await workloads.workflow(String(match.id));
          definitions.set(key, {
            kind: 'workflows',
            namespace,
            name,
            uid: item.source_uid,
            resource_version: '100',
            generation: item.generation,
            spec_json: JSON.stringify(
              {
                schedule: item.schedule,
                timeZone: item.time_zone ?? 'UTC',
                suspend: item.suspend,
                concurrencyPolicy: item.concurrency_policy ?? 'Forbid',
                failPolicy:
                  item.fail_policy === 'fail_fast'
                    ? 'FailFast'
                    : item.fail_policy === 'continue'
                      ? 'Continue'
                      : item.fail_policy,
                timeoutSeconds: item.timeout_seconds,
                tasks: item.tasks.map((task) => ({
                  name: task.name,
                  jobRef: { name: task.job_ref },
                  ...(task.depends_on.length ? { dependsOn: task.depends_on } : {}),
                  ...(task.condition ? { condition: task.condition } : {}),
                })),
              },
              null,
              2,
            ),
            manifest_digest: `revision-${item.id}-${item.generation}`,
            last_request_id: '',
            last_manifest_digest: '',
          });
        }
      }
      const found = definitions.get(key);
      if (!found)
        throw new APIError(
          'Definition not found. Use create-only expectations for a new resource.',
          404,
          'NOT_FOUND',
        );
      return answer(found);
    },
    applyDefinition: async (kind, namespace, name, input) => {
      let spec: string;
      try {
        const parsed = parse(input.spec_json);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error();
        spec = stringify(parsed, null, 2)!;
      } catch {
        throw new APIError('Spec must be a JSON object.', 400, 'MALFORMED_REQUEST');
      }
      try {
        const { importDefinitionSpec } = await import('./manifest-import');
        importDefinitionSpec(
          spec,
          kind === 'jobs' ? 'ScheduledJob' : 'WorkflowJob',
          namespace,
          name,
        );
      } catch (error) {
        throw new APIError(
          error instanceof Error ? error.message : 'The sample definition is invalid.',
          400,
          'MALFORMED_REQUEST',
        );
      }
      const hash = await crypto.subtle.digest(
        'SHA-256',
        new TextEncoder().encode(`${kind}/${namespace}/${name}/${spec}`),
      );
      const digest = Array.from(new Uint8Array(hash), (n) => n.toString(16).padStart(2, '0')).join(
        '',
      );
      const key = `${kind}/${namespace}/${name}`;
      const existing = definitions.get(key);
      if (
        !input.dry_run &&
        existing?.last_request_id === input.request_id &&
        existing.manifest_digest === digest
      )
        return answer({ ...existing, request_id: input.request_id, state: 'reconciled' as const });
      if (
        (existing?.uid ?? '') !== input.expected_uid ||
        (existing?.resource_version ?? '') !== input.expected_resource_version
      )
        throw new APIError(
          'Definition changed. Load and review its current identity before applying.',
          409,
          'CONFLICT',
        );
      if (!input.dry_run && input.manifest_digest !== digest)
        throw new APIError('Checked manifest digest changed.', 409, 'CONFLICT');
      const result = {
        kind,
        namespace,
        name,
        uid: existing?.uid ?? (input.dry_run ? '' : randomUUID()),
        resource_version: input.dry_run
          ? (existing?.resource_version ?? '')
          : String(Number(existing?.resource_version ?? 100) + 1),
        generation: input.dry_run
          ? (existing?.generation ?? 0)
          : Number(existing?.generation ?? 0) + 1,
        spec_json: spec,
        manifest_digest: digest,
        last_request_id: input.dry_run ? (existing?.last_request_id ?? '') : input.request_id,
        last_manifest_digest: input.dry_run ? (existing?.last_manifest_digest ?? '') : digest,
        request_id: input.request_id,
        state: input.dry_run ? ('validated' as const) : ('accepted' as const),
      };
      if (!input.dry_run) definitions.set(key, result);
      return answer(result);
    },
    attemptLogs: async (parent, attempt, tail) => {
      advance();
      const instance =
        parent.kind === 'instance' ? runs.find((run) => String(run.id) === parent.run) : undefined;
      const invocation =
        parent.kind === 'function'
          ? await workloads.functionRun(parent.id, parent.run).catch((error) => {
              if (error instanceof APIError && error.status === 404) return undefined;
              throw error;
            })
          : undefined;
      const selected = (instance?.attempts ?? invocation?.attempts)?.find(
        (entry) => entry.attempt_number === attempt,
      );
      const job = jobs.find((item) => item.source_uid === instance?.source_uid);
      const workload = job?.name ?? `function-${parent.kind === 'function' ? parent.id : 'task'}`;
      const message =
        selected?.phase === 'Failed'
          ? job?.name === 'billing-reconciliation'
            ? 'ERROR Settlement ledger returned HTTP 503. Process exited with status 1.'
            : 'ERROR Process exited with status 1.'
          : selected?.phase === 'Succeeded'
            ? 'Process exited with status 0.'
            : selected?.phase === 'Canceled'
              ? 'Process stopped after cancellation.'
              : 'Processing started.';
      return answer({
        state: selected ? 'ready' : 'absent',
        text: selected
          ? `${selected.started_at ?? ''} ${workload} attempt ${attempt}\n${selected.completed_at ?? selected.started_at ?? ''} ${message}\n`
          : '',
        truncated: false,
        tail_lines: tail,
        limit_bytes: 65536,
        attempt,
        container: 'task',
        reason: selected
          ? 'Sample log snapshot'
          : 'No container output was recorded for this attempt.',
      });
    },
  };
}
