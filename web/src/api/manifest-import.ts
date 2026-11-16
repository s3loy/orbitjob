import { isAlias, isCollection, isScalar, parseAllDocuments, visit } from 'yaml';
import { parse as parseJSON } from 'lossless-json';
import {
  checkDraft,
  initialDraft,
  manifestObject,
  manifestYAML,
  manifestSchema,
  type Draft,
  type ManifestKind,
  type Rule,
  type Task,
} from './manifests';

type ObjectValue = Record<string, unknown>;
function object(value: unknown, path: string): ObjectValue {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error(`${path} must be an object. Use the example for this workload type.`);
  return value as ObjectValue;
}
function keys(value: ObjectValue, allowed: string[], path: string) {
  const unsupported = Object.keys(value).filter((key) => !allowed.includes(key));
  if (unsupported.length)
    throw new Error(
      `${path}.${unsupported[0]} is not supported by this installation's workload API. Remove it or use a supported field; importing never discards fields silently.`,
    );
}
function text(value: unknown): string {
  return value === undefined ? '' : typeof value === 'string' ? value : String(value);
}
function list(value: unknown): string[] {
  return Array.isArray(value) ? value.map((item) => text(item)) : [];
}
function scalarFields(value: ObjectValue, allowed: string[], path: string) {
  keys(value, allowed, path);
}

// Keep the import boundary aligned with the closed typed Apply API. The CRD
// validator checks values; these keys prevent Kubernetes pruning or the Go
// decoder from silently rejecting an apparently successful import later.
function checkSupportedSpec(spec: ObjectValue, kind: ManifestKind) {
  keys(
    spec,
    kind === 'ScheduledJob'
      ? [
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
        ]
      : [
          'executionPolicyVersion',
          'schedule',
          'timeZone',
          'suspend',
          'concurrencyPolicy',
          'failPolicy',
          'timeoutSeconds',
          'history',
          'tasks',
        ],
    'spec',
  );
  if (spec.history !== undefined)
    scalarFields(
      object(spec.history, 'spec.history'),
      ['successfulRuns', 'failedRuns'],
      'spec.history',
    );
  if (kind === 'ScheduledJob') {
    const template = object(spec.jobTemplate, 'spec.jobTemplate');
    keys(template, ['image', 'command', 'args', 'backoffLimit', 'resources'], 'spec.jobTemplate');
    if (template.resources !== undefined) {
      const resources = object(template.resources, 'spec.jobTemplate.resources');
      keys(resources, ['requests', 'limits'], 'spec.jobTemplate.resources');
      for (const group of ['requests', 'limits'])
        if (resources[group] !== undefined) {
          const values = object(resources[group], `spec.jobTemplate.resources.${group}`);
          keys(
            values,
            ['cpu', 'memory', 'ephemeral-storage'],
            `spec.jobTemplate.resources.${group}`,
          );
          for (const [name, value] of Object.entries(values))
            if (typeof value !== 'string' || !value.trim())
              throw new Error(
                `spec.jobTemplate.resources.${group}.${name} must be a Kubernetes quantity string.`,
              );
        }
    }
    if (spec.retryPolicy !== undefined)
      scalarFields(
        object(spec.retryPolicy, 'spec.retryPolicy'),
        ['maxAttempts'],
        'spec.retryPolicy',
      );
    return;
  }
  if (!Array.isArray(spec.tasks)) throw new Error('spec.tasks must be a list of workflow tasks.');
  spec.tasks.forEach((value, index) => {
    const path = `spec.tasks[${index}]`;
    const task = object(value, path);
    keys(task, ['name', 'jobRef', 'dependsOn', 'condition'], path);
    keys(object(task.jobRef, `${path}.jobRef`), ['name'], `${path}.jobRef`);
    if (task.condition !== undefined) {
      const condition = object(task.condition, `${path}.condition`);
      keys(condition, ['type', 'rules'], `${path}.condition`);
      if (!Array.isArray(condition.rules))
        throw new Error(`${path}.condition.rules must be a list of rules.`);
      condition.rules.forEach((value, ruleIndex) => {
        const rulePath = `${path}.condition.rules[${ruleIndex}]`;
        const rule = object(value, rulePath);
        keys(rule, ['task', 'field', 'operator', 'values', 'number'], rulePath);
        const numeric = ['lt', 'le', 'gt', 'ge'].includes(text(rule.operator));
        if (numeric && rule.number === undefined)
          throw new Error(
            `${rulePath}.number is required for a numeric comparison. Add an integer operand before importing.`,
          );
        if (numeric && Array.isArray(rule.values) && rule.values.length)
          throw new Error(
            `${rulePath} uses a numeric comparison. Remove values and supply number instead.`,
          );
        if (!numeric && rule.number !== undefined)
          throw new Error(
            `${rulePath} uses a phase comparison. Remove number and supply values instead.`,
          );
      });
    }
  });
}

