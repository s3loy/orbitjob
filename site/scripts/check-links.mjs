import { readFile, readdir, stat } from 'node:fs/promises';
import path from 'node:path';
import { siteOrigin } from './site-config.mjs';

async function htmlFiles(directory) {
  return (await Promise.all((await readdir(directory, { withFileTypes: true })).map(entry => {
    const file = path.join(directory, entry.name);
    return entry.isDirectory() ? htmlFiles(file) : file.endsWith('.html') ? [file] : [];
  }))).flat();
}

export async function checkArtifactLinks({ dist, repo, base = '/orbitjob', origin = siteOrigin, files }) {
  dist = path.resolve(dist);
  repo = path.resolve(repo);
  base = base.replace(/\/$/, '');
  const expectedOrigin = new URL(origin).origin;
  const failures = new Set();
  const cache = new Map();
  const html = async file => {
    if (!cache.has(file)) cache.set(file, await readFile(file, 'utf8'));
    return cache.get(file);
  };
  for (const file of files || await htmlFiles(dist)) {
    const name = path.relative(dist, file).split(path.sep).join('/');
    // 404.html is a file route, unlike a page emitted as <route>/index.html.
    const current = new URL(`${base}/${name.replace(/(^|\/)index\.html$/, '$1')}`, expectedOrigin);
    for (const match of (await html(file)).matchAll(/(?:href|src)="([^"]+)"/g)) {
      const target = match[1].replaceAll('&amp;', '&');
      if (!target || /^(?:mailto:|tel:|data:|blob:|javascript:)/i.test(target)) continue;
      const source = target.match(/^https:\/\/github.com\/s3loy\/orbitjob\/blob\/(?:dev|main)\/([^#?]+)(?:#([^?]+))?$/);
      if (source) {
        try {
          const local = path.resolve(repo, decodeURIComponent(source[1]));
          if (!local.startsWith(`${repo}${path.sep}`)) failures.add(`Unsafe repository target: ${target}`);
          else await stat(local);
        } catch { failures.add(`Missing repository target: ${target}`); }
        continue;
      }
      let url;
      try { url = new URL(target, current); }
      catch { failures.add(`${name} has an invalid URL: ${target}`); continue; }
      if (!['http:', 'https:'].includes(url.protocol) || url.origin !== expectedOrigin) continue;
      if (base && url.pathname !== base && !url.pathname.startsWith(`${base}/`)) {
        failures.add(`Escapes base: ${target}`);
        continue;
      }
      let local;
      try {
        const relative = decodeURIComponent(url.pathname.slice(base.length)).replace(/^\//, '');
        local = path.resolve(dist, relative);
      } catch { failures.add(`Invalid target encoding: ${target}`); continue; }
      if (!local.startsWith(`${dist}${path.sep}`) && local !== dist) {
        failures.add(`Unsafe target: ${target}`);
        continue;
      }
      try {
        if ((await stat(local)).isDirectory()) local = path.join(local, 'index.html');
        await stat(local);
        if (url.hash && local.endsWith('.html') && !url.pathname.startsWith(`${base}/api/`) && !url.pathname.startsWith(`${base}/zh-cn/api/`)) {
          const id = decodeURIComponent(url.hash.slice(1));
          if (!(await html(local)).includes(`id="${id}"`)) failures.add(`${name} has no anchor: ${target}`);
        }
      } catch { failures.add(`${name} has no target: ${target}`); }
    }
  }
  return [...failures];
}
