import { describe, expect, it, vi } from 'vitest';
import type { Definition } from './api/definitions';
import type { Job } from './api/contracts';
import { APIError, PAGE_SIZE } from './api/transport';
import { checkDraft, manifestObject, manifestSpecJSON } from './api/manifests';
import { clearAdministrationSession, rejectAdministrationCredential } from './administration-state';
import {
  draftFromJobDefinition,
  helloJobDraft,
  loadJobStartingSources,
  resolveJobStartingPoint,
  type JobStartingSource,
} from './job-starting-point';

const source: JobStartingSource = {
  id: '9007199254740993',
  name: 'billing-export',
  namespace: 'source-space',
  source_uid: 'source-uid',
};
const definition = (spec: string): Definition => ({
  kind: 'jobs',
  name: source.name,
  namespace: source.namespace,
  uid: source.source_uid,
  resource_version: '9007199254740993',
  generation: '9007199254740993',
  spec_json: spec,
  manifest_digest: 'source-digest',
  last_request_id: 'source-operation',
  last_manifest_digest: 'source-receipt',
});

describe('new Job drafts from desired state', () => {
  it('preserves execution settings and exact argv while removing schedule and source identity', () => {
    const spec = {
      executionPolicyVersion: 1,
      retrySafe: true,
      schedule: '0 4 * * *',
      suspend: true,
      concurrencyPolicy: 'Replace',
      misfirePolicy: 'FireOnce',
      timeoutSeconds: 2147483647,
      retryPolicy: { maxAttempts: 7 },
      history: { successfulRuns: 0, failedRuns: 9 },
      jobTemplate: {
        image: 'registry.example.com/reports/export@sha256:example',
        command: ['/app/export'],
        args: ['', 'two words', '$HOME', 'line1\nline2'],
        backoffLimit: 4,
        resources: {
          requests: { cpu: '125m' },
          limits: { memory: '9007199254740993', 'ephemeral-storage': '1Gi' },
        },
      },
    };
    const original = definition(JSON.stringify(spec));
    const draft = draftFromJobDefinition(original, 'target-space');
    expect(draft.name).not.toBe(source.name);
    expect(draft.namespace).toBe('target-space');
    expect(draft.command).toEqual(spec.jobTemplate.command);
    expect(draft.args).toEqual(spec.jobTemplate.args);
    expect(checkDraft(draft)).toEqual([]);
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual({
      ...spec,
      schedule: '',
      suspend: false,
    });
    expect(manifestObject(draft).metadata).toEqual({
      name: draft.name,
      namespace: 'target-space',
    });
    expect(
      JSON.stringify(draft, (_, value) => (typeof value === 'bigint' ? String(value) : value)),
    ).not.toMatch(/source-uid|source-digest|source-operation|source-receipt/);
    expect(original).toEqual(definition(JSON.stringify(spec)));
  });

  it('keeps an unselected target namespace unselected instead of using the source namespace', () => {
    const draft = draftFromJobDefinition(
      definition('{"jobTemplate":{"image":"busybox:1.37.0"}}'),
      '',
    );
    expect(draft.namespace).toBe('');
    expect(checkDraft(draft).map((issue) => issue.path)).toContain('metadata.namespace');
    expect(manifestSpecJSON(draft)).not.toContain('source-space');
  });

  it('preserves omitted optional settings and allows restoring the original schedule after reuse', () => {
    const spec = '{"schedule":"0 4 * * *","suspend":true,"jobTemplate":{"image":"busybox:1.37.0"}}';
    const draft = draftFromJobDefinition(definition(spec), 'target-space');
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual({
      schedule: '',
      suspend: false,
      jobTemplate: { image: 'busybox:1.37.0' },
    });
    draft.schedule = '0 4 * * *';
    draft.suspend = true;
    expect(JSON.parse(manifestSpecJSON(draft))).toEqual(JSON.parse(spec));
  });

  it.each([
    ['{"timeoutSeconds":9007199254740993,"jobTemplate":{"image":"busybox"}}', /timeoutSeconds/],
    ['{"jobTemplate":{"image":"busybox","env":[{"name":"TOKEN","value":"secret"}]}}', /env/],
    ['{"jobTemplate":{"image":"busybox","envFrom":[]}}', /envFrom/],
    ['{"jobTemplate":{"image":"busybox"},"futureBinding":"secret"}', /futureBinding/],
  ])('refuses unsupported desired state without dropping fields', (spec, problem) => {
    expect(() => draftFromJobDefinition(definition(spec), 'target-space')).toThrow(problem);
  });

  it('creates an editable valid suggested name even at source DNS label limits', () => {
    const original = definition('{"jobTemplate":{"image":"busybox:1.37.0"}}');
    for (const name of ['a'.repeat(63), `${'a'.repeat(58)}-copy`, 'reports.daily']) {
      const draft = draftFromJobDefinition({ ...original, name }, 'target-space');
      expect(draft.name).not.toBe(name);
      expect(checkDraft(draft)).toEqual([]);
    }
  });

  it('prepares a complete hello command with no schedule and keeps the caller namespace', () => {
    const draft = helloJobDraft('target-space');
    expect(checkDraft(draft)).toEqual([]);
    expect(draft).toMatchObject({
      namespace: 'target-space',
      image: 'busybox:1.37.0',
      command: ['printf'],
      args: ['Hello from OrbitJob!\\n'],
      schedule: '',
      suspend: false,
      attempts: '1',
      backoff: '0',
      retrySafe: false,
    });
    expect(helloJobDraft('').namespace).toBe('');
  });
});