function workflowTasks(spec: ObjectValue): Task[] {
  return (spec.tasks as ObjectValue[]).map((task) => {
    const condition = task.condition as ObjectValue | undefined;
    return {
      name: text(task.name),
      job: text((task.jobRef as ObjectValue).name),
      dependencies: list(task.dependsOn),
      combination: condition ? (text(condition.type) as Task['combination']) : '',
      rules: condition
        ? (condition.rules as ObjectValue[]).map((rule): Rule => ({
            task: text(rule.task),
            field: text(rule.field),
            operator: text(rule.operator),
            values: list(rule.values),
            number: text(rule.number),
          }))
        : [],
    };
  });
}

/** Import exactly one desired-state resource without changing its numeric values. */
function parseManifestInput(input: string): unknown {
  if (!input.trim()) throw new Error('Paste a ScheduledJob or WorkflowJob YAML document first.');
  if (new TextEncoder().encode(input).byteLength > 262144)
    throw new Error('This YAML is too large to import. Keep one workload document under 256 KiB.');
  let value: unknown;
  try {
    const documents = parseAllDocuments(input, {
      intAsBigInt: true,
      uniqueKeys: true,
      stringKeys: true,
      version: '1.2',
      merge: false,
    });
    if (documents.length !== 1)
      throw new Error(
        'Import exactly one workload document. Remove extra YAML documents separated by --- .',
      );
    const document = documents[0];
    if (document.errors.length) {
      const line = document.errors[0].linePos?.[0].line;
      throw new Error(
        `YAML cannot be read${line ? ` near line ${line}` : ''}. Check indentation, colons, and duplicate field names.`,
      );
    }
    if (document.warnings.length || document.directives?.yaml.version !== '1.2')
      throw new Error('Use standard YAML 1.2 without custom tags or version directives.');
    let count = 0;
    visit(document, (_, node, path) => {
      if (path.filter(isCollection).length > 64)
        throw new Error('YAML nesting exceeds 64 levels. Reduce the configuration depth.');
      if (++count > 20000)
        throw new Error('This YAML has too many fields. Keep only one workload configuration.');
      if (isAlias(node))
        throw new Error(
          'YAML aliases and merge references are not supported. Expand each field explicitly before importing.',
        );
      if (isScalar(node) && typeof node.value === 'number')
        throw new Error(
          'Numeric workload fields must use whole-number integers. Write them in decimal notation without a decimal point or exponent to avoid rounding.',
        );
    });
    value = document.toJS({ maxAliasCount: 0 });
  } catch (error) {
    if (error instanceof Error && !(error instanceof RangeError)) throw error;
    throw new Error(
      'YAML could not be imported. Reduce its nesting and use the workload example.',
      { cause: error },
    );
  }
  return value;
}

/** Strict direct import remains available for validated configuration callers. */
export function importManifestYAML(input: string, expectedKind: ManifestKind): Draft {
  return importResource(parseManifestInput(input), expectedKind);
}

export type ManifestImportPreview = { draft: Draft; cleanedYAML: string; removedFields: string[] };
export function previewManifestImport(
  input: string,
  expectedKind: ManifestKind,
): ManifestImportPreview {
  const source = object(parseManifestInput(input), 'Document');
  const metadata = { ...object(source.metadata, 'metadata') };
  const cleaned: ObjectValue = { ...source, metadata };
  const removedFields: string[] = [];
  for (const key of [
    'uid',
    'resourceVersion',
    'generation',
    'creationTimestamp',
    'managedFields',
    'selfLink',
    'deletionTimestamp',
    'deletionGracePeriodSeconds',
  ]) {
    if (Object.hasOwn(metadata, key)) {
      delete metadata[key];
      removedFields.push(`metadata.${key}`);
    }
  }
  if (Object.hasOwn(cleaned, 'status')) {
    delete cleaned.status;
    removedFields.push('status');
  }
  if (metadata.annotations !== undefined) {
    const annotations = { ...object(metadata.annotations, 'metadata.annotations') };
    for (const key of [
      'kubectl.kubernetes.io/last-applied-configuration',
      'orbitjob.io/admin-request-id',
      'orbitjob.io/admin-manifest-digest',
      'orbitjob.io/admin-admission-proof',
    ]) {
      if (Object.hasOwn(annotations, key)) {
        delete annotations[key];
        removedFields.push(`metadata.annotations[${JSON.stringify(key)}]`);
      }
    }
    if (Object.keys(annotations).length) metadata.annotations = annotations;
    else delete metadata.annotations;
  }
  // Arbitrary labels, annotations and unsupported execution fields still fail.
  const draft = importResource(cleaned, expectedKind);
  return { draft, cleanedYAML: manifestYAML(draft), removedFields };
}

