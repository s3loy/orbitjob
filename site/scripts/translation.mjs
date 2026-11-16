import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import GithubSlugger from 'github-slugger';
import { guides } from './guide-sources.mjs';

export const translationFile = id => `site/i18n/zh-CN/topics/${id}.md`;
export const sha256 = text => createHash('sha256').update(text).digest('hex');

export function markdownParts(markdown) {
  const headings = [], fences = [], prose = [];
  let fence, block = [];
  for (const line of markdown.split('\n')) {
    const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (fence) {
      block.push(line);
      if (marker && marker[1][0] === fence.character && marker[1].length >= fence.length && !marker[2].trim()) {
        fences.push(block.join('\n')); fence = undefined; block = [];
      }
      continue;
    }
    if (marker) { fence = { character: marker[1][0], length: marker[1].length }; block = [line]; continue; }
    prose.push(line);
    const heading = line.match(/^(#{1,6}) (.+)$/);
    if (heading) headings.push({ depth: heading[1].length, text: heading[2].replace(/\s+#+\s*$/, '') });
  }
  assert(!fence, 'Unclosed Markdown fence');
  return { headings, fences, prose: prose.join('\n') };
}

const codeSpans = prose => [...prose.matchAll(/(`+)([^`]*?)\1/g)].map(match => match[2]).sort();
const linkTargets = prose => [...prose.replace(/(`+)[^`]*?\1/g, '').matchAll(/!?\[[^\]\n]*\]\(([^\s)]+)\)/g)].map(match => match[1]).sort();
const explicitIDs = prose => [...prose.matchAll(/\bid=["']([^"']+)["']/g)].map(match => match[1]).sort();

export function validateTopicTranslation(english, chinese, label = 'translation') {
  const source = markdownParts(english), translated = markdownParts(chinese);
  assert(source.headings[0]?.depth === 1 && translated.headings[0]?.depth === 1, `${label}: missing H1`);
  assert(/\p{Script=Han}/u.test(translated.headings[0].text), `${label}: untranslated title`);
  assert.deepEqual(translated.headings.map(h => h.depth), source.headings.map(h => h.depth), `${label}: heading structure differs`);
  assert.deepEqual(translated.fences, source.fences, `${label}: executable blocks or literal schemas differ`);
  assert.deepEqual(codeSpans(translated.prose), codeSpans(source.prose), `${label}: inline literals differ`);
  assert.deepEqual(linkTargets(translated.prose), linkTargets(source.prose), `${label}: source link targets differ`);
  assert.deepEqual(explicitIDs(translated.prose), explicitIDs(source.prose), `${label}: explicit anchors differ`);
  return { headings: source.headings.length, fences: source.fences.length };
}

function plainHeading(text) {
  return text.replace(/!?\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/<[^>]+>/g, '').replace(/[`*~]/g, '').replace(/&amp;/g, '&');
}

// Pair heading identities while retaining translated labels in the visible TOC.
export function alignHeadingIDs(english, chinese) {
  const headings = markdownParts(english).headings;
  const slugger = new GithubSlugger();
  const ids = headings.map(heading => slugger.slug(plainHeading(heading.text)));
  let index = 0, fence;
  const output = chinese.split('\n').map(line => {
    const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (marker) {
      if (!fence) fence = { character: marker[1][0], length: marker[1].length };
      else if (marker[1][0] === fence.character && marker[1].length >= fence.length && !marker[2].trim()) fence = undefined;
      return line;
    }
    if (fence) return line;
    const heading = line.match(/^(#{1,6}) (.+)$/);
    if (!heading) return line;
    assert.equal(heading[1].length, headings[index]?.depth, 'Translated heading level differs');
    return `${line} {#${ids[index++]}}`;
  }).join('\n');
  assert.equal(index, headings.length, 'Translated heading count differs');
  return output;
}

// Preserve explicit identities before Astro collects headings and TOC labels.
export function translatedHeadingIDs() {
  return {
    name: 'translated-heading-ids',
    element: {
      filter: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'],
      visit(node, context) {
        const last = node.children?.at(-1);
        const match = last?.type === 'text' && last.value.match(/\s+\{#([^\s{}]+)\}$/);
        if (match) {
          context.replaceNode(last, { type: 'text', value: last.value.slice(0, -match[0].length) });
          context.setProperty(node, 'id', match[1]);
        }
      },
    },
  };
}

export async function validateTranslationBaseline(repo, entries = guides) {
  const baseline = JSON.parse(await readFile(path.join(repo, 'site/i18n/zh-CN/reviewed.json'), 'utf8'));
  assert.deepEqual(Object.keys(baseline.topics).sort(), entries.map(entry => entry.id).sort(), 'Chinese review baseline must cover every published topic');
  for (const entry of entries) {
    const english = await readFile(path.join(repo, entry.source), 'utf8');
    const chinese = await readFile(path.join(repo, translationFile(entry.id)), 'utf8');
    const recorded = baseline.topics[entry.id];
    assert.equal(recorded.source, entry.source, `Changed translation mapping: ${entry.id}`);
    assert.equal(recorded.englishSha256, sha256(english), `English changed since Chinese review: ${entry.source}`);
    assert.equal(recorded.chineseSha256, sha256(chinese), `Chinese changed since review: ${entry.id}`);
    validateTopicTranslation(english, chinese, entry.id);
  }
  for (const source of ['api/openapi.yaml', 'site/i18n/zh-CN/api/current.json']) {
    assert.equal(baseline.references?.[source], sha256(await readFile(path.join(repo, source), 'utf8')), `API input changed since Chinese review: ${source}`);
  }
}

// Invoke only after semantic review; recording hashes does not perform that review.
if (process.argv[1] === fileURLToPath(import.meta.url) && process.argv.includes('--record-reviewed')) {
  const repo = fileURLToPath(new URL('../../', import.meta.url));
  const topics = {};
  for (const entry of guides) {
    const english = await readFile(path.join(repo, entry.source), 'utf8');
    const chinese = await readFile(path.join(repo, translationFile(entry.id)), 'utf8');
    validateTopicTranslation(english, chinese, entry.id);
    topics[entry.id] = { source: entry.source, englishSha256: sha256(english), chineseSha256: sha256(chinese) };
  }
  const references = {};
  for (const source of ['api/openapi.yaml', 'site/i18n/zh-CN/api/current.json']) references[source] = sha256(await readFile(path.join(repo, source), 'utf8'));
  await writeFile(path.join(repo, 'site/i18n/zh-CN/reviewed.json'), JSON.stringify({ topics, references }, null, 2) + '\n');
  console.log(`Recorded ${guides.length} supplied translation reviews; hashes do not replace semantic review.`);
}
