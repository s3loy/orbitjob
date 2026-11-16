import assert from 'node:assert/strict';
import { readFile, readdir, stat } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'yaml';
import { guides } from './guide-sources.mjs';
import { translationFile, validateTranslationBaseline, markdownParts } from './translation.mjs';
import { loadChineseAPI, translateOpenAPI } from './localize.mjs';

const site = fileURLToPath(new URL('../', import.meta.url));
const repo = path.resolve(site, '..');
const dist = path.join(site, 'dist');
const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
async function files(directory) {
  return (await Promise.all((await readdir(directory, { withFileTypes: true })).map(entry => entry.isDirectory() ? files(path.join(directory, entry.name)) : path.join(directory, entry.name)))).flat();
}
const output = await files(dist);
assert(output.includes(path.join(dist, 'index.html')), 'Documentation entry is missing');
assert(!output.some(file => file.startsWith(path.join(dist, 'demo/'))), 'The documentation artifact must not contain a Console demo');
await validateTranslationBaseline(repo);
assert(output.includes(path.join(dist, '.nojekyll')), 'Pages asset marker is missing');
const authoritative = parse(await readFile(path.join(repo, 'api/openapi.yaml'), 'utf8'));
assert.deepEqual(JSON.parse(await readFile(path.join(dist, 'openapi.json'), 'utf8')), authoritative, 'Published OpenAPI differs from the contract');
const methods = new Set(['get', 'post', 'put', 'patch', 'delete', 'head', 'options']);
const expected = Object.entries(authoritative.paths).flatMap(([url,item]) => Object.entries(item).filter(([method]) => methods.has(method)).map(([method,op]) => `${method} ${url} ${op.operationId}`)).sort();
const chinese = JSON.parse(await readFile(path.join(dist, 'zh-cn/openapi.json'), 'utf8'));
assert.deepEqual(chinese, translateOpenAPI(authoritative, await loadChineseAPI(path.join(site, 'i18n/zh-CN/api'))), 'Localized OpenAPI is stale');
// Ignore prose only. Executable examples/defaults/enums and tag identities are contract data.
function wireContract(value, key = '') {
  if (!value || typeof value !== 'object' || ['example', 'examples', 'default', 'enum'].includes(key)) return value;
  if (Array.isArray(value)) return value.map(item => wireContract(item));
  return Object.fromEntries(Object.entries(value).filter(([name, item]) => name !== 'x-displayName' && !(typeof item === 'string' && ['title', 'summary', 'description'].includes(name))).map(([name, item]) => [name, wireContract(item, name)]));
}
const sourceWire = wireContract(authoritative), chineseWire = wireContract(chinese);
delete sourceWire.tags; delete chineseWire.tags;
assert.deepEqual(chineseWire, sourceWire, 'Translation changed the HTTP wire contract');
assert.deepEqual(chinese.tags.map(tag => tag.name).sort(), [...new Set(Object.values(authoritative.paths).flatMap(item => Object.values(item).flatMap(op => op.tags || [])))].sort(), 'Translation changed tag identities');
const headingIDs = content => [...content.matchAll(/<h[1-6][^>]* id="([^"]+)"/g)].map(match => match[1]);
for (const locale of ['', 'zh-cn']) {
  const prefix = `${base}${locale ? '/zh-cn' : ''}`;
  const catalog = JSON.parse(await readFile(path.join(site, `src/generated/catalog${locale ? '.zh-cn' : ''}.json`), 'utf8'));
  assert.deepEqual(catalog.operations.map(op => `${op.method} ${op.path} ${op.id}`).sort(), expected, `Incomplete ${locale || 'English'} operation catalog`);
  for (const guide of guides) assert(catalog.pages.some(page => page.source === (locale ? translationFile(guide.id) : guide.source) && page.route === `${prefix}/docs/${guide.id}/`), `Unpublished ${locale} topic: ${guide.source}`);
  for (const op of catalog.operations) {
    const file = path.join(dist, locale, 'docs/reference/http/operations', op.id.toLowerCase(), 'index.html');
    assert(output.includes(file), `Operation missing: ${locale} ${op.id}`);
    const content = await readFile(file, 'utf8');
    assert(content.includes(locale ? '响应' : 'Responses') && content.includes(locale ? '请求' : 'Request'), `Contract sections missing: ${locale} ${op.id}`);
    if (op.path.startsWith('/api/v1/auth/')) {
      assert(!content.includes('Authorization: Bearer $ORBITJOB_API_KEY'), `Account endpoint has a bearer-key example: ${op.id}`);
      assert(content.includes('ORBITJOB_ORIGIN'), `Account origin missing: ${op.id}`);
      if (op.authenticated && !['get', 'head', 'options'].includes(op.method)) assert(content.includes('X-CSRF-Token: $ORBITJOB_CSRF_TOKEN'), `Account write CSRF missing: ${op.id}`);
    }
    if (locale) {
      const relative = `reference/http/operations/${op.id.toLowerCase()}.md`;
      const en = markdownParts(await readFile(path.join(site, 'src/content/docs/generated', relative), 'utf8'));
      const zh = markdownParts(await readFile(path.join(site, 'src/content/docs/zh-cn/generated', relative), 'utf8'));
      assert.deepEqual(zh.fences.filter(block => block.startsWith('```bash')), en.fences.filter(block => block.startsWith('```bash')), `Localized request command differs: ${op.id}`);
    }
  }
  for (const name of Object.keys(authoritative.components?.schemas || {})) assert(output.includes(path.join(dist, locale, 'docs/reference/http/schemas', name.toLowerCase(), 'index.html')), `Schema missing: ${locale} ${name}`);
  for (const resource of ['scheduledjobs', 'jobruns', 'workflowjobs', 'workflowruns']) assert(output.includes(path.join(dist, locale, 'docs/reference/kubernetes', resource, 'index.html')), `CRD reference missing: ${locale} ${resource}`);
  assert(output.includes(path.join(dist, locale, 'docs/reference/helm-values/index.html')), `Helm reference missing: ${locale}`);
  const explorer = await readFile(path.join(dist, locale, 'api/index.html'), 'utf8');
  assert(/connect-src 'self'/.test(explorer.replaceAll('&#39;', "'")), 'API explorer must prohibit external connections');
  for (const script of explorer.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
    if (!script[1].includes('application/json') && script[2].trim()) assert(/\bsrc=/.test(script[1]), 'API explorer has an executable inline script blocked by its CSP');
  }
  if (locale) for (const page of catalog.pages) {
    const relative = page.route.slice(`${prefix}/`.length);
    const en = await readFile(path.join(dist, relative, 'index.html'), 'utf8');
    const zh = await readFile(path.join(dist, locale, relative, 'index.html'), 'utf8');
    assert.deepEqual(headingIDs(zh), headingIDs(en), `Language switch loses heading identity: ${relative}`);
  }
}
const failures = new Set();
const cache = new Map();
let currentPages = 0;
let historicalPages = 0;
async function html(file) { if (!cache.has(file)) cache.set(file,await readFile(file,'utf8')); return cache.get(file); }
for (const file of output.filter(file => file.endsWith('.html'))) {
  const current = new URL(`${base}/${path.relative(dist,file).split(path.sep).join('/').replace(/index\.html$/,'')}`, 'https://site.invalid');
  const content = await html(file);
  const historical = path.relative(dist, file).split(path.sep).join('/').startsWith('dev/bench/');
  if (historical) historicalPages++; else currentPages++;
  if (!historical) {
    const chinesePage = path.relative(dist, file).split(path.sep).join('/').startsWith('zh-cn/');
    assert(new RegExp(`<html[^>]*lang="${chinesePage ? 'zh-CN' : 'en'}"`).test(content), `Unexpected page language: ${file}`);
  }
  for (const match of content.matchAll(/(?:href|src)="([^"]+)"/g)) {
    const target = match[1].replaceAll('&amp;', '&');
    if (!target || /^(?:mailto:|tel:|data:|blob:|javascript:)/i.test(target)) continue;
    if (/^https?:/i.test(target)) {
      const source = target.match(/^https:\/\/github.com\/s3loy\/orbitjob\/blob\/(?:dev|main)\/([^#?]+)(?:#([^?]+))?$/);
      if (source) {
        const local = path.resolve(repo,decodeURIComponent(source[1]));
        if (!local.startsWith(`${repo}${path.sep}`)) failures.add(`Unsafe repository target: ${target}`);
        else try { await stat(local); } catch { failures.add(`Missing repository target: ${target}`); }
      }
      continue;
    }
    const url = new URL(target,current);
    if (base && url.pathname !== base && !url.pathname.startsWith(`${base}/`)) { failures.add(`Escapes base: ${target}`); continue; }
    const relative = decodeURIComponent(url.pathname.slice(base.length)).replace(/^\//,'');
    let local = path.resolve(dist,relative);
    if (!local.startsWith(`${dist}${path.sep}`) && local !== dist) { failures.add(`Unsafe target: ${target}`); continue; }
    try {
      if ((await stat(local)).isDirectory()) local = path.join(local,'index.html');
      await stat(local);
      if (url.hash && local.endsWith('.html') && !url.pathname.startsWith(`${base}/api/`) && !url.pathname.startsWith(`${base}/zh-cn/api/`)) {
        const id = decodeURIComponent(url.hash.slice(1));
        if (!(await html(local)).includes(`id="${id}"`)) failures.add(`${path.relative(dist,file)} has no anchor: ${target}`);
      }
    } catch { failures.add(`${path.relative(dist,file)} has no target: ${target}`); }
  }
}
assert.equal(failures.size,0,[...failures].join('\n'));
console.log(`Verified ${currentPages} English and Chinese pages and ${historicalPages} historical pages, ${guides.length} topics per language, ${expected.length} operations, schemas and local/repository links`);
