import assert from 'node:assert/strict';
import test from 'node:test';
import { guides, validateGuides, rewriteLinks } from './guide-sources.mjs';

test('topics have distinct sources and structured identities', () => {
  validateGuides();
  assert(guides.some(guide => guide.id === 'tasks/read-logs'));
  assert.throws(() => validateGuides([guides[0],guides[0]]), /Duplicate topic/);
  assert.throws(() => validateGuides([{source:'../private.md',id:'tasks/unsafe',group:'tasks'}]), /Unsafe/);
});

test('published source links respect the deployment base and keep fragments', () => {
  const routes = new Map([['docs/accounts.md','/preview/docs/operations/accounts/']]);
  assert.equal(rewriteLinks('[Accounts](../accounts.md#sessions)','docs/tasks/example.md',routes,'/preview'), '[Accounts](/preview/docs/operations/accounts/#sessions)');
  assert.equal(rewriteLinks('[Contract](../../api/openapi.yaml)','docs/tasks/example.md',routes,'/preview'), '[Contract](/preview/openapi.json)');
});

test('link rewriting preserves inline literals and CommonMark fences', () => {
  const routes = new Map([['README.md','/preview/']]);
  const literal = '````md\n```\n[Readme](README.md)\n~~~\n````\n`[Readme](README.md)`\n[Readme](README.md)';
  assert.equal(rewriteLinks(literal,'USAGE.md',routes,'/preview'),literal.replace(/\n\[Readme\]\(README.md\)$/,'\n[Readme](/preview/)'));
});
