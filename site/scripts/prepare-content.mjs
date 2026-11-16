import { readFile, writeFile, mkdir, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';
import { parse } from 'yaml';
import { guides, groups, chineseGroups, validateGuides, rewriteLinks } from './guide-sources.mjs';
import { generateAPI } from './api-reference.mjs';
import { generateConfiguration } from './config-reference.mjs';
import { loadChineseAPI, translateOpenAPI } from './localize.mjs';
import { translationFile, validateTranslationBaseline, alignHeadingIDs } from './translation.mjs';

const site = fileURLToPath(new URL('../', import.meta.url));
const repo = path.resolve(site, '..');
const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
const sourceRef = 'dev';
validateGuides();
await validateTranslationBaseline(repo);
const spec = parse(await readFile(path.join(repo, 'api/openapi.yaml'), 'utf8'));
const chineseSpec = translateOpenAPI(spec, await loadChineseAPI(path.join(site, 'i18n/zh-CN/api')));
await mkdir(path.join(site, 'public/zh-cn'), { recursive: true });
await mkdir(path.join(site, 'src/generated'), { recursive: true });
await writeFile(path.join(site, 'public/openapi.json'), JSON.stringify(spec));
await writeFile(path.join(site, 'public/zh-cn/openapi.json'), JSON.stringify(chineseSpec));

for (const locale of ['', 'zh-cn']) {
  const zh = Boolean(locale);
  const prefix = `${base}${zh ? '/zh-cn' : ''}`;
  const output = path.join(site, `src/content/docs/${zh ? 'zh-cn/' : ''}generated`);
  await rm(output, { recursive: true, force: true });
  const routes = new Map(guides.map(({ source, id }) => [source, `${prefix}/docs/${id}/`]));
  routes.set('README.md', `${prefix}/`);
  routes.set('README.zh.md', `${base}/zh-cn/`);
  routes.set('USAGE.md', `${prefix}/docs/tasks/`);
  const banner = zh
    ? '当前源码的开发文档。请使用来自同一源码版本的组件镜像和 Chart；已发布制品可能与此不同。'
    : 'Development documentation for the current source. Use matching component images and chart files; released artifacts can differ.';
  const pages = [];
  async function write(id, title, description, body, source) {
    const file = path.join(output, `${id}.md`);
    await mkdir(path.dirname(file), { recursive: true });
    const metadata = { title, description, slug: `${zh ? 'zh-cn/' : ''}docs/${id.replace(/\/index$/, '')}`, banner: { content: banner }, ...(source ? { editUrl: `https://github.com/s3loy/orbitjob/blob/${sourceRef}/${source}` } : {}) };
    const yaml = Object.entries(metadata).map(([key, value]) => `${key}: ${JSON.stringify(value)}`).join('\n');
    await writeFile(file, `---\n${yaml}\n---\n\n${body.trim()}\n`);
    pages.push({ route: `${prefix}/docs/${id.replace(/\/index$/, '')}/`, title, source, locale: zh ? 'zh-cn' : 'en' });
  }
  for (const entry of guides) {
    const english = await readFile(path.join(repo, entry.source), 'utf8');
    const text = zh ? await readFile(path.join(repo, translationFile(entry.id)), 'utf8') : english;
    const heading = text.match(/^# ([^\n]+)\n/);
    assert(heading, `Content must start with one H1: ${entry.source}`);
    const body = text.slice(heading[0].length).trim();
    const englishBody = english.slice(english.indexOf('\n') + 1).trim();
    const summary = body.split(/\n\s*\n/).find(p => !/^(?:#|>|\||```)/.test(p)) || heading[1];
    const description = summary.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[`*\n]/g, ' ').replace(/\s+/g, ' ').slice(0, 190);
    const aligned = zh ? alignHeadingIDs(englishBody, body) : body;
    await write(entry.id, heading[1], description, rewriteLinks(aligned, entry.source, routes, base, sourceRef), zh ? translationFile(entry.id) : entry.source);
  }
  for (const group of groups) {
    if (guides.some(guide => guide.id === group.id)) continue;
    const [title, description] = zh ? chineseGroups[group.id] : [group.title, group.description];
    const topics = pages.filter(({ route }) => route.startsWith(`${prefix}/docs/${group.id}/`));
    let body = `${description}\n\n${topics.map(({ route, title }) => `- [${title}](${route})`).join('\n')}`;
    if (group.id === 'reference') body += zh
      ? `\n- [Helm 配置值](${prefix}/docs/reference/helm-values/)\n- [Kubernetes 资源 Schema](${prefix}/docs/reference/kubernetes/scheduledjobs/)\n- [HTTP API 参考](${prefix}/docs/reference/http/)\n- [API 浏览器](${prefix}/api/)\n- [下载原始 OpenAPI](${base}/openapi.json)`
      : `\n- [Helm values](${prefix}/docs/reference/helm-values/)\n- [Kubernetes resource schemas](${prefix}/docs/reference/kubernetes/scheduledjobs/)\n- [HTTP API reference](${prefix}/docs/reference/http/)\n- [API explorer](${prefix}/api/)\n- [Download OpenAPI](${base}/openapi.json)`;
    await write(`${group.id}/index`, title, description, body);
  }
  const displaySpec = zh ? chineseSpec : spec;
  const methods = new Set(['get', 'post', 'put', 'patch', 'delete', 'head', 'options']);
  const operations = Object.entries(displaySpec.paths).flatMap(([url, item]) => Object.entries(item).filter(([method]) => methods.has(method)).map(([method, op]) => ({ path: url, method, id: op.operationId, tag: op.tags?.[0] || 'Service' })));
  const apiPages = await generateAPI({ spec: displaySpec, operations, output, base: prefix, locale, englishOutput: path.join(site, 'src/content/docs/generated') });
  const configurationPages = await generateConfiguration({ repo, output, base: prefix, locale });
  const catalog = { operations, pages: [...pages, ...apiPages, ...configurationPages] };
  await writeFile(path.join(site, `src/generated/catalog${zh ? '.zh-cn' : ''}.json`), JSON.stringify(catalog, null, 2));
  if (zh) await writeFile(path.join(site, 'public/zh-cn/search-titles.json'), JSON.stringify(catalog.pages.map(({ route, title }) => ({ route, title }))));
}
console.log(`Prepared ${guides.length} complete topics per language, localized references and ${Object.keys(spec.paths).length} HTTP paths`);
