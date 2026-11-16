import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { stringify } from 'yaml';
import { chineseReferenceText } from './localize.mjs';
import { alignHeadingIDs } from './translation.mjs';

export const apiSlug = (name) => name.toLowerCase();
const cell = (value = '') => String(value).replaceAll('|', '\\|').replaceAll('\n', ' ');
const code = (value) => `\`${cell(value)}\``;
const schemaName = (ref) => ref.slice('#/components/schemas/'.length);
const fence = (value) => `\n\n\`\`\`yaml\n${stringify(value).trim()}\n\`\`\`\n`;

// Every page is derived from the same contract; no second hand-authored API.
export async function generateAPI({ spec, operations, output, base, locale = '', englishOutput }) {
  const schemaHref = (name) => `${base}/docs/reference/http/schemas/${apiSlug(name)}/`;
  const opHref = (id) => `${base}/docs/reference/http/operations/${apiSlug(id)}/`;
  const pages = [];
  const used = new Set();
  async function write(id, title, text) {
    assert(!used.has(id), `Duplicate API slug: ${id}`);
    used.add(id);
    const file = path.join(output, `${id}.md`);
    await mkdir(path.dirname(file), { recursive: true });
    let fenceOpen = false;
    let body = locale === 'zh-cn' ? chineseReferenceText(text) : text;
    body = body.split('\n').map((line) => {
      if (/^\s*```/.test(line)) { fenceOpen = !fenceOpen; return line; }
      // Literal angle-bracket placeholders are prose, not HTML elements.
      return fenceOpen ? line : line.split(/(`+[^`]*`+)/).map((part, index) => index % 2 ? part : part.replaceAll('<', '&lt;').replaceAll('>', '&gt;')).join('');
    }).join('\n');
    if (locale === 'zh-cn') {
      const english = await readFile(path.join(englishOutput, `${id}.md`), 'utf8');
      body = alignHeadingIDs(english.replace(/^---\n[\s\S]*?\n---\n/, '').trim(), body);
    }
    await writeFile(file, `---\ntitle: ${JSON.stringify(title)}\nslug: ${locale ? `${locale}/` : ''}docs/${id.replace(/\/index$/, '')}\neditUrl: https://github.com/s3loy/orbitjob/blob/dev/api/openapi.yaml\n---\n\n${body.trim()}\n`);
    pages.push({ route: `${base}/docs/${id.replace(/\/index$/, '')}/`, title });
  }
  function refs(value) {
    if (!value || typeof value !== 'object') return [];
    const found = Object.values(value).flatMap((item) => refs(item));
    if (typeof value.$ref === 'string' && value.$ref.startsWith('#/components/schemas/')) found.push(schemaName(value.$ref));
    return [...new Set(found)];
  }
  function type(schema = {}) {
    if (schema.$ref) { const name = schemaName(schema.$ref); return `[${name}](${schemaHref(name)})`; }
    if (schema.type === 'array') return `array of ${type(schema.items)}`;
    if (schema.oneOf) return schema.oneOf.map(type).join(locale ? ' 或 ' : ' or ');
    if (schema.anyOf) return schema.anyOf.map(type).join(locale ? ' 或 ' : ' or ');
    return `${schema.type || 'object'}${schema.format ? ` (${schema.format})` : ''}`;
  }
  function details(schema) {
    const names = refs(schema);
    return (names.length ? `\nSchema reference: ${names.map((name) => `[${name}](${schemaHref(name)})`).join(', ')}.\n` : '') + fence(schema);
  }
  const groups = new Map();
  for (const entry of operations) {
    const op = spec.paths[entry.path][entry.method];
    const parameters = [...(spec.paths[entry.path].parameters || []), ...(op.parameters || [])];
    entry.href = opHref(entry.id);
    entry.summary = op.summary || '';
    entry.permission = op['x-orbitjob-permission'] || null;
    entry.authenticated = (op.security ?? spec.security ?? []).length > 0;
    entry.tagLabel = spec.tags?.find((tag) => tag.name === entry.tag)?.['x-displayName'] || entry.tag;
    if (!groups.has(entry.tag)) groups.set(entry.tag, []);
    groups.get(entry.tag).push(entry);
    let body = `${code(entry.method.toUpperCase())} ${code(entry.path)}\n\n${op.description || op.summary || ''}\n`;
    body += '\n## Access\n\n';
    if (entry.authenticated) {
      const security = op.security ?? spec.security;
      body += security.map((requirement) => Object.keys(requirement).map((name) => {
        const scheme = spec.components?.securitySchemes?.[name];
        return `${code(name)}: ${scheme?.description || 'Use the authentication scheme declared in the contract.'}`;
      }).join(locale ? ' 且 ' : ' and ')).join('\n\nAlternative: ') + '\n';
    } else body += 'This operation does not require authentication.\n';
    body += entry.permission ? `\nRequired policy action: ${code(entry.permission)}. Resource scope and policy boundaries are enforced by the service.\n` : (entry.authenticated ? '\nRead the operation description for its authorization requirements.\n' : '');
    body += `\n[Authentication and calling conventions](${base}/docs/tasks/call-api/).\n`;
    body += '\n## Request\n\n';
    if (parameters.length) {
      body += '| Name | Location | Required | Type | Description |\n| --- | --- | --- | --- | --- |\n';
      for (const param of parameters) body += `| ${code(param.name)} | ${param.in} | ${param.required ? 'Yes' : 'No'} | ${type(param.schema)} | ${cell(param.description || param.schema?.description || '')} |\n`;
      body += '\nParameter constraints (including defaults, enums, limits and patterns):\n' + fence(parameters);
    } else body += 'No path, query or header parameters are declared.\n';
    const request = op.requestBody;
    if (request) {
      body += `\n### Request body\n\n${request.required ? 'Required' : 'Optional'}. ${request.description || ''}\n`;
      for (const [media, content] of Object.entries(request.content || {})) {
        body += `\n#### ${media}\n${details(content.schema || {})}`;
        if (content.example !== undefined) body += '\nExample body (replace sample resource IDs with your own):\n\n```json\n' + JSON.stringify(content.example, null, 2) + '\n```\n';
        if (content.examples) body += '\nExamples:\n' + fence(content.examples);
      }
    } else body += '\nThis operation has no request body.\n';
    const accountEndpoint = entry.path.startsWith('/api/v1/auth/');
    const endpoint = entry.path.replace(/\{([^}]+)\}/g, (_, name) => `$${name.toUpperCase()}`);
    const query = parameters.filter((p) => p.in === 'query' && p.required).map((p) => `${p.name}=$${p.name.toUpperCase()}`).join('&');
    const lines = [`curl --request ${entry.method.toUpperCase()} \\`, `  --url "$ORBITJOB_API${endpoint}${query ? `?${query}` : ''}"`];
    if (accountEndpoint) {
      lines.push('  --header "Origin: $ORBITJOB_ORIGIN"');
      if (entry.authenticated || entry.id === 'getConsoleSession') lines.push('  --cookie cookies.txt');
      if (['loginConsole', 'setupConsole', 'acceptConsoleInvitation'].includes(entry.id)) lines.push('  --cookie-jar cookies.txt');
      if (!['get', 'head', 'options'].includes(entry.method) && entry.authenticated) lines.push('  --header "X-CSRF-Token: $ORBITJOB_CSRF_TOKEN"');
    } else if (entry.authenticated) lines.push('  --header "Authorization: Bearer $ORBITJOB_API_KEY"');
    for (const param of parameters.filter((p) => p.in === 'header' && p.required)) lines.push(`  --header "${param.name}: $${param.name.toUpperCase().replaceAll('-', '_')}"`);
    if (request?.required) {
      const media = Object.keys(request.content || {})[0] || 'application/json';
      lines.push(`  --header 'Content-Type: ${media}'`, '  --data-binary @request.json');
    }
    body += `\n### cURL template\n\nSet ${code('ORBITJOB_API')} to your installation origin. Replace path variables with the resource IDs above and URL-encode query values.${request?.required ? ' Create `request.json` using the required body schema.' : ''}\n\n`;
    if (accountEndpoint) body += 'Account endpoints use human sessions, not API keys. Set `ORBITJOB_ORIGIN` to the exact configured Console origin. Obtain a private cookie jar through login and read the CSRF token from the same-origin session endpoint before authenticated writes. Setup, invitation and reset capabilities have separate prerequisites; see the [account guide](' + base + '/docs/operations/accounts/). Never paste a capability into this documentation site.\n\n';
    else if (entry.authenticated) body += 'This is a minimal tenant-key request. Platform callers must select `tenant_id` where this operation supports it. Optional concurrency, version and idempotency fields remain in the parameter table above; use the calling guide for a complete execution or safe-retry sequence.\n\n';
    body += `\`\`\`bash\n${lines.map((line, i) => line + (i > 0 && i < lines.length - 1 ? ' \\' : '')).join('\n')}\n\`\`\`\n`;
    if (entry.id === 'acceptConsoleInvitation') {
      body += '\n### Accept as an existing account\n\nThe preceding template is the anonymous new-account flow. To join an existing account, first sign in and keep its private cookie jar. Read its current CSRF token, then submit `request.json` containing the invitation `token`; an optional `username` must match the signed-in account. Do not submit another account\'s password to claim its membership.\n\n```bash\ncurl --request POST \\\n  --url "$ORBITJOB_API/api/v1/auth/invitations/accept" \\\n  --header "Origin: $ORBITJOB_ORIGIN" \\\n  --header "X-CSRF-Token: $ORBITJOB_CSRF_TOKEN" \\\n  --header \'Content-Type: application/json\' \\\n  --cookie cookies.txt \\\n  --cookie-jar cookies.txt \\\n  --data-binary @request.json\n```\n';
    }
    body += '\n## Responses\n';
    for (const [status, response] of Object.entries(op.responses || {})) {
      body += `\n### ${status}: ${response.description || 'Response'}\n`;
      if (response.headers) body += '\nResponse headers:\n' + fence(response.headers);
      if (!response.content) body += '\nNo response body is declared.\n';
      for (const [media, content] of Object.entries(response.content || {})) body += `\n${code(media)}\n${details(content.schema || {})}`;
    }
    const tagSlug = entry.tag.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    body += `\n[Open this operation in the rich API reference](${base}/api/#tag/${tagSlug}/${entry.id}).\n`;
    await write(`reference/http/operations/${apiSlug(entry.id)}`, op.summary || entry.id, body);
  }
  for (const [name, schema] of Object.entries(spec.components?.schemas || {})) {
    let body = `${schema.description || 'Schema generated from the service contract.'}\n`;
    if (schema.properties) {
      body += '\n## Fields\n\n| Field | Required | Type | Description |\n| --- | --- | --- | --- |\n';
      for (const [key, property] of Object.entries(schema.properties)) body += `| ${code(key)} | ${schema.required?.includes(key) ? 'Yes' : 'No'} | ${type(property)} | ${cell(property.description || '')} |\n`;
    }
    body += '\n## Complete schema\n' + details(schema);
    const consumers = operations.filter((entry) => refs(spec.paths[entry.path][entry.method]).includes(name));
    if (consumers.length) body += '\n## Used by\n\n' + consumers.map((entry) => `- [${entry.method.toUpperCase()} ${entry.path}](${entry.href})`).join('\n') + '\n';
    await write(`reference/http/schemas/${apiSlug(name)}`, name, body);
  }
  const contractBase = locale === 'zh-cn' ? base.replace(/\/zh-cn$/, '') : base;
  let index = `${operations.length} HTTP operations. Every operation and schema below is generated from the service contract and included in site search.\n\n[Calling the API](${base}/docs/tasks/call-api/) · [Rich API reference](${base}/api/) · [Download OpenAPI](${contractBase}/openapi.json)\n`;
  for (const [tag, entries] of groups) {
    index += `\n## ${entries[0].tagLabel || tag}\n\n| Operation | Purpose |\n| --- | --- |\n`;
    for (const entry of entries) index += `| [${entry.method.toUpperCase()} ${entry.path}](${entry.href}) | ${cell(entry.summary)} |\n`;
  }
  await write('reference/http/index', locale === 'zh-cn' ? 'API 参考' : 'API reference', index);
  return pages;
}
