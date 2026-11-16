import { describe, expect, it } from 'vitest';
import { parse, stringify } from 'yaml';
import { importDefinitionSpec, importManifestYAML, previewManifestImport } from './manifest-import';
import {
  checkDraft,
  initialDraft,
  manifestObject,
  manifestSpecJSON,
  manifestYAML,
  type ManifestKind,
} from './manifests';

const apiVersion = 'workloads.orbitjob.io/v1alpha1';
const resource = (
  kind: string,
  spec: unknown,
  metadata: Record<string, unknown> = { name: 'imported-workload', namespace: 'orbitjob' },
) => ({ apiVersion, kind, metadata, spec });
const job = {
  jobTemplate: {
    image: 'busybox:1.37.0',
    command: ['sh', '-c'],
    args: ['', 'two words', '$HOME', 'line1\nline2'],
    backoffLimit: 2,
  },
  history: { failedRuns: 7 },
};
const workflow = (number: bigint) => ({
  timeZone: 'Asia/Shanghai',
  history: { successfulRuns: 5 },
  tasks: [
    { name: 'extract', jobRef: { name: 'extract-job' } },
    {
      name: 'publish',
      jobRef: { name: 'publish-job' },
      dependsOn: ['extract'],
      condition: {
        type: 'all',
        rules: [{ task: 'extract', field: 'duration_ms', operator: 'ge', number }],
      },
    },
  ],
});
const exact = (value: unknown): unknown => parse(stringify(value), { intAsBigInt: true });

describe('online desired-state projection', () => {
  it('keeps advanced changes and saved defaults when another form field changes', () => {
    const spec = { ...job, timeoutSeconds: 321, concurrencyPolicy: 'Allow' };
    const draft = importDefinitionSpec(JSON.stringify(spec), 'ScheduledJob', 'orbitjob', 'report');
    expect(draft.timeout).toBe('321');
    expect(draft.args).toEqual(job.jobTemplate.args);
    draft.image = 'busybox:1.37.1';
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual({
      ...spec,
      jobTemplate: { ...job.jobTemplate, image: 'busybox:1.37.1' },
    });
    expect(JSON.parse(manifestSpecJSON(draft))).not.toHaveProperty('retryPolicy');
  });

  it('preserves exact workflow operands and supported task details through form edits', () => {
    const input = `{
      "timeZone":"Asia/Shanghai","history":{"successfulRuns":5},
      "tasks":[
        {"name":"extract","jobRef":{"name":"extract-job"}},
        {"name":"publish","jobRef":{"name":"publish-job"},"dependsOn":["extract"],
          "condition":{"type":"all","rules":[
            {"task":"extract","field":"duration_ms","operator":"ge","number":9223372036854775807}
          ]}}
      ]
    }`;
    const draft = importDefinitionSpec(input, 'WorkflowJob', 'orbitjob', 'close');
    expect(draft.tasks[1].rules[0].number).toBe('9223372036854775807');
    draft.timeout = '321';
    expect(parse(manifestYAML(draft), { intAsBigInt: true }).spec).toEqual({
      ...workflow(9223372036854775807n),
      timeoutSeconds: 321n,
      history: { successfulRuns: 5n },
    });
  });

  it.each([
    ['{"jobTemplate":{"image":"busybox"},"unknownOption":true}', /unknownOption/],
    ['{"jobTemplate":{"image":"busybox"},"timeoutSeconds":1.5}', /whole-number/],
    ['{"jobTemplate":{"image":"busybox"},"timeoutSeconds":1,"timeoutSeconds":2}', /duplicate/],
    ['{"jobTemplate":', /JSON|object|expected|end/i],
  ])('refuses unprojectable JSON instead of silently discarding it', (input, error) => {
    expect(() => importDefinitionSpec(input, 'ScheduledJob', 'orbitjob', 'report')).toThrow(error);
  });
});

