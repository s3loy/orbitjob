import assert from 'node:assert/strict';
import { access, copyFile, mkdir, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const site = fileURLToPath(new URL('../', import.meta.url));
await access(path.join(site, 'dist/index.html'));
// GitHub Pages must serve underscored Astro assets without Jekyll filtering.
await writeFile(path.join(site, 'dist/.nojekyll'), '');
// CI may supply an existing benchmark archive; this never writes to its branch.
if (process.env.BENCHMARK_ARCHIVE) {
  const source = path.resolve(process.env.BENCHMARK_ARCHIVE);
  const archiveFiles = ['data.js', 'index.html'];
  const entries = await readdir(source, { withFileTypes: true });
  assert.deepEqual(
    entries.map(entry => entry.isFile() ? entry.name : `${entry.name} (not a regular file)`).sort(),
    archiveFiles,
    'Benchmark archive must contain only regular files data.js and index.html',
  );
  const target = path.join(site, 'dist/dev/bench');
  await mkdir(path.dirname(target), { recursive: true });
  await mkdir(target);
  for (const name of archiveFiles) await copyFile(path.join(source, name), path.join(target, name));
  console.log('Included the existing benchmark archive at dev/bench/');
}
console.log('Assembled the documentation site; no Console bundle is built or copied');
