import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { posix } from 'node:path';
import { execPath } from 'node:process';
import { fileURLToPath } from 'node:url';
import { inflateRawSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';

const root = new URL('../../', import.meta.url);
const output = new URL('../public/ai/orbitjob-workloads/', import.meta.url);
const expectedSources: Record<string, string> = {
  'references/openapi.yaml': 'api/openapi.yaml',
  'references/scheduledjob-crd.yaml':
    'charts/orbitjob/crds/workloads.orbitjob.io_scheduledjobs.yaml',
  'references/workflowjob-crd.yaml': 'charts/orbitjob/crds/workloads.orbitjob.io_workflowjobs.yaml',
  'references/usage.md': 'USAGE.md',
  'references/docs/getting-started.md': 'docs/getting-started.md',
  'references/docs/concepts/execution-model.md': 'docs/concepts/execution-model.md',
  'references/docs/reference/workload-resources.md': 'docs/reference/workload-resources.md',
  'references/docs/reference/http-conventions.md': 'docs/reference/http-conventions.md',
  'references/docs/reference/access-policies.md': 'docs/reference/access-policies.md',
  'references/docs/tasks/manage-jobs.md': 'docs/tasks/manage-jobs.md',
  'references/docs/tasks/trigger-and-observe.md': 'docs/tasks/trigger-and-observe.md',
  'references/docs/tasks/cancel-a-run.md': 'docs/tasks/cancel-a-run.md',
  'references/docs/tasks/read-logs.md': 'docs/tasks/read-logs.md',
  'references/docs/tasks/manage-access.md': 'docs/tasks/manage-access.md',
  'references/docs/tasks/call-api.md': 'docs/tasks/call-api.md',
  'references/docs/tasks/checks-and-slos.md': 'docs/tasks/checks-and-slos.md',
  'references/docs/tasks/functions-and-workflows.md': 'docs/tasks/functions-and-workflows.md',
};
const expectedFiles = [
  'SKILL.md',
  'assets/manual-job.yaml',
  'assets/manual-workflow.yaml',
  'references/api-index.md',
  ...Object.keys(expectedSources),
  'sources.json',
].sort();
const bytes = readFileSync(new URL('orbitjob-workloads.zip', output));
const entries = new Map<string, Buffer>();
let offset = 0;
while (bytes.readUInt32LE(offset) === 0x04034b50) {
  const size = bytes.readUInt32LE(offset + 18);
  const nameLength = bytes.readUInt16LE(offset + 26);
  const extraLength = bytes.readUInt16LE(offset + 28);
  const name = bytes.subarray(offset + 30, offset + 30 + nameLength).toString();
  const start = offset + 30 + nameLength + extraLength;
  if (bytes.readUInt16LE(offset + 8) !== 8) throw new Error('Expected deflated ZIP entry');
  if (!name.startsWith('orbitjob-workloads/') || entries.has(name))
    throw new Error(`Invalid or duplicate ZIP entry: ${name}`);
  const data = inflateRawSync(bytes.subarray(start, start + size));
  if (data.length !== bytes.readUInt32LE(offset + 22)) throw new Error(`Invalid ZIP size: ${name}`);
  entries.set(name, data);
  offset = start + size;
}

type Provenance = { source: string; sourceSha256: string; generatedSha256: string };
const provenance: { files: Record<string, Provenance> } = JSON.parse(
  readFileSync(new URL('sources.json', output)).toString(),
);
const hash = (data: Buffer) => createHash('sha256').update(data).digest('hex');
const fenced = (text: string) =>
  text.match(/^ {0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\1[ \t]*$/gm) ?? [];
const links = (text: string) =>
  [
    ...text
      .replace(/^ {0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\1[ \t]*$/gm, '')
      .replace(/(`+)[^`]*\1/g, '')
      .matchAll(/!?\[([^\]\n]*)\]\((<[^>]+>|[^\s)]+)\)/g),
  ].map((match) => ({ label: match[1], target: match[2].replace(/^<|>$/g, ''), text: match[0] }));
const isLocal = (target: string) => !/^(?:[a-z][a-z\d+.-]*:|\/\/|#)/i.test(target);

describe('downloadable workload assistant', () => {
  it('ships the generated files unchanged inside the ZIP', () => {
    expect(bytes.readUInt32LE(offset)).toBe(0x02014b50);
    expect([...entries.keys()].sort()).toEqual(
      expectedFiles.map((name) => `orbitjob-workloads/${name}`),
    );
    for (const [name, data] of entries)
      expect(
        data.equals(readFileSync(new URL(name.slice('orbitjob-workloads/'.length), output))),
      ).toBe(true);
  });

  it('resolves every navigable local file link within the extracted package', () => {
    let checked = 0;
    for (const [name, data] of entries) {
      if (!name.endsWith('.md')) continue;
      for (const { target } of links(data.toString())) {
        if (!isLocal(target)) continue;
        const path = decodeURIComponent(target.split(/[?#]/)[0]);
        const resolved = posix.normalize(posix.join(posix.dirname(name), path));
        expect(entries.has(resolved), `${name} -> ${target}`).toBe(true);
        checked++;
      }
    }
    expect(checked).toBeGreaterThan(0);
  });

  it('distinguishes original source hashes from transformed output hashes', () => {
    expect(
      Object.fromEntries(
        Object.entries(provenance.files).map(([target, entry]) => [target, entry.source]),
      ),
    ).toEqual(expectedSources);
    for (const [target, entry] of Object.entries(provenance.files)) {
      expect(entry.sourceSha256).toBe(hash(readFileSync(new URL(entry.source, root))));
      expect(entry.generatedSha256).toBe(hash(readFileSync(new URL(target, output))));
      expect(entry).not.toHaveProperty('sha256');
      if (entry.source.endsWith('.md')) expect(entry.generatedSha256).not.toBe(entry.sourceSha256);
      else expect(entry.generatedSha256).toBe(entry.sourceSha256);
    }
  });

  it('preserves source links, labels, anchors and code across all bundled Markdown', () => {
    const packaged = new Map(
      Object.entries(expectedSources).map(([target, source]) => [
        new URL(source, root).href,
        target,
      ]),
    );
    let repositoryReferences = 0;
    let bundledReferences = 0;
    let anchoredReferences = 0;
    let codeBlocks = 0;
    for (const [targetFile, sourceFile] of Object.entries(expectedSources)) {
      if (!sourceFile.endsWith('.md')) continue;
      const source = readFileSync(new URL(sourceFile, root)).toString();
      const generated = readFileSync(new URL(targetFile, output)).toString();
      const generatedLinks = links(generated);
      expect(generated.startsWith(`> Repository source: ${sourceFile}. `), targetFile).toBe(true);
      expect(generated, targetFile).toContain(
        'References marked repository require a source checkout.',
      );
      expect(fenced(generated), targetFile).toEqual(fenced(source));
      codeBlocks += fenced(source).length;
      for (const link of links(source)) {
        if (!isLocal(link.target)) {
          expect(generated, `${sourceFile} -> ${link.target}`).toContain(link.text);
          continue;
        }
        const path = link.target.split(/[?#]/)[0];
        const suffix = link.target.slice(path.length);
        const bundled = packaged.get(new URL(path, new URL(sourceFile, root)).href);
        if (bundled) {
          const expectedUrl = new URL(`${bundled}${suffix}`, output).href;
          expect(
            generatedLinks.some(
              (candidate) =>
                candidate.label === link.label &&
                new URL(candidate.target, new URL(targetFile, output)).href === expectedUrl,
            ),
            `${sourceFile} -> ${link.target}`,
          ).toBe(true);
          bundledReferences++;
          if (suffix.includes('#')) anchoredReferences++;
        } else {
          expect(generated, sourceFile).toContain(`${link.label} (repository: \`${link.target}\`)`);
          repositoryReferences++;
        }
      }
    }
    expect(repositoryReferences).toBeGreaterThan(0);
    expect(bundledReferences).toBeGreaterThan(0);
    expect(anchoredReferences).toBeGreaterThan(0);
    expect(codeBlocks).toBeGreaterThan(0);
    expect(readFileSync(new URL('references/usage.md', output)).toString()).toContain(
      '[Call the HTTP API safely](docs/tasks/call-api.md)',
    );
    expect(readFileSync(new URL('references/docs/tasks/call-api.md', output)).toString()).toContain(
      '[OpenAPI](../../openapi.yaml)',
    );
    expect(
      readFileSync(new URL('references/docs/reference/http-conventions.md', output)).toString(),
    ).toContain('[calling the API](../tasks/call-api.md#handle-failures)');
    expect(
      readFileSync(new URL('references/docs/tasks/read-logs.md', output)).toString(),
    ).toContain('archive limits (repository: `../execution-logs.md#installation-and-capacity`)');
  });

  it('ships manual examples with explicit execution policy version 1', () => {
    for (const [name, kind] of [
      ['assets/manual-job.yaml', 'ScheduledJob'],
      ['assets/manual-workflow.yaml', 'WorkflowJob'],
    ]) {
      const document = parse(entries.get(`orbitjob-workloads/${name}`)!.toString());
      expect(document, name).toMatchObject({
        apiVersion: 'workloads.orbitjob.io/v1alpha1',
        kind,
        spec: { executionPolicyVersion: 1, schedule: '' },
      });
    }
  });

  it('reproduces the package from the current source files', () => {
    const result = execFileSync(execPath, ['scripts/generate-workload-assistant.mjs', '--check'], {
      cwd: fileURLToPath(new URL('../', import.meta.url)),
      encoding: 'utf8',
    });
    expect(result).toContain('Workload assistant checked: 22 files; CRD examples validated');
  });
});
