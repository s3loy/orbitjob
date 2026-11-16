import { stringify } from 'yaml';
import { stringify as stringifyJSON, LosslessNumber } from 'lossless-json';
import { validateScheduledJob, validateWorkflow } from './manifest-validators.js';
import schema from './manifest-schema-info.json';
import type { ExecutionResources } from '../ExecutionResourcesFields';

export type ManifestKind = 'ScheduledJob' | 'WorkflowJob';
export type Rule = {
  task: string;
  field: string;
  operator: string;
  values: string[];
  number: string;
};
export type Task = {
  name: string;
  job: string;
  dependencies: string[];
  combination: '' | 'all' | 'any';
  rules: Rule[];
};
export type Draft = {
  executionPolicyVersion?: number;
  retrySafe?: boolean;
  resources?: ExecutionResources;
  kind: ManifestKind;
  name: string;
  namespace: string;
  schedule: string;
  timeZone: string;
  image: string;
  command: string[];
  args: string[];
  timeout: string;
  attempts: string;
  backoff: string;
  success: string;
  failed: string;
  concurrency: string;
  misfire: string;
  suspend: boolean;
  failPolicy: string;
  tasks: Task[];
  // Imported desired state and its form projection travel together. Only
  // fields changed in the form overwrite the original supported spec.
  sourceSpec?: Record<string, unknown>;
  sourceFormSpec?: Record<string, unknown>;
};
export type DraftIssue = { path: string; message: string };
export const manifestSchema = schema;
export function initialDraft(kind: ManifestKind): Draft {
  return {
    kind,
    executionPolicyVersion: 1,
    retrySafe: false,
    name: kind === 'ScheduledJob' ? 'nightly-disk-usage' : 'billing-report-delivery',
    namespace: 'orbitjob',
    schedule: '0 2 * * *',
    timeZone: 'UTC',
    image: 'busybox:1.37.0',
    command: ['df'],
    args: ['-P', '/'],
    timeout: '120',
    attempts: '1',
    backoff: '0',
    success: '3',
    failed: '3',
    concurrency: 'Forbid',
    misfire: 'Skip',
    suspend: false,
    failPolicy: 'Continue',
    tasks: [
      { name: 'extract', job: 'billing-extract', dependencies: [], combination: '', rules: [] },
      {
        name: 'publish',
        job: 'billing-publish',
        dependencies: ['extract'],
        combination: 'all',
        rules: [
          { task: 'extract', field: 'phase', operator: 'eq', values: ['Succeeded'], number: '0' },
        ],
      },
    ],
  };
}
export function manifestObject(d: Draft, checkProjection = false) {
  const integer = (s: string) => (/^-?\d+$/.test(s) ? BigInt(s) : s);
  const formSpec =
    d.kind === 'ScheduledJob'
      ? {
          ...(d.executionPolicyVersion === undefined
            ? {}
            : { executionPolicyVersion: d.executionPolicyVersion }),
          ...(d.retrySafe === undefined ? {} : { retrySafe: d.retrySafe }),
          schedule: d.schedule,
          suspend: d.suspend,
          concurrencyPolicy: d.concurrency,
          misfirePolicy: d.misfire,
          timeoutSeconds: integer(d.timeout),
          history: { successfulRuns: integer(d.success), failedRuns: integer(d.failed) },
          retryPolicy: { maxAttempts: integer(d.attempts) },
          jobTemplate: {
            ...(d.resources ? { resources: d.resources } : {}),
            image: d.image,
            command: d.command,
            args: d.args,
            backoffLimit: integer(d.backoff),
          },
        }
      : {
          ...(d.executionPolicyVersion === undefined
            ? {}
            : { executionPolicyVersion: d.executionPolicyVersion }),
          schedule: d.schedule,
          timeZone: d.timeZone,
          suspend: d.suspend,
          concurrencyPolicy: d.concurrency,
          failPolicy: d.failPolicy,
          timeoutSeconds: integer(d.timeout),
          history: { successfulRuns: integer(d.success), failedRuns: integer(d.failed) },
          tasks: d.tasks.map((t) => ({
            name: t.name,
            jobRef: { name: t.job },
            ...(t.dependencies.length ? { dependsOn: t.dependencies } : {}),
            ...(t.combination
              ? {
                  condition: {
                    type: t.combination,
                    rules: t.rules.map((r) => ({
                      task: r.task,
                      field: r.field,
                      operator: r.operator,
                      ...(['lt', 'le', 'gt', 'ge'].includes(r.operator)
                        ? { number: integer(r.number) }
                        : { values: r.values }),
                    })),
                  },
                }
              : {}),
          })),
        };
  // Only bounded int32s are converted for the generated schema validator.
  // Workflow int64 rule operands are range-checked separately without rounding.
  const spec =
    d.sourceSpec && d.sourceFormSpec
      ? overlayFormChanges(d.sourceSpec, d.sourceFormSpec, formSpec)
      : formSpec;
  function project(value: unknown, path = ''): unknown {
    if (typeof value === 'bigint')
      return /^tasks\.\d+\.condition\.rules\.\d+\.number$/.test(path)
        ? 0
        : value >= -2147483648n && value <= 2147483647n
          ? Number(value)
          : value.toString();
    if (Array.isArray(value)) return value.map((item, index) => project(item, `${path}.${index}`));
    if (value && typeof value === 'object')
      return Object.fromEntries(
        Object.entries(value).map(([k, v]) => [k, project(v, path ? `${path}.${k}` : k)]),
      );
    return value;
  }
  return {
    apiVersion: schema.apiVersion,
    kind: d.kind,
    metadata: { name: d.name, namespace: d.namespace },
    spec: checkProjection ? project(spec) : spec,
  };
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function sameValue(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((value, index) => sameValue(value, b[index]));
  if (record(a) && record(b)) {
    const keys = Object.keys(a);
    return (
      keys.length === Object.keys(b).length &&
      keys.every((key) => Object.hasOwn(b, key) && sameValue(a[key], b[key]))
    );
  }
  return false;
}

// Arrays are edited as values except workflow tasks, whose stable names keep
// each original task's supported nested fields attached through reordering.
function overlayFormChanges(
  source: unknown,
  baseline: unknown,
  edited: unknown,
  path = '',
): unknown {
  if (sameValue(baseline, edited)) return source;
  if (
    path === 'tasks' &&
    Array.isArray(source) &&
    Array.isArray(baseline) &&
    Array.isArray(edited)
  ) {
    return edited.map((task) => {
      if (!record(task)) return task;
      const original = source.find((item) => record(item) && item.name === task.name);
      const previous = baseline.find((item) => record(item) && item.name === task.name);
      return previous ? overlayFormChanges(original, previous, task, 'task') : task;
    });
  }
  if (record(baseline) && record(edited)) {
    const next: Record<string, unknown> = record(source) ? { ...source } : {};
    for (const key of new Set([...Object.keys(baseline), ...Object.keys(edited)])) {
      if (sameValue(baseline[key], edited[key])) continue;
      if (!Object.hasOwn(edited, key)) delete next[key];
      else
        next[key] = overlayFormChanges(
          next[key],
          baseline[key],
          edited[key],
          path ? `${path}.${key}` : key,
        );
    }
    return next;
  }
  return edited;
}
export function manifestYAML(d: Draft) {
  return stringify(manifestObject(d), {
    lineWidth: 0,
    defaultStringType: 'QUOTE_DOUBLE',
    defaultKeyType: 'PLAIN',
  });
}
export function checkDraft(d: Draft): DraftIssue[] {
  const errors: DraftIssue[] = [];
  const add = (path: string, message: string) => errors.push({ path, message });
  if (
    d.name.length > 253 ||
    !/^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$/.test(d.name) ||
    d.name.split('.').some((p) => p.length > 63 || !/^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(p))
  )
    add('metadata.name', 'Use a Kubernetes DNS subdomain name (up to 253 characters).');
  if (d.namespace.length > 63 || !/^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(d.namespace))
    add('metadata.namespace', 'Use a namespace DNS label (up to 63 characters).');
  const validate = d.kind === 'ScheduledJob' ? validateScheduledJob : validateWorkflow;
  if (!validate(manifestObject(d, true)))
    for (const error of validate.errors ?? [])
      add(
        error.instancePath.replaceAll('/', '.').replace(/^\./, '') +
          (error.params.missingProperty ? `.${error.params.missingProperty}` : ''),
        error.message ?? 'Invalid CR field.',
      );
  if (d.kind === 'WorkflowJob') {
    const names = new Set<string>();
    for (const t of d.tasks) {
      if (names.has(t.name)) add('spec.tasks', `Task ${t.name} is duplicated.`);
      names.add(t.name);
    }
    for (const t of d.tasks) {
      if (new Set(t.dependencies).size !== t.dependencies.length)
        add(`tasks.${t.name}`, 'A dependency occurs more than once.');
      for (const dep of t.dependencies)
        if (!names.has(dep) || dep === t.name) add(`tasks.${t.name}`, `Invalid dependency ${dep}.`);
      if (t.combination)
        for (const r of t.rules) {
          if (!t.dependencies.includes(r.task))
            add(`tasks.${t.name}.condition`, 'A rule must reference a declared dependency.');
          const numeric = ['lt', 'le', 'gt', 'ge'].includes(r.operator);
          if (numeric ? r.field === 'phase' : r.field !== 'phase')
            add(
              `tasks.${t.name}.condition`,
              'Numeric comparisons use attempts/duration_ms; string comparisons use phase.',
            );
          if (
            numeric &&
            (!/^-?\d+$/.test(r.number) ||
              BigInt(r.number) < -9223372036854775808n ||
              BigInt(r.number) > 9223372036854775807n)
          )
            add(`tasks.${t.name}.condition`, 'Number must be a signed 64-bit integer.');
          if (
            !numeric &&
            (['eq', 'ne'].includes(r.operator) ? r.values.length !== 1 : !r.values.length)
          )
            add(
              `tasks.${t.name}.condition`,
              'This phase comparison has an invalid number of values.',
            );
        }
    }
    const visiting = new Set<string>(),
      done = new Set<string>();
    function visit(name: string): boolean {
      if (visiting.has(name)) return true;
      if (done.has(name)) return false;
      visiting.add(name);
      for (const dep of d.tasks.find((t) => t.name === name)?.dependencies ?? [])
        if (names.has(dep) && visit(dep)) return true;
      visiting.delete(name);
      done.add(name);
      return false;
    }
    if (d.tasks.some((t) => visit(t.name))) add('spec.tasks', 'Dependencies form a cycle.');
  }
  return errors;
}

export function manifestSpecJSON(draft: Draft) {
  return (
    stringifyJSON(
      manifestObject(draft).spec,
      (_key, value) => (typeof value === 'bigint' ? new LosslessNumber(value.toString()) : value),
      2,
    ) ?? '{}'
  );
}