describe('editable YAML import', () => {
  it.each(['ScheduledJob', 'WorkflowJob'] as ManifestKind[])(
    'leaves omitted %s options visibly unspecified instead of claiming initial form defaults',
    (kind) => {
      const spec =
        kind === 'ScheduledJob'
          ? { jobTemplate: { image: 'busybox:1.37.0' } }
          : { tasks: [{ name: 'extract', jobRef: { name: 'extract-job' } }] };
      const draft = importManifestYAML(stringify(resource(kind, spec)), kind);
      for (const field of [
        'schedule',
        'timeZone',
        'timeout',
        'success',
        'failed',
        'concurrency',
        'misfire',
        'failPolicy',
        'backoff',
        'attempts',
      ] as const)
        expect(draft[field], field).toBe('');
      expect(checkDraft(draft)).toEqual([]);
      expect(parse(manifestYAML(draft), { intAsBigInt: true }).spec).toEqual(spec);
      expect(JSON.parse(manifestSpecJSON(draft))).toEqual(spec);
      draft.concurrency = 'Forbid';
      draft.timeout = '120';
      draft.success = '0';
      expect(checkDraft(draft)).toEqual([]);
      expect(JSON.parse(manifestSpecJSON(draft))).toEqual({
        ...spec,
        concurrencyPolicy: 'Forbid',
        timeoutSeconds: 120,
        history: { successfulRuns: 0 },
      });
      expect(initialDraft(kind).concurrency).toBe('Forbid');
      expect(initialDraft(kind).misfire).toBe('Skip');
      expect(initialDraft(kind).timeout).toBe('120');
    },
  );

  it('keeps explicit zero and explicit optional choices distinct from omission', () => {
    const spec = {
      jobTemplate: { image: 'busybox:1.37.0', backoffLimit: 0 },
      timeoutSeconds: 0,
      history: { successfulRuns: 0, failedRuns: 0 },
      retryPolicy: { maxAttempts: 1 },
      concurrencyPolicy: 'Allow',
      misfirePolicy: 'FireOnce',
    };
    const draft = importManifestYAML(stringify(resource('ScheduledJob', spec)), 'ScheduledJob');
    expect([
      draft.timeout,
      draft.success,
      draft.failed,
      draft.backoff,
      draft.attempts,
      draft.concurrency,
      draft.misfire,
    ]).toEqual(['0', '0', '0', '0', '1', 'Allow', 'FireOnce']);
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual(spec);
    const workflowDraft = importManifestYAML(
      stringify(
        resource('WorkflowJob', { ...workflow(1n), timeZone: 'UTC', failPolicy: 'Continue' }),
      ),
      'WorkflowJob',
    );
    expect(workflowDraft.timeZone).toBe('UTC');
    expect(workflowDraft.failPolicy).toBe('Continue');
  });

  it('roundtrips a supported spec without inserting form defaults or losing argument boundaries', () => {
    const draft = importManifestYAML(stringify(resource('ScheduledJob', job)), 'ScheduledJob');
    expect(checkDraft(draft)).toEqual([]);
    expect(parse(manifestYAML(draft), { intAsBigInt: true }).spec).toEqual(exact(job));
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual(job);
    expect(draft.command).toEqual(['sh', '-c']);
    expect(draft.args).toEqual(job.jobTemplate.args);
  });

  it('overlays only edited nested fields and leaves omitted supported values omitted', () => {
    const draft = importManifestYAML(stringify(resource('ScheduledJob', job)), 'ScheduledJob');
    const source = structuredClone(draft.sourceSpec);
    draft.image = 'alpine:3.22';
    draft.failed = '4';
    draft.command.push('literal extra argument');
    const spec = parse(manifestYAML(draft), { intAsBigInt: true }).spec;
    expect(spec).toEqual(
      exact({
        ...job,
        history: { failedRuns: 4 },
        jobTemplate: {
          ...job.jobTemplate,
          image: 'alpine:3.22',
          command: [...job.jobTemplate.command, 'literal extra argument'],
        },
      }),
    );
    expect(spec.history).not.toHaveProperty('successfulRuns');
    expect(spec).not.toHaveProperty('retryPolicy');
    expect(draft.sourceSpec).toEqual(source);
  });

  it.each([9007199254740993n, -9223372036854775808n, 9223372036854775807n])(
    'keeps the exact signed int64 operand %s in YAML and JSON',
    (number) => {
      const original = workflow(number);
      const draft = importManifestYAML(stringify(resource('WorkflowJob', original)), 'WorkflowJob');
      expect(checkDraft(draft)).toEqual([]);
      expect(draft.tasks[1].rules[0].number).toBe(number.toString());
      expect(parse(manifestYAML(draft), { intAsBigInt: true }).spec).toEqual(exact(original));
      expect(manifestSpecJSON(draft)).toContain(number.toString());
      expect(manifestSpecJSON(draft)).not.toContain(`"${number}"`);
      draft.timeout = '120';
      expect(
        parse(manifestYAML(draft), { intAsBigInt: true }).spec.tasks[1].condition.rules[0].number,
      ).toBe(number);
    },
  );

  it('keeps task details attached to stable task names while reordering and editing references', () => {
    const draft = importManifestYAML(
      stringify(resource('WorkflowJob', workflow(9007199254740993n))),
      'WorkflowJob',
    );
    draft.tasks = [draft.tasks[1], draft.tasks[0]];
    draft.tasks[0] = { ...draft.tasks[0], job: 'new-publish-job' };
    expect(checkDraft(draft)).toEqual([]);
    const spec = parse(manifestYAML(draft), { intAsBigInt: true }).spec;
    expect(spec.tasks[0]).toEqual(
      exact({ ...workflow(9007199254740993n).tasks[1], jobRef: { name: 'new-publish-job' } }),
    );
    expect(spec.tasks[1]).toEqual(workflow(9007199254740993n).tasks[0]);
    draft.tasks[0] = { ...draft.tasks[0], combination: '', dependencies: [] };
    const edited = parse(manifestYAML(draft), { intAsBigInt: true }).spec;
    expect(edited.tasks[0]).not.toHaveProperty('condition');
    expect(edited.tasks[0]).not.toHaveProperty('dependsOn');
  });

  it('retains supported empty nested objects and allows adding a previously omitted value', () => {
    const spec = { jobTemplate: { image: 'busybox:1.37.0' }, history: {}, retryPolicy: {} };
    const draft = importManifestYAML(stringify(resource('ScheduledJob', spec)), 'ScheduledJob');
    expect(manifestObject(draft).spec).toEqual(spec);
    draft.attempts = '2';
    expect(parse(manifestYAML(draft), { intAsBigInt: true }).spec.retryPolicy).toEqual({
      maxAttempts: 2n,
    });
  });

  it.each([
    ['', /Paste/],
    ['metadata: [broken', /YAML cannot be read/],
    [
      stringify(resource('ScheduledJob', job)) +
        '\n---\n' +
        stringify(resource('ScheduledJob', job)),
      /exactly one/,
    ],
    ['apiVersion: x\napiVersion: y\n', /duplicate field/],
    [stringify({ ...resource('ScheduledJob', job), apiVersion: 'batch/v1' }), /apiVersion/],
    [stringify(resource('WorkflowJob', workflow(1n))), /This page imports ScheduledJob/],
    [stringify(resource('ScheduledJob', job, { namespace: 'orbitjob' })), /metadata.name/],
    [
      stringify(
        resource('ScheduledJob', job, {
          name: 'work',
          namespace: 'orbitjob',
          labels: { team: 'ops' },
        }),
      ),
      /metadata.labels cannot be edited/,
    ],
    [
      stringify({ ...resource('ScheduledJob', job), status: { activeRevision: 1 } }),
      /Document.status is not supported/,
    ],
    [
      stringify(
        resource('ScheduledJob', {
          jobTemplate: { image: 'busybox', env: [{ name: 'MODE', value: 'prod' }] },
        }),
      ),
      /spec.jobTemplate.env is not supported/,
    ],
    [
      stringify(
        resource('ScheduledJob', {
          jobTemplate: { image: 'busybox', resources: { limits: { 'nvidia.com/gpu': '1' } } },
        }),
      ),
      /spec.jobTemplate.resources.limits.nvidia.com\/gpu is not supported/,
    ],
    [
      stringify(
        resource('ScheduledJob', {
          jobTemplate: { image: 'busybox' },
          timeoutSeconds: 2147483648n,
        }),
      ),
      /timeoutSeconds/,
    ],
  ])('rejects unsupported or malformed documents with recovery guidance', (yaml, error) => {
    expect(() => importManifestYAML(yaml, 'ScheduledJob')).toThrow(error);
  });

  it('rejects aliases before any expansion', () => {
    const yaml = `apiVersion: ${apiVersion}\nkind: ScheduledJob\nmetadata: {name: work, namespace: orbitjob}\nspec:\n  jobTemplate: &template {image: busybox}\n  retryPolicy: *template\n`;
    expect(() => importManifestYAML(yaml, 'ScheduledJob')).toThrow(/aliases/);
  });

  it.each(['9007199254740993.0', '9.007199254740993e15', '1.0000000000000001', '.nan', '.inf'])(
    'rejects a floating operand before it can round: %s',
    (number) => {
      const yaml = stringify(resource('WorkflowJob', workflow(1n))).replace(
        'number: 1',
        `number: ${number}`,
      );
      expect(() => importManifestYAML(yaml, 'WorkflowJob')).toThrow(/whole-number integers/);
    },
  );

  it.each([-9223372036854775809n, 9223372036854775808n])('rejects int64 overflow %s', (number) => {
    expect(() =>
      importManifestYAML(stringify(resource('WorkflowJob', workflow(number))), 'WorkflowJob'),
    ).toThrow(/64-bit/);
  });

  it('does not invent a missing numeric rule operand or preserve conflicting operand fields', () => {
    const yaml = stringify(resource('WorkflowJob', workflow(1n)));
    expect(() => importManifestYAML(yaml.replace(/\s+number: 1/, ''), 'WorkflowJob')).toThrow(
      /number is required/,
    );
    const conflicting = parse(yaml, { intAsBigInt: true });
    conflicting.spec.tasks[1].condition.rules[0].values = ['Succeeded'];
    expect(() => importManifestYAML(stringify(conflicting), 'WorkflowJob')).toThrow(
      /Remove values/,
    );
    expect(() =>
      importManifestYAML(
        yaml.replace('operator: ge', 'operator: eq').replace('field: duration_ms', 'field: phase'),
        'WorkflowJob',
      ),
    ).toThrow(/Remove number/);
  });
});

