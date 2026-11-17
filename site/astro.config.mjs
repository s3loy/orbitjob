import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { satteri } from '@astrojs/markdown-satteri';
import { groups, guides, chineseGroups } from './scripts/guide-sources.mjs';
import { translatedHeadingIDs } from './scripts/translation.mjs';
import { siteOrigin } from './scripts/site-config.mjs';

export const base = process.env.SITE_BASE || '/orbitjob';
export default defineConfig({
  site: siteOrigin,
  base,
  trailingSlash: 'always',
  // The API explorer's CSP requires bundled scripts to remain external.
  vite: { build: { assetsInlineLimit: 0 } },
  markdown: { processor: satteri({ hastPlugins: [translatedHeadingIDs] }) },
  integrations: [starlight({
    // Static hosts use one root 404.html for missing routes in both languages.
    disable404Route: true,
    title: 'OrbitJob',
    defaultLocale: 'root',
    locales: {
      root: { label: 'English', lang: 'en' },
      'zh-cn': { label: '简体中文', lang: 'zh-CN' },
    },
    description: 'Install, use and operate the OrbitJob Kubernetes execution ledger.',
    logo: { src: './public/favicon.svg', replacesTitle: false },
    social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/s3loy/orbitjob' }],
    customCss: ['./src/styles/docs.css', '@fontsource/geist/latin-400.css', '@fontsource/geist/latin-500.css'],
    components: { Search: './src/components/Search.astro', PageTitle: './src/components/PageTitle.astro' },
    sidebar: [
      { label: 'Documentation', translations: { 'zh-CN': '文档首页' }, link: '/' },
      ...groups.map(group => ({
        label: group.title,
        translations: { 'zh-CN': chineseGroups[group.id][0] },
        collapsed: group.id !== 'getting-started',
        items: [
          { label: group.title, translations: { 'zh-CN': chineseGroups[group.id][0] }, slug: `docs/${group.id}` },
          ...guides.filter(guide => guide.group === group.id && guide.id !== group.id).map(guide => ({ slug: `docs/${guide.id}` })),
          ...(group.id === 'reference' ? [{ label: 'Helm values', translations: { 'zh-CN': 'Helm 配置值' }, slug: 'docs/reference/helm-values' }, ...['scheduledjobs', 'jobruns', 'workflowjobs', 'workflowruns'].map(resource => ({ slug: `docs/reference/kubernetes/${resource}` })), { label: 'HTTP API', translations: { 'zh-CN': 'HTTP API' }, slug: 'docs/reference/http' }, { label: 'API explorer', translations: { 'zh-CN': 'API 浏览器' }, link: '/api/' }] : []),
        ],
      })),
    ],
  })],
});
