import assert from 'node:assert/strict';
import test from 'node:test';
import { translateOpenAPI, chineseReferenceText, remapChineseLinks } from './localize.mjs';

test('Chinese prose does not change example bodies, field names, enums or stable API tags', () => {
  const source = { info: { title: 'Title', description: 'Description' }, paths: { '/items/{id}': { post: {
    tags: ['Items'], operationId: 'createItem', summary: 'Create',
    requestBody: { content: { 'application/json': { schema: { type: 'object', properties: { description: { type: 'string', description: 'Field text', enum: ['Description'] } } }, example: { description: 'Description', title: 'Title' } } } },
  } } }, components: { schemas: {} } };
  const result = translateOpenAPI(source, { Title: '标题', Description: '说明', Items: '条目', Create: '创建', 'Field text': '字段说明' });
  assert.equal(result.info.title, '标题');
  const op = result.paths['/items/{id}'].post;
  assert.equal(op.operationId, 'createItem');
  assert.deepEqual(op.tags, ['Items']);
  assert.equal(result.tags[0]['x-displayName'], '条目');
  assert.deepEqual(op.requestBody.content['application/json'].example, { description: 'Description', title: 'Title' });
  assert.deepEqual(op.requestBody.content['application/json'].schema.properties.description.enum, ['Description']);
  assert.equal(source.info.title, 'Title');
});

test('Chinese routes change Markdown links without changing runnable code', () => {
  const text = '[Guide](/orbitjob/docs/api-guide/)\n[Contract](/orbitjob/openapi.json)\n\n```bash\ncurl http://localhost:18080/api/v1/jobs\n```';
  const output = remapChineseLinks(text, '/preview');
  assert(output.startsWith('[Guide](/preview/zh-cn/docs/api-guide/)'));
  assert(output.includes('[Contract](/preview/openapi.json)'));
  assert(output.endsWith('```bash\ncurl http://localhost:18080/api/v1/jobs\n```'));
});

test('Chinese reference headings translate as whole labels, leaving code examples intact', () => {
  const text = '## Request\n### Request body\n\n```json\n{"description":"Request"}\n```\n## Responses';
  assert.equal(chineseReferenceText(text), '## 请求\n### 请求体\n\n```json\n{"description":"Request"}\n```\n## 响应');
});

test('Missing translations fail visibly instead of publishing an English fallback', () => {
  assert.throws(() => translateOpenAPI({ info: { title: 'Not translated' }, paths: {} }, {}), /Missing Chinese API text/);
});
