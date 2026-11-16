import { expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { z } from 'zod';
import type { RunDetail } from '../src/api/contracts';

const namespace = 'orbitjob';
const ownHold = 'orbitjob.io/termination-evidence';
const kube = (args: string[], input?: string) =>
  execFileSync('kubectl', ['-n', namespace, ...args], {
    encoding: 'utf8',
    input,
    maxBuffer: 16 * 1024 * 1024,
    stdio: ['pipe', 'pipe', 'pipe'],
  });
const identity = z.object({
  name: z.string().min(1),
  uid: z.string().min(1),
  resourceVersion: z.string().min(1),
  finalizers: z.array(z.string()).default([]),
  deletionTimestamp: z.string().optional(),
});
const jobSchema = z.object({
  metadata: identity.extend({
    ownerReferences: z.array(
      z.object({ uid: z.string(), kind: z.string(), controller: z.boolean().optional() }),
    ),
  }),
  spec: z.object({
    backoffLimit: z.literal(0),
    parallelism: z.literal(1),
    completions: z.literal(1),
    template: z.object({ metadata: z.object({ finalizers: z.array(z.string()) }) }),
  }),
});
const containers = z.array(z.object({ name: z.string().min(1) }));
const podSchema = z.object({
  metadata: identity.extend({
    ownerReferences: z.array(
      z.object({ uid: z.string(), kind: z.string(), controller: z.boolean().optional() }),
    ),
  }),
  spec: z.object({
    containers,
    initContainers: containers.default([]),
    ephemeralContainers: containers.default([]),
  }),
  status: z.object({ phase: z.string() }),
});
const proofSchema = z.object({
  source: z.literal('pods'),
  protocol: z.literal('pod-hold-v1'),
  hold_release: z.literal('done'),
  revoked: z.literal(false).optional(),
  namespace: z.literal(namespace),
  job_name: z.string().min(1),
  job_uid: z.string().min(1),
  resource_version: z.string().min(1),
  stop_boundary: z.enum(['failure_target', 'success_criteria_met', 'failed', 'complete']),
  observed_at: z.string().datetime({ offset: true }),
  pods: z
    .array(
      z.object({
        uid: z.string().min(1),
        name: z.string().min(1),
        resource_version: z.string().min(1),
        containers: z
          .array(
            z.object({
              name: z.string().min(1),
              state: z.object({
                terminated: z.object({
                  finishedAt: z.string().datetime({ offset: true }),
                  exitCode: z.number().int(),
                  reason: z.string().optional(),
                }),
              }),
            }),
          )
          .min(1),
      }),
    )
    .min(1),
});

export type CancellationTarget = {
  tenantID: string;
  runID: string;
  attemptID: string;
  attemptNumber: number;
  jobName: string;
  jobUID: string;
  ownerRunUID: string;
  hold: string;
  pods: { uid: string; name: string; containers: string[] }[];
};

function ownedPods(jobUID: string) {
  const list = z
    .object({ items: z.array(z.unknown()) })
    .parse(JSON.parse(kube(['get', 'pods', '-o', 'json'])));
  return list.items
    .filter((item) => {
      const owners = z
        .object({
          metadata: z.object({
            ownerReferences: z
              .array(
                z.object({ uid: z.string(), kind: z.string(), controller: z.boolean().optional() }),
              )
              .default([]),
          }),
        })
        .parse(item);
      return owners.metadata.ownerReferences.some(
        (owner) => owner.uid === jobUID && owner.kind === 'Job' && owner.controller === true,
      );
    })
    .map((item) => podSchema.parse(item));
}

export function holdCancellationJob(run: RunDetail, hold: string): CancellationTarget {
  expect(execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim()).toBe(
    'kind-orbitjob-console-e2e',
  );
  expect(run.attempts).toHaveLength(1);
  const attempt = run.attempts[0];
  const target: CancellationTarget = {
    tenantID: z
      .string()
      .regex(/^[0-9A-HJKMNP-TV-Z]{26}$/)
      .parse(run.tenant_id),
    runID: z
      .string()
      .regex(/^[1-9]\d*$/)
      .parse(String(run.id)),
    attemptID: z
      .string()
      .regex(/^[1-9]\d*$/)
      .parse(String(attempt.id)),
    attemptNumber: z.number().int().positive().parse(attempt.attempt_number),
    jobName: z
      .string()
      .regex(/^[a-z0-9][a-z0-9.-]*$/)
      .parse(attempt.kubernetes_job_name),
    jobUID: z.string().min(1).parse(attempt.kubernetes_job_uid),
    ownerRunUID: z.string().min(1).parse(attempt.owner_run_uid),
    hold,
    pods: [],
  };
  expect(String(attempt.run_id)).toBe(target.runID);
  expect(run.attempt).toBe(target.attemptNumber);
  const job = jobSchema.parse(JSON.parse(kube(['get', 'job', target.jobName, '-o', 'json'])));
  expect(job.metadata.uid).toBe(target.jobUID);
  expect(job.metadata.ownerReferences.filter((owner) => owner.controller === true)).toEqual([
    { uid: target.ownerRunUID, kind: 'JobRun', controller: true },
  ]);
  expect(job.metadata.deletionTimestamp).toBeUndefined();
  expect(job.spec.template.metadata.finalizers).toContain(ownHold);
  expect(job.metadata.finalizers).not.toContain(hold);
  const pods = ownedPods(target.jobUID);
  expect(pods).toHaveLength(1);
  target.pods = pods.map((pod) => {
    expect(pod.status.phase).toBe('Running');
    expect(pod.metadata.finalizers).toContain(ownHold);
    return {
      uid: pod.metadata.uid,
      name: pod.metadata.name,
      containers: [
        ...pod.spec.containers,
        ...pod.spec.initContainers,
        ...pod.spec.ephemeralContainers,
      ]
        .map((container) => container.name)
        .sort(),
    };
  });
  kube([
    'patch',
    'job',
    target.jobName,
    '--type=json',
    '-p',
    JSON.stringify([
      { op: 'test', path: '/metadata/uid', value: target.jobUID },
      { op: 'test', path: '/metadata/resourceVersion', value: job.metadata.resourceVersion },
      { op: 'add', path: '/metadata/finalizers', value: [...job.metadata.finalizers, hold] },
    ]),
  ]);
  return target;
}

export function releaseCancellationHold(target: CancellationTarget) {
  const job = jobSchema.parse(JSON.parse(kube(['get', 'job', target.jobName, '-o', 'json'])));
  expect(job.metadata.uid).toBe(target.jobUID);
  const index = job.metadata.finalizers.indexOf(target.hold);
  expect(index).toBeGreaterThanOrEqual(0);
  kube([
    'patch',
    'job',
    target.jobName,
    '--type=json',
    '-p',
    JSON.stringify([
      { op: 'test', path: '/metadata/uid', value: target.jobUID },
      { op: 'test', path: '/metadata/resourceVersion', value: job.metadata.resourceVersion },
      { op: 'test', path: `/metadata/finalizers/${index}`, value: target.hold },
      { op: 'remove', path: `/metadata/finalizers/${index}` },
    ]),
  ]);
}

export function readTrustedCancellationEvidence(run: RunDetail, target: CancellationTarget) {
  expect(String(run.id)).toBe(target.runID);
  expect(run.tenant_id).toBe(target.tenantID);
  expect(run.attempt).toBe(target.attemptNumber);
  expect(run.attempts).toHaveLength(1);
  const attempt = run.attempts[0];
  expect(String(attempt.id)).toBe(target.attemptID);
  expect(String(attempt.run_id)).toBe(target.runID);
  expect(attempt.attempt_number).toBe(target.attemptNumber);
  expect(attempt.kubernetes_job_name).toBe(target.jobName);
  expect(attempt.kubernetes_job_uid).toBe(target.jobUID);
  expect(attempt.owner_run_uid).toBe(target.ownerRunUID);
  expect(attempt.execution_unknown_since).toBeNull();
  // Only validated decimal IDs and a tenant ULID enter SQL; values are quoted
  // through psql variables. This transaction cannot write fixture state.
  const output = kube(
    [
      'exec',
      '-i',
      'deploy/orbitjob-postgres',
      '--',
      'sh',
      '-c',
      'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At "$@"',
      'psql',
      '-v',
      `tenant=${target.tenantID}`,
      '-v',
      `run=${target.runID}`,
      '-v',
      `attempt=${target.attemptID}`,
    ],
    `BEGIN READ ONLY;
SELECT json_build_object('tenant_id',r.tenant_id,'run_id',r.id::text,'run_phase',r.phase,
 'attempt_id',a.id::text,'attempt_number',a.attempt_number,'attempt_phase',a.phase,
 'job_name',a.kubernetes_job_name,'job_uid',a.kubernetes_job_uid,'owner_run_uid',a.owner_run_uid,
 'unknown_since',a.execution_unknown_since,'proof',a.termination_evidence)
FROM job_run_control_plane r JOIN job_run_attempts_control_plane a ON a.tenant_id=r.tenant_id AND a.run_id=r.id
WHERE r.tenant_id=:'tenant' AND r.id=:'run'::bigint AND a.id=:'attempt'::bigint;
COMMIT;`,
  );
  const raw: unknown = JSON.parse(output);
  const saved = z
    .object({
      tenant_id: z.string(),
      run_id: z.string(),
      run_phase: z.string(),
      attempt_id: z.string(),
      attempt_number: z.number(),
      attempt_phase: z.string(),
      job_name: z.string(),
      job_uid: z.string(),
      owner_run_uid: z.string(),
      unknown_since: z.null(),
      proof: proofSchema,
    })
    .parse(raw);
  expect(saved).toMatchObject({
    tenant_id: target.tenantID,
    run_id: target.runID,
    run_phase: run.phase,
    attempt_id: target.attemptID,
    attempt_number: target.attemptNumber,
    job_name: target.jobName,
    job_uid: target.jobUID,
    owner_run_uid: target.ownerRunUID,
  });
  expect(saved.proof.job_name).toBe(target.jobName);
  expect(saved.proof.job_uid).toBe(target.jobUID);
  expect(saved.proof.pods.map((pod) => pod.uid).sort()).toEqual(
    target.pods.map((pod) => pod.uid).sort(),
  );
  for (const pod of saved.proof.pods) {
    const original = target.pods.find((item) => item.uid === pod.uid)!;
    expect(pod.name).toBe(original.name);
    expect(pod.containers.map((container) => container.name).sort()).toEqual(original.containers);
    for (const container of pod.containers) {
      expect(Date.parse(container.state.terminated.finishedAt)).toBeGreaterThan(0);
      expect(['ContainerStatusUnknown', 'NodeLost']).not.toContain(
        container.state.terminated.reason,
      );
    }
  }
  if (run.phase === 'CancelUnknown') {
    expect(['Running', 'Failed']).toContain(attempt.phase);
    if (attempt.phase === 'Failed') expect(attempt.completed_at).toBeTruthy();
    const held = jobSchema.parse(JSON.parse(kube(['get', 'job', target.jobName, '-o', 'json'])));
    expect(held.metadata.uid).toBe(target.jobUID);
    expect(held.metadata.deletionTimestamp).toBeTruthy();
    expect(held.metadata.finalizers).toContain(target.hold);
  }
  return { ...saved, rawProof: z.object({ proof: z.unknown() }).parse(raw).proof };
}

export async function expectCancellationResourcesGone(target: CancellationTarget) {
  await expect
    .poll(
      () => {
        const job = kube(['get', 'job', target.jobName, '--ignore-not-found', '-o', 'json']).trim();
        if (job) expect(jobSchema.parse(JSON.parse(job)).metadata.uid).toBe(target.jobUID);
        return job === '' && ownedPods(target.jobUID).length === 0;
      },
      { timeout: 60_000, intervals: [1000] },
    )
    .toBe(true);
}
