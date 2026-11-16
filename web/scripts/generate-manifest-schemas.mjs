import { readFile, writeFile, appendFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
import { parse } from 'yaml';
import Ajv from 'ajv';
import standalone from 'ajv/dist/standalone/index.js';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const ajv = new Ajv({ allErrors: true, strict: false, code: { source: true, esm: true } });
ajv.addFormat('int32', true).addFormat('int64', true);
const info = { apiVersion: 'workloads.orbitjob.io/v1alpha1', sources: {} };
function bounds(schema) {
  if (!schema || typeof schema !== 'object') return;
  if (schema.format === 'int32') {
    schema.minimum = Math.max(schema.minimum ?? -2147483648, -2147483648);
    schema.maximum = Math.min(schema.maximum ?? 2147483647, 2147483647);
  }
  for (const value of Object.values(schema)) {
    if (Array.isArray(value)) value.forEach(bounds);
    else if (value && typeof value === 'object') bounds(value);
  }
}
for (const [name, file] of [
  ['scheduledJob', 'workloads.orbitjob.io_scheduledjobs.yaml'],
  ['workflow', 'workloads.orbitjob.io_workflowjobs.yaml'],
]) {
  const source = await readFile(resolve(root, '../charts/orbitjob/crds', file), 'utf8');
  const schema = parse(source).spec.versions.find((v) => v.name === 'v1alpha1').schema
    .openAPIV3Schema;
  // The draft contains desired state only; readonly status is never exported.
  delete schema.properties.status;
  bounds(schema);
  ajv.addSchema(schema, name);
  info.sources[name] = {
    file: `charts/orbitjob/crds/${file}`,
    sha256: createHash('sha256').update(source).digest('hex'),
  };
}
// Standalone validators preserve the Console CSP: no eval or runtime compiler.
const body = standalone(ajv, {
  validateScheduledJob: 'scheduledJob',
  validateWorkflow: 'workflow',
}).replaceAll('require("ajv/dist/runtime/ucs2length").default', 'ucs2length');
if (body.includes('require('))
  throw new Error('Unhandled standalone runtime helper: add an explicit ESM import.');
const outputs = [
  [
    'src/api/manifest-validators.js',
    '// Generated from CRDs. Run npm run generate.\n' +
      'import runtimeLength from "ajv/dist/runtime/ucs2length.js";\n' +
      'const ucs2length = typeof runtimeLength === "function" ? runtimeLength : runtimeLength.default;\n' +
      body +
      '\n',
  ],
  ['src/api/manifest-schema-info.json', JSON.stringify(info, null, 2) + '\n'],
];
for (const [file, text] of outputs) {
  const path = resolve(root, file);
  if (process.argv.includes('--check')) {
    if ((await readFile(path, 'utf8').catch(() => '')) !== text)
      throw new Error(`Manifest schema drift: ${file}`);
  } else {
    await writeFile(path, '');
    for (let i = 0; i < text.length; i += 8192) await appendFile(path, text.slice(i, i + 8192));
  }
}
