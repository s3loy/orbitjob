import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'orbitjob-benchmark-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

test('benchmark history preserves FETCH_HEAD and uses its own revision after another fetch', async t => {
  const root = await fixture(t);
  const remote = path.join(root, 'remote');
  const checkout = path.join(root, 'checkout');
  const shimDir = path.join(root, 'bin');
  const env = {
    ...Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_'))),
    GIT_CONFIG_GLOBAL: os.devNull,
    GIT_CONFIG_NOSYSTEM: '1',
    GIT_AUTHOR_NAME: 'OrbitJob Test',
    GIT_AUTHOR_EMAIL: 'orbitjob@example.invalid',
    GIT_COMMITTER_NAME: 'OrbitJob Test',
    GIT_COMMITTER_EMAIL: 'orbitjob@example.invalid',
  };
  const git = (cwd, ...args) => execFileSync('git', args, { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  await mkdir(path.join(remote, 'dev/bench'), { recursive: true });
  const index = path.join(remote, 'dev/bench/index.html');
  const data = path.join(remote, 'dev/bench/data.js');
  git(remote, 'init', '-q', '-b', 'dev');
  await writeFile(index, 'dev fixture\n');
  await writeFile(data, 'benchmark data\n');
  git(remote, 'add', '--', 'dev/bench');
  git(remote, 'commit', '-qm', 'development branch');
  const dev = git(remote, 'rev-parse', 'HEAD');
  git(remote, 'switch', '-qc', 'gh-pages');
  await writeFile(index, 'benchmark fixture\n');
  git(remote, 'commit', '-qam', 'benchmark branch');
  git(root, 'clone', '-q', '--branch', 'dev', '--depth', '1', pathToFileURL(remote).href, checkout);
  git(checkout, 'fetch', '-q', '--depth=1', 'origin', 'refs/heads/dev');

  const scripts = path.join(checkout, 'site/scripts');
  await mkdir(scripts, { recursive: true });
  await copyFile(new URL('./fetch-benchmark-history.sh', import.meta.url), path.join(scripts, 'fetch-benchmark-history.sh'));
  await mkdir(shimDir);
  await writeFile(path.join(shimDir, 'git'), `#!/usr/bin/env bash
set -euo pipefail
PATH="$FIXTURE_GIT_PATH" git "$@"
if [[ $1 == fetch && $* == *refs/heads/gh-pages* ]]; then
  test "$(PATH="$FIXTURE_GIT_PATH" git rev-parse --verify FETCH_HEAD)" = "$FIXTURE_DEV_REVISION"
  PATH="$FIXTURE_GIT_PATH" git fetch --depth=1 --no-tags origin refs/heads/dev >&2
fi
`, { mode: 0o700 });

  const archive = execFileSync('bash', [path.join(scripts, 'fetch-benchmark-history.sh'), path.join(root, 'archive')], {
    env: { ...env, PATH: `${shimDir}${path.delimiter}${process.env.PATH}`, FIXTURE_GIT_PATH: process.env.PATH, FIXTURE_DEV_REVISION: dev },
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
  assert.equal(await readFile(path.join(archive, 'index.html'), 'utf8'), 'benchmark fixture\n');
  assert.equal(await readFile(path.join(archive, 'data.js'), 'utf8'), 'benchmark data\n');
  assert.equal(git(checkout, 'rev-parse', 'FETCH_HEAD'), dev);
  assert.equal(git(checkout, 'for-each-ref', 'refs/orbitjob/benchmark-history/'), '');
});
