import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { pathToFileURL } from 'node:url';

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'orbitjob-benchmark-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

test('benchmark history supports legacy ls-tree and uses its own revision after another fetch', async t => {
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
  git(remote, 'add', '--chmod=+x', '--', 'dev/bench/index.html');
  git(remote, 'commit', '-qm', 'benchmark branch');
  git(root, 'clone', '-q', '--branch', 'dev', '--depth', '1', pathToFileURL(remote).href, checkout);
  git(checkout, 'fetch', '-q', '--depth=1', 'origin', 'refs/heads/dev');

  const scripts = path.join(checkout, 'site/scripts');
  await mkdir(scripts, { recursive: true });
  await copyFile(new URL('./fetch-benchmark-history.sh', import.meta.url), path.join(scripts, 'fetch-benchmark-history.sh'));
  await mkdir(shimDir);
  await writeFile(path.join(shimDir, 'git'), `#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == ls-tree ]]; then
  for option in "$@"; do
    case $option in
      --format|--format=*) echo "error: unknown option 'format'" >&2; exit 129 ;;
    esac
  done
fi
PATH="$FIXTURE_GIT_PATH" git "$@"
if [[ $1 == fetch && $* == *refs/heads/gh-pages* ]]; then
  test "$(PATH="$FIXTURE_GIT_PATH" git rev-parse --verify FETCH_HEAD)" = "$FIXTURE_DEV_REVISION"
  PATH="$FIXTURE_GIT_PATH" git fetch --depth=1 --no-tags origin refs/heads/dev >&2
fi
`, { mode: 0o700 });

  const fetchHistory = destination => spawnSync('bash', [path.join(scripts, 'fetch-benchmark-history.sh'), destination], {
    env: { ...env, PATH: `${shimDir}${path.delimiter}${process.env.PATH}`, FIXTURE_GIT_PATH: process.env.PATH, FIXTURE_DEV_REVISION: dev },
    encoding: 'utf8',
  });
  const fetched = fetchHistory(path.join(root, 'archive'));
  assert.equal(fetched.status, 0, fetched.stderr);
  const archive = fetched.stdout.trim();
  assert.equal(await readFile(path.join(archive, 'index.html'), 'utf8'), 'benchmark fixture\n');
  assert.equal(await readFile(path.join(archive, 'data.js'), 'utf8'), 'benchmark data\n');
  assert.equal(git(checkout, 'rev-parse', 'FETCH_HEAD'), dev);
  assert.equal(git(checkout, 'for-each-ref', 'refs/orbitjob/benchmark-history/'), '');

  await writeFile(path.join(remote, 'dev/bench/local-draft.txt'), 'private draft fixture\n');
  git(remote, 'add', '--', 'dev/bench/local-draft.txt');
  git(remote, 'commit', '-qm', 'unexpected archive member');
  async function assertRejected(name) {
    const destination = path.join(root, name);
    const rejected = fetchHistory(destination);
    assert.equal(rejected.status, 1, rejected.stderr);
    assert.match(rejected.stderr, /Benchmark history must contain only regular files/);
    await assert.rejects(readdir(destination), { code: 'ENOENT' });
    assert.equal(git(checkout, 'for-each-ref', 'refs/orbitjob/benchmark-history/'), '');
  }
  await assertRejected('extra-file-archive');

  git(remote, 'rm', '--', 'dev/bench/local-draft.txt', 'dev/bench/data.js');
  await symlink('index.html', data);
  git(remote, 'add', '--', 'dev/bench/data.js');
  git(remote, 'commit', '-qm', 'unexpected archive symlink');
  await assertRejected('symlink-archive');
});

test('site assembly rejects extra files and non-regular benchmark entries', async t => {
  const root = await fixture(t);
  const source = path.join(root, 'source');
  const dist = path.join(root, 'dist');
  const scripts = path.join(root, 'scripts');
  const target = path.join(dist, 'dev/bench');
  for (const directory of [source, dist, scripts]) await mkdir(directory);
  const assemble = path.join(scripts, 'assemble.mjs');
  await copyFile(new URL('./assemble.mjs', import.meta.url), assemble);
  await writeFile(path.join(dist, 'index.html'), 'site fixture\n');
  await writeFile(path.join(source, 'index.html'), 'benchmark fixture\n');
  await writeFile(path.join(source, 'data.js'), 'benchmark data\n');
  const build = () => spawnSync(process.execPath, [assemble], {
    env: { ...process.env, BENCHMARK_ARCHIVE: source },
    encoding: 'utf8',
  });

  await writeFile(path.join(source, 'local-draft.txt'), 'private draft fixture\n');
  const extra = build();
  assert.equal(extra.status, 1, extra.stderr);
  assert.match(extra.stderr, /Benchmark archive must contain only regular files/);
  assert.match(extra.stderr, /local-draft\.txt/);
  await assert.rejects(readdir(target), { code: 'ENOENT' });
  await rm(path.join(source, 'local-draft.txt'));

  await rm(path.join(source, 'data.js'));
  await symlink('index.html', path.join(source, 'data.js'));
  const symlinked = build();
  assert.equal(symlinked.status, 1, symlinked.stderr);
  assert.match(symlinked.stderr, /data\.js \(not a regular file\)/);
  await rm(path.join(source, 'data.js'));
  await writeFile(path.join(source, 'data.js'), 'benchmark data\n');

  const clean = build();
  assert.equal(clean.status, 0, clean.stderr);
  assert.deepEqual((await readdir(target)).sort(), ['data.js', 'index.html']);
  assert.equal(await readFile(path.join(target, 'data.js'), 'utf8'), 'benchmark data\n');
  assert.equal(await readFile(path.join(target, 'index.html'), 'utf8'), 'benchmark fixture\n');
});
