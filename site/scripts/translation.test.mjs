import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { alignHeadingIDs, markdownParts, sha256, translatedHeadingIDs, validateTopicTranslation, validateTranslationBaseline } from './translation.mjs';

test('translation validation protects nested fences, literals, links and explicit anchors', () => {
  const source = '# Guide\n\n## Run `job_id`\nUse `job:Trigger` [API](../api.md#request).\n<a id="explicit"></a>\n\n````md\n```bash\ncurl "$ORIGIN"\n```\n````';
  const zh = source.replace('# Guide', '# 指南').replace('## Run', '## 执行').replace('Use ', '使用 ').replace('[API]', '[接口]');
  assert.equal(validateTopicTranslation(source, zh).fences, 1);
  assert.throws(() => validateTopicTranslation(source, zh.replace('$ORIGIN', '$OTHER')), /blocks/);
  assert.throws(() => validateTopicTranslation(source, zh.replace('job:Trigger', 'job:Get')), /literals/);
  assert.throws(() => validateTopicTranslation(source, zh.replace('../api.md', '../other.md')), /targets/);
  assert.equal(markdownParts(source).headings.length, 2);
});

test('review baseline rejects changed prose and contract inputs even when examples still match', async () => {
  const repo = await mkdtemp(path.join(tmpdir(), 'orbitjob-translation-'));
  const english = '# Guide\n\nRead permission is required.\n';
  const chinese = '# 指南\n\n需要读取权限。\n';
  const entries = [{ id: 'guide', source: 'guide.md' }];
  const inputs = { 'guide.md': english, 'site/i18n/zh-CN/topics/guide.md': chinese, 'api/openapi.yaml': 'paths: {}\n', 'site/i18n/zh-CN/api/current.json': '{}\n' };
  try {
    for (const [file, text] of Object.entries(inputs)) {
      await mkdir(path.dirname(path.join(repo, file)), { recursive: true });
      await writeFile(path.join(repo, file), text);
    }
    const baseline = { topics: { guide: { source: 'guide.md', englishSha256: sha256(english), chineseSha256: sha256(chinese) } }, references: Object.fromEntries(Object.entries(inputs).filter(([file]) => file.startsWith('api/') || file.endsWith('current.json')).map(([file, text]) => [file, sha256(text)])) };
    await writeFile(path.join(repo, 'site/i18n/zh-CN/reviewed.json'), JSON.stringify(baseline));
    await validateTranslationBaseline(repo, entries);
    await writeFile(path.join(repo, 'guide.md'), english.replace('Read', 'Write'));
    await assert.rejects(validateTranslationBaseline(repo, entries), /English changed/);
    await writeFile(path.join(repo, 'guide.md'), english);
    await writeFile(path.join(repo, 'site/i18n/zh-CN/topics/guide.md'), chinese.replace('读取', '写入'));
    await assert.rejects(validateTranslationBaseline(repo, entries), /Chinese changed/);
    await writeFile(path.join(repo, 'site/i18n/zh-CN/topics/guide.md'), chinese);
    await writeFile(path.join(repo, 'api/openapi.yaml'), 'paths: {changed: {}}\n');
    await assert.rejects(validateTranslationBaseline(repo, entries), /API input changed/);
  } finally { await rm(repo, { recursive: true, force: true }); }
});

test('translated labels retain duplicate English heading identities and literal fenced headings', () => {
  const en = '## Request\n\n~~~md\n## Literal\n~~~\n\n## Request';
  assert.equal(alignHeadingIDs(en, en.replaceAll('Request', '请求')), '## 请求 {#request}\n\n~~~md\n## Literal\n~~~\n\n## 请求 {#request-1}');
  const heading = { type: 'element', tagName: 'h2', children: [{ type: 'text', value: '请求 {#request}' }], properties: {} };
  translatedHeadingIDs().element.visit(heading, {
    replaceNode(node, replacement) { heading.children[heading.children.indexOf(node)] = replacement; },
    setProperty(node, key, value) { node.properties[key] = value; },
  });
  assert.equal(heading.properties.id, 'request');
  assert.equal(heading.children[0].value, '请求');
});