describe('readable Job sources', () => {
  it('paginates from the unfiltered page without reading definitions during browsing', async () => {
    const row: Job = {
      ...source,
      tenant_id: 'workspace',
      generation: 1,
      actor: 'operator',
      schedule: '',
      schedule_summary: 'Manual only',
      source_kind: 'WorkflowJob',
      suspend: false,
      concurrency_policy: 'Forbid',
      misfire_policy: 'Skip',
      created_at: '2026-11-16T00:00:00Z',
    };
    const client = {
      jobs: vi.fn(async () => Array.from({ length: PAGE_SIZE }, () => row)),
      definition: vi.fn(),
    };
    expect(await loadJobStartingSources(client, PAGE_SIZE, 'billing')).toEqual({
      items: [],
      hasMore: true,
    });
    expect(client.jobs).toHaveBeenCalledWith(PAGE_SIZE, 'billing');
    expect(client.definition).not.toHaveBeenCalled();
  });

  it('uses the authoritative definition instead of a projection to construct the draft', async () => {
    const client = {
      definition: vi.fn(async () => definition('{"jobTemplate":{"image":"authoritative-image"}}')),
    };
    const draft = await resolveJobStartingPoint(client, source, 'target-space');
    expect(client.definition).toHaveBeenCalledWith('jobs', 'source-space', 'billing-export');
    expect(draft.image).toBe('authoritative-image');
  });

  it.each([{ uid: 'replacement-uid' }, { name: 'another-job' }, { namespace: 'another-space' }])(
    'rejects a changed source rather than copying another object',
    async (changed) => {
      const client = {
        definition: vi.fn(async () => ({
          ...definition('{"jobTemplate":{"image":"busybox"}}'),
          ...changed,
        })),
      };
      await expect(resolveJobStartingPoint(client, source, 'target-space')).rejects.toThrow(
        'changed',
      );
    },
  );

  it('reports read permission errors instead of returning an empty source list', async () => {
    const error = new APIError('Permission required to read Jobs.', 403);
    const client = {
      jobs: vi.fn(async () => {
        throw error;
      }),
    };
    await expect(loadJobStartingSources(client, 0, '')).rejects.toBe(error);
  });

  it('stops new source requests after credential rejection', async () => {
    const client = { jobs: vi.fn(async () => []) };
    const error = new APIError('Sign in required.', 401);
    rejectAdministrationCredential(client, error);
    await expect(loadJobStartingSources(client, 0, '')).rejects.toBe(error);
    expect(client.jobs).not.toHaveBeenCalled();
  });

  it('rejects a late successful definition after the connection has closed', async () => {
    let finish!: (value: Definition) => void;
    const client = {
      definition: vi.fn(
        () =>
          new Promise<Definition>((resolve) => {
            finish = resolve;
          }),
      ),
    };
    const request = resolveJobStartingPoint(client, source, 'target-space');
    clearAdministrationSession(client);
    finish(definition('{"jobTemplate":{"image":"busybox"}}'));
    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('rejects a late successful definition if another request invalidated the credential', async () => {
    let finish!: (value: Definition) => void;
    const client = {
      definition: vi.fn(
        () =>
          new Promise<Definition>((resolve) => {
            finish = resolve;
          }),
      ),
    };
    const request = resolveJobStartingPoint(client, source, 'target-space');
    const error = new APIError('Sign in required.', 401);
    rejectAdministrationCredential(client, error);
    finish(definition('{"jobTemplate":{"image":"busybox"}}'));
    await expect(request).rejects.toBe(error);
  });
});
