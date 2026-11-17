import assert from 'node:assert/strict';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { checkArtifactLinks } from './check-links.mjs';

async function fixture(t, pages) {
  const repo = await mkdtemp(path.join(os.tmpdir(), 'orbitjob-site-links-'));
  const dist = path.join(repo, 'dist');
  t.after(() => rm(repo, { recursive: true, force: true }));
  for (const [name, content] of Object.entries(pages)) {
    const file = path.join(dist, name);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, content);
  }
  return { repo, dist };
}

test('missing same-site absolute links fail independently of page metadata', async t => {
  const location = await fixture(t, {
    'index.html': '<a href="https://s3loy.github.io/orbitjob/missing/?q=1#section">Missing</a>',
  });
  assert.deepEqual(await checkArtifactLinks(location), [
    'index.html has no target: https://s3loy.github.io/orbitjob/missing/?q=1#section',
  ]);
});

for (const base of ['/orbitjob', '/review', '/']) {
  test(`local and same-site links resolve files, queries and anchors under ${base}`, async t => {
    const prefix = base.replace(/\/$/, '');
    const location = await fixture(t, {
      'index.html': `<a href="https://s3loy.github.io${prefix}/topic/?q=1&amp;more=2#part%20one">Topic</a><a href="//s3loy.github.io${prefix}/404.html?missing=1#recovery">404 artifact</a>`,
      'topic/index.html': '<h1 id="part one">Topic</h1><a href="../?q=1">Home</a>',
      '404.html': `<main id="recovery"><a href="${prefix}/">Home</a><a href="#recovery">Recovery</a></main>`,
    });
    assert.deepEqual(await checkArtifactLinks({ ...location, base }), []);
  });
}

test('404.html does not make a nonexistent /404/ route or anchor valid', async t => {
  const location = await fixture(t, {
    'index.html': '<a href="https://s3loy.github.io/orbitjob/404/">Missing route</a><a href="https://s3loy.github.io/orbitjob/404.html#missing">Missing anchor</a>',
    '404.html': '<h1 id="recovery">Recovery</h1>',
  });
  assert.deepEqual(await checkArtifactLinks(location), [
    'index.html has no target: https://s3loy.github.io/orbitjob/404/',
    'index.html has no anchor: https://s3loy.github.io/orbitjob/404.html#missing',
  ]);
});

test('external URLs stay external and repository sources still require a local file', async t => {
  const location = await fixture(t, {
    'index.html': '<a href="https://example.com/missing/">External</a><img src="//cdn.example.com/image.png"><a href="https://s3loy.github.io.evil.invalid/orbitjob/missing/">Other origin</a><a href="https://github.com/s3loy/orbitjob/blob/dev/README.md#unpublished-anchor">Source</a><a href="https://github.com/s3loy/orbitjob/blob/main/missing.md">Missing source</a>',
  });
  await writeFile(path.join(location.repo, 'README.md'), '# README');
  assert.deepEqual(await checkArtifactLinks(location), [
    'Missing repository target: https://github.com/s3loy/orbitjob/blob/main/missing.md',
  ]);
});

test('same-site paths cannot escape the configured base or artifact', async t => {
  const location = await fixture(t, {
    'index.html': '<a href="https://s3loy.github.io/elsewhere/">Other project</a><a href="/orbitjob/%2F..%2Fsecret">Unsafe</a><a href="/orbitjob/%ZZ">Malformed encoding</a>',
  });
  assert.deepEqual(await checkArtifactLinks(location), [
    'Escapes base: https://s3loy.github.io/elsewhere/',
    'Unsafe target: /orbitjob/%2F..%2Fsecret',
    'Invalid target encoding: /orbitjob/%ZZ',
  ]);
});

test('historical benchmark HTML keeps link checks without requiring current metadata', async t => {
  const location = await fixture(t, {
    'index.html': '<a href="/orbitjob/dev/bench/">Archive</a>',
    'dev/bench/index.html': '<html><a href="./result.html#measurement">Result</a></html>',
    'dev/bench/result.html': '<html><p id="measurement">Historical measurement</p><a href="https://example.com/original">Original</a></html>',
  });
  assert.deepEqual(await checkArtifactLinks(location), []);
});
