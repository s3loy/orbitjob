import { appendFile, mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { dirname, join, posix, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { deflateRawSync } from 'node:zlib';
import { parse } from 'yaml';
import Ajv from 'ajv';

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repo = resolve(web, '..');
const output = join(web, 'public/ai/orbitjob-workloads');
const check = process.argv.includes('--check');
const sources = [
  ['api/openapi.yaml', 'references/openapi.yaml'],
  [
    'charts/orbitjob/crds/workloads.orbitjob.io_scheduledjobs.yaml',
    'references/scheduledjob-crd.yaml',
  ],
  [
    'charts/orbitjob/crds/workloads.orbitjob.io_workflowjobs.yaml',
    'references/workflowjob-crd.yaml',
  ],
  ['USAGE.md', 'references/usage.md'],
  ...[
    'docs/getting-started.md',
    'docs/concepts/execution-model.md',
    'docs/reference/workload-resources.md',
    'docs/reference/http-conventions.md',
    'docs/reference/access-policies.md',
    ...[
      'manage-jobs',
      'trigger-and-observe',
      'cancel-a-run',
      'read-logs',
      'manage-access',
      'call-api',
      'checks-and-slos',
      'functions-and-workflows',
    ].map((id) => `docs/tasks/${id}.md`),
  ].map((source) => [source, `references/${source}`]),
];
const files = new Map();
const provenance = {};
function portableMarkdown(text, source, targetFile) {
  const packaged = new Map(sources);
  let fence;
  const content = text
    .split('\n')
    .map((line) => {
      const marker = line.match(/^ {0,3}(`{3,}|~{3,})/);
      if (marker) {
        if (!fence) fence = marker[1];
        else if (marker[1][0] === fence[0] && marker[1].length >= fence.length) fence = undefined;
        return line;
      }
      if (fence) return line;
      // Preserve code spans, fragment links and external URLs. Resolve each
      // repository link from its source before mapping it into the package.
      return line.replace(
        /(`+)[^`]*\1|(?<!!)\[([^\]\n]+)\]\(([^()\s]+)\)/g,
        (match, code, label, target) => {
          if (code || /^(?:[a-z][a-z\d+.-]*:|\/\/|#)/i.test(target)) return match;
          const [, path, suffix] = target.match(/^([^?#]*)(.*)$/);
          const resolved = posix.normalize(posix.join(posix.dirname(source), path));
          const bundled = packaged.get(resolved);
          return bundled
            ? `[${label}](${posix.relative(posix.dirname(targetFile), bundled)}${suffix})`
            : `${label} (repository: \`${target}\`)`;
        },
      );
    })
    .join('\n');
  return (
    `> Repository source: ${source}. References marked repository require a source checkout.\n\n` +
    content
  );
}
for (const [source, target] of sources) {
  const bytes = await readFile(join(repo, source));
  const generated = source.endsWith('.md')
    ? Buffer.from(portableMarkdown(bytes.toString(), source, target))
    : bytes;
  files.set(target, generated);
  provenance[target] = {
    source,
    sourceSha256: createHash('sha256').update(bytes).digest('hex'),
    generatedSha256: createHash('sha256').update(generated).digest('hex'),
  };
}
const spec = parse(files.get('references/openapi.yaml').toString());
const apiLines = [
  '# Workload API index',
  '',
  'Generated from api/openapi.yaml. See openapi.yaml for request bodies, authorization, errors and response schemas.',
  '',
];
for (const [path, methods] of Object.entries(spec.paths)) {
  if (!/^\/api\/v1\/(definitions|jobs|instances|workflows|functions)(?:\/|$)/.test(path)) continue;
  for (const [method, operation] of Object.entries(methods)) {
    if (!['get', 'post', 'put', 'delete', 'patch'].includes(method)) continue;
    apiLines.push(
      '- `' +
        method.toUpperCase() +
        ' ' +
        path +
        '` — ' +
        (operation.summary ?? operation.operationId),
    );
  }
}
files.set('references/api-index.md', Buffer.from(apiLines.join('\n') + '\n'));
files.set(
  'sources.json',
  Buffer.from(
    JSON.stringify({ generatedFrom: 'repository source files', files: provenance }, null, 2) + '\n',
  ),
);

// Validate shipped examples against the same CRD schemas used by Kubernetes.
const ajv = new Ajv({ strict: false, allErrors: true });
ajv.addFormat('int32', true).addFormat('int64', true).addFormat('date-time', true);
for (const [example, crd] of [
  ['assets/manual-job.yaml', 'references/scheduledjob-crd.yaml'],
  ['assets/manual-workflow.yaml', 'references/workflowjob-crd.yaml'],
]) {
  const bytes = await readFile(join(output, example));
  const document = parse(bytes.toString());
  const definition = parse(files.get(crd).toString());
  const version = definition.spec.versions.find((version) => version.name === 'v1alpha1');
  const validate = ajv.compile(version.schema.openAPIV3Schema);
  if (
    document.apiVersion !== `${definition.spec.group}/v1alpha1` ||
    document.kind !== definition.spec.names.kind ||
    document.spec?.executionPolicyVersion !== 1 ||
    !validate(document)
  )
    throw new Error(`Invalid assistant example ${example}: ${JSON.stringify(validate.errors)}`);
  files.set(example, bytes);
}
files.set('SKILL.md', await readFile(join(output, 'SKILL.md')));

async function store(path, bytes) {
  if (check) {
    if (!(await readFile(path).catch(() => Buffer.alloc(0))).equals(bytes))
      throw new Error(`Workload assistant drift: ${relative(web, path)}. Run npm run generate.`);
    return;
  }
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, Buffer.alloc(0));
  for (let index = 0; index < bytes.length; index += 8192)
    await appendFile(path, bytes.subarray(index, index + 8192));
}
for (const [name, bytes] of files) await store(join(output, name), bytes);

// A deterministic ZIP needs no shell commands, archive package or network access.
function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}
function archive(entries) {
  const local = [],
    central = [];
  let offset = 0;
  for (const [path, data] of entries) {
    const name = Buffer.from(`orbitjob-workloads/${path}`);
    const compressed = deflateRawSync(data);
    const crc = crc32(data);
    const header = Buffer.alloc(30);
    header.writeUInt32LE(0x04034b50, 0);
    header.writeUInt16LE(20, 4);
    header.writeUInt16LE(0x800, 6);
    header.writeUInt16LE(8, 8);
    header.writeUInt16LE(33, 12); // 1980-01-01, fixed for reproducibility.
    header.writeUInt32LE(crc, 14);
    header.writeUInt32LE(compressed.length, 18);
    header.writeUInt32LE(data.length, 22);
    header.writeUInt16LE(name.length, 26);
    const entry = Buffer.alloc(46);
    entry.writeUInt32LE(0x02014b50, 0);
    entry.writeUInt16LE(20, 4);
    header.copy(entry, 6, 4, 30);
    entry.writeUInt32LE(offset, 42);
    local.push(header, name, compressed);
    central.push(entry, name);
    offset += header.length + name.length + compressed.length;
  }
  const index = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(index.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, index, end]);
}
await store(
  join(output, 'orbitjob-workloads.zip'),
  archive([...files].sort(([a], [b]) => a.localeCompare(b))),
);

// Refuse unnoticed packaging leftovers: every downloadable reference has one owner.
async function inventory(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map(async (entry) =>
        entry.isDirectory()
          ? inventory(join(directory, entry.name))
          : [relative(output, join(directory, entry.name))],
      ),
    )
  ).flat();
}
for (const name of await inventory(output))
  if (!files.has(name) && name !== 'orbitjob-workloads.zip')
    throw new Error(`Unmanaged workload assistant file: ${name}`);
console.log(
  `Workload assistant ${check ? 'checked' : 'generated'}: ${files.size} files; CRD examples validated`,
);