describe('import cleanup preview', () => {
  it('keeps exported server metadata out of the draft until the user confirms', () => {
    const exported = resource('ScheduledJob', { jobTemplate: { image: 'busybox:1.37.0' } });
    const input = stringify({
      ...exported,
      metadata: { ...exported.metadata, uid: 'server-owned', resourceVersion: '7' },
      status: { ready: true },
    });
    const preview = previewManifestImport(input, 'ScheduledJob');
    expect(preview.removedFields).toEqual(['metadata.uid', 'metadata.resourceVersion', 'status']);
    expect(preview.cleanedYAML).not.toContain('server-owned');
    expect(preview.draft.image).toBe('busybox:1.37.0');
    expect(input).toContain('server-owned');
    expect(() => importManifestYAML(input, 'ScheduledJob')).toThrow();
  });
  it('continues to reject unsupported execution fields and measures UTF-8 bytes', () => {
    const input = stringify(
      resource('ScheduledJob', {
        jobTemplate: { image: 'busybox:1.37.0', env: [{ name: 'MODE', value: 'prod' }] },
      }),
    );
    expect(() => previewManifestImport(input, 'ScheduledJob')).toThrow(/env/);
    const oversized =
      stringify(resource('ScheduledJob', { jobTemplate: { image: 'busybox:1.37.0' } })) +
      '\n#' +
      '\u754c'.repeat(90000);
    expect(oversized.length).toBeLessThan(262144);
    expect(() => previewManifestImport(oversized, 'ScheduledJob')).toThrow(/too large/);
  });
});