/** Project the complete online desired state into the form without rounding integers. */
export function importDefinitionSpec(
  input: string,
  kind: ManifestKind,
  namespace: string,
  name: string,
): Draft {
  if (new TextEncoder().encode(input).byteLength > 262144)
    throw new Error('Keep the advanced spec under 256 KiB.');
  const spec = parseJSON(input, undefined, {
    parseNumber(raw) {
      if (!/^-?\d+$/.test(raw))
        throw new Error('Numeric workload fields must use whole-number integers.');
      return BigInt(raw);
    },
    onDuplicateKey({ key }) {
      throw new Error(`The advanced spec contains duplicate field ${key}.`);
    },
  });
  return importResource(
    { apiVersion: manifestSchema.apiVersion, kind, metadata: { namespace, name }, spec },
    kind,
  );
}

function importResource(value: unknown, expectedKind: ManifestKind): Draft {
  const resource = object(value, 'Document');
  keys(resource, ['apiVersion', 'kind', 'metadata', 'spec'], 'Document');
  if (resource.apiVersion !== manifestSchema.apiVersion)
    throw new Error(
      `Use apiVersion: ${manifestSchema.apiVersion}. This installation cannot import another API version.`,
    );
  if (resource.kind !== expectedKind)
    throw new Error(
      `This page imports ${expectedKind} documents. Choose the matching workload page or change kind.`,
    );
  const metadata = object(resource.metadata, 'metadata');
  const unsupportedMetadata = Object.keys(metadata).filter(
    (key) => !['name', 'namespace'].includes(key),
  );
  if (unsupportedMetadata.length)
    throw new Error(
      `metadata.${unsupportedMetadata[0]} cannot be edited through this workload API. Keep only metadata.name and metadata.namespace; exported cluster status, labels and annotations are not imported.`,
    );
  if (
    typeof metadata.name !== 'string' ||
    !metadata.name ||
    typeof metadata.namespace !== 'string' ||
    !metadata.namespace
  )
    throw new Error('Provide both metadata.name and metadata.namespace as text before importing.');
  const spec = object(resource.spec, 'spec');
  checkSupportedSpec(spec, expectedKind);
  const d = initialDraft(expectedKind);
  const history = spec.history as ObjectValue | undefined;
  const template = spec.jobTemplate as ObjectValue | undefined;
  const retry = spec.retryPolicy as ObjectValue | undefined;
  const draft: Draft = {
    ...d,
    executionPolicyVersion:
      spec.executionPolicyVersion === undefined ? undefined : Number(spec.executionPolicyVersion),
    retrySafe: spec.retrySafe === undefined ? undefined : spec.retrySafe === true,
    resources: template?.resources as Draft['resources'],
    name: metadata.name,
    namespace: metadata.namespace,
    schedule: text(spec.schedule),
    timeZone: text(spec.timeZone),
    suspend: spec.suspend === true,
    concurrency: text(spec.concurrencyPolicy),
    timeout: text(spec.timeoutSeconds),
    success: text(history?.successfulRuns),
    failed: text(history?.failedRuns),
    misfire: text(spec.misfirePolicy),
    failPolicy: text(spec.failPolicy),
    image: text(template?.image),
    command: list(template?.command),
    args: list(template?.args),
    backoff: text(template?.backoffLimit),
    attempts: text(retry?.maxAttempts),
    tasks: expectedKind === 'WorkflowJob' ? workflowTasks(spec) : d.tasks,
  };
  draft.sourceFormSpec = structuredClone(manifestObject(draft).spec) as ObjectValue;
  draft.sourceSpec = spec;
  const issues = checkDraft(draft);
  if (issues.length) {
    const first = issues[0];
    throw new Error(
      `Check ${first.path || 'spec'}: ${first.message} Correct the YAML and import it again.`,
    );
  }
  return draft;
}
