import type { Definition } from './api/definitions';
import type { Job } from './api/contracts';
import { importDefinitionSpec } from './api/manifest-import';
import { initialDraft, manifestObject, type Draft } from './api/manifests';
import { PAGE_SIZE, type Client } from './api/transport';
import {
  administrationAuthenticationError,
  administrationCredential,
} from './administration-state';
import { withCredential } from './credential-requests';
import { workloadOrigin } from './workload-origin';

export type JobStartingSource = Pick<Job, 'id' | 'name' | 'namespace' | 'source_uid'>;

function requireCurrentCredential(client: object) {
  const rejected = administrationAuthenticationError(client);
  if (rejected) throw rejected;
  if (administrationCredential(client).stage === 'closed')
    throw new DOMException('Connection closed', 'AbortError');
}

export async function loadJobStartingSources(
  client: Pick<Client, 'jobs'>,
  offset: number,
  search: string,
) {
  requireCurrentCredential(client);
  const rows = await withCredential(client, () => client.jobs(offset, search));
  requireCurrentCredential(client);
  return {
    items: rows
      .filter(
        (row) => workloadOrigin(row).actionable && row.source_uid && row.namespace && row.name,
      )
      .map(({ id, name, namespace, source_uid }) => ({ id, name, namespace, source_uid })),
    hasMore: rows.length >= PAGE_SIZE,
  };
}

function suggestedJobName(sourceName: string): string {
  const stem = sourceName
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 58)
    .replace(/-+$/, '');
  const suggestion = `${stem || 'job'}-copy`;
  return suggestion === sourceName ? `${stem}-new` : suggestion;
}

// Reusing newly supported execution bindings needs an explicit product decision.
// The importer owns validation; this boundary prevents future additions from
// silently bringing credentials, mounts or other bindings into a new Job.
function requireReusableFields(spec: Record<string, unknown>) {
  const fields = [
    'executionPolicyVersion',
    'retrySafe',
    'schedule',
    'suspend',
    'concurrencyPolicy',
    'misfirePolicy',
    'timeoutSeconds',
    'history',
    'retryPolicy',
    'jobTemplate',
  ];
  const unsupported = Object.keys(spec).find((key) => !fields.includes(key));
  if (unsupported)
    throw new Error(
      `spec.${unsupported} cannot be reused automatically. Configure a new Job or import a reviewed manifest.`,
    );
  const template = spec.jobTemplate as Record<string, unknown>;
  const binding = Object.keys(template).find(
    (key) => !['image', 'command', 'args', 'backoffLimit', 'resources'].includes(key),
  );
  if (binding)
    throw new Error(
      `spec.jobTemplate.${binding} cannot be reused automatically. Configure a new Job or import a reviewed manifest.`,
    );
}

export function draftFromJobDefinition(definition: Definition, namespace: string): Draft {
  if (definition.kind !== 'jobs')
    throw new Error('Choose a ScheduledJob source. Workflows cannot be reused as a Job.');
  // Import validates desired state independently of the unfinished target form.
  // The placeholder is never returned or submitted as the target namespace.
  const imported = importDefinitionSpec(
    definition.spec_json,
    'ScheduledJob',
    'draft-target',
    suggestedJobName(definition.name),
  );
  requireReusableFields(imported.sourceSpec!);
  const draft: Draft = {
    ...imported,
    namespace,
    schedule: '',
    suspend: false,
    tasks: [],
    sourceSpec: undefined,
    sourceFormSpec: undefined,
  };
  return {
    ...draft,
    sourceSpec: { ...imported.sourceSpec, schedule: '', suspend: false },
    sourceFormSpec: structuredClone(manifestObject(draft).spec) as Record<string, unknown>,
  };
}

export async function resolveJobStartingPoint(
  client: Pick<Client, 'definition'>,
  source: JobStartingSource,
  namespace: string,
): Promise<Draft> {
  requireCurrentCredential(client);
  const definition = await withCredential(client, () =>
    client.definition('jobs', source.namespace, source.name),
  );
  requireCurrentCredential(client);
  if (
    !source.source_uid ||
    definition.uid !== source.source_uid ||
    definition.name !== source.name ||
    definition.namespace !== source.namespace
  )
    throw new Error(
      'This source changed since it was listed. Reload the Job list and choose it again; your draft has been kept.',
    );
  return draftFromJobDefinition(definition, namespace);
}

export function helloJobDraft(namespace: string): Draft {
  return {
    ...initialDraft('ScheduledJob'),
    name: 'hello-orbitjob',
    namespace,
    schedule: '',
    suspend: false,
    image: 'busybox:1.37.0',
    command: ['printf'],
    args: ['Hello from OrbitJob!\\n'],
    tasks: [],
  };
}
