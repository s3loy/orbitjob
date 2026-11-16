import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const image = process.argv[2];
if (!image) throw new Error('Usage: npm run image:check -- <admin-image>');
const directory = mkdtempSync(join(tmpdir(), 'orbitjob-console-image-'));
let container;
try {
  container = execFileSync('docker', ['create', image], { encoding: 'utf8' }).trim();
  execFileSync('docker', ['cp', `${container}:/console`, directory], { stdio: 'pipe' });
  const root = join(directory, 'console');
  const entry = readFileSync(join(root, 'index.html'), 'utf8');
  const references = [...entry.matchAll(/(?:src|href)="([^"]+)"/g)].map((match) => match[1]);
  if (!references.some((path) => path.endsWith('.js'))) throw new Error('Entry has no script');
  for (const reference of references) {
    if (!reference.startsWith('/console/') || reference.includes('..'))
      throw new Error(`Unexpected external or unsafe asset: ${reference}`);
    if (!statSync(join(root, reference.slice('/console/'.length))).size)
      throw new Error(`Empty asset: ${reference}`);
  }
  const assets = readdirSync(join(root, 'assets'));
  for (const route of [
    'Jobs-',
    'Runs-',
    'Connection-',
    'Monitoring-',
    'Workflows-',
    'Functions-',
    'Administration-',
    'ManifestBuilder-',
    'DefinitionEditor-',
  ]) {
    if (!assets.some((name) => name.startsWith(route) && name.endsWith('.js')))
      throw new Error(`Missing lazy route bundle: ${route}`);
  }
  console.log(
    `Console image check passed: ${image}; ${references.length} entry references, ${assets.length} assets`,
  );
} finally {
  if (container) execFileSync('docker', ['rm', container], { stdio: 'pipe' });
  rmSync(directory, { recursive: true, force: true });
}
