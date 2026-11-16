import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';

export async function loadChineseAPI(directory) {
  const dictionary = {};
  for (const file of (await readdir(directory)).filter((name) => name.endsWith('.json')).sort()) {
    const entries = JSON.parse(await readFile(path.join(directory, file), 'utf8'));
    for (const [english, chinese] of Object.entries(entries)) {
      assert(typeof chinese === 'string' && chinese.trim(), `Empty Chinese API translation in ${file}: ${english}`);
      assert(!dictionary[english] || dictionary[english] === chinese, `Conflicting Chinese API translation: ${english}`);
      dictionary[english] = chinese;
    }
  }
  return dictionary;
}

export function translateOpenAPI(source, dictionary) {
  const spec = structuredClone(source);
  const missing = new Set();
  const translate = (value) => {
    if (typeof value !== 'string' || !value.trim()) return value;
    if (!dictionary[value]) { missing.add(value); return value; }
    return dictionary[value];
  };
  function visit(value, key = '') {
    if (!value || typeof value !== 'object') return;
    // Example payloads are executable wire data, not translation material.
    if (['example', 'examples', 'default', 'enum'].includes(key)) return;
    for (const [name, item] of Object.entries(value)) {
      if (['title', 'summary', 'description'].includes(name) && typeof item === 'string') value[name] = translate(item);
      else visit(item, name);
    }
  }
  visit(spec);
  const tagNames = [...new Set(Object.values(spec.paths).flatMap((item) => Object.values(item)
    .flatMap((operation) => operation?.tags || [])))];
  const existing = new Map((spec.tags || []).map((tag) => [tag.name, tag]));
  spec.tags = tagNames.map((name) => ({ ...existing.get(name), name, 'x-displayName': translate(name) }));
  assert.equal(missing.size, 0, `Missing Chinese API text:\n${[...missing].join('\n')}`);
  return spec;
}

const labels = [
  ['## Access', '## 访问权限'], ['## Request', '## 请求'], ['## Responses', '## 响应'],
  ['### Request body', '### 请求体'], ['### cURL template', '### cURL 调用模板'],
  ['## Fields', '## 字段'], ['## Complete schema', '## 完整 Schema'], ['## Used by', '## 使用此 Schema 的接口'],
  ['| Name | Location | Required | Type | Description |', '| 参数名 | 位置 | 必填 | 类型 | 说明 |'],
  ['| Field | Required | Type | Description |', '| 字段 | 必填 | 类型 | 说明 |'],
  ['| Operation | Purpose |', '| 接口 | 用途 |'], ['| Yes |', '| 是 |'], ['| No |', '| 否 |'],
  ['Schema reference: ', '相关 Schema：'], ['Required policy action: ', '所需策略 action：'],
  ['. Resource scope and policy boundaries are enforced by the service.', '。资源范围和策略边界由服务端校验。'],
  ['[Authentication and calling conventions]', '[认证方式与调用约定]'],
  ['This operation does not require authentication.', '此接口无需认证。'],
  ['Read the operation description for its authorization requirements.', '授权要求见本接口说明。'],
  ['No path, query or header parameters are declared.', '此接口没有声明路径、查询或请求头参数。'],
  ['Parameter constraints (including defaults, enums, limits and patterns):', '参数约束（包括默认值、枚举、范围和格式）：'],
  ['This operation has no request body.', '此接口不接收请求体。'],
  ['No response body is declared.', '此响应没有声明响应体。'], ['Response headers:', '响应头：'],
  ['Example body (replace sample resource IDs with your own):', '请求体示例（请将示例资源 ID 替换为实际值）：'],
  ['Examples:', '示例：'], ['Schema generated from the service contract.', '此 Schema 从服务端接口契约自动生成。'],
  ['Set `ORBITJOB_API` to your installation origin. Replace path variables with the resource IDs above and URL-encode query values.', '将 `ORBITJOB_API` 设为实际安装的 API 地址。路径变量使用上表中的资源 ID；查询参数值需要进行 URL 编码。'],
  [' Create `request.json` using the required body schema.', ' 按请求体 Schema 准备 `request.json`。'],
  ['This is a minimal tenant-key request. Platform callers must select `tenant_id` where this operation supports it. Optional concurrency, version and idempotency fields remain in the parameter table above; use the calling guide for a complete execution or safe-retry sequence.', '这是使用租户 API key 的最小请求。平台凭据在接口支持时需要显式指定 `tenant_id`。可选的并发、版本和幂等参数见上表；完整执行流程和安全重试方式请参考调用指南。'],
  ['[Open this operation in the rich API reference]', '[在 API 浏览器中查看此接口]'],
  ['[Calling the API]', '[API 调用指南]'], ['[Rich API reference]', '[API 浏览器]'], ['[Download OpenAPI]', '[下载原始 OpenAPI]'],
  ['Account endpoints use human sessions, not API keys. Set `ORBITJOB_ORIGIN` to the exact configured Console origin. Obtain a private cookie jar through login and read the CSRF token from the same-origin session endpoint before authenticated writes. Setup, invitation and reset capabilities have separate prerequisites; see the [account guide]', '账户接口使用个人会话。将 `ORBITJOB_ORIGIN` 设置为配置中的确切 Console 源地址。通过登录获取私有 Cookie 文件；在进行认证写入前，从同源会话接口读取 CSRF token。首次设置、邀请和密码重置令牌各有前提要求，见[账户指南]'],
  ['Never paste a capability into this documentation site.', '请勿将能力令牌粘贴到本文档站。'],
  ['### Accept as an existing account', '### 使用已有账户接受邀请'],
  ['The preceding template is the anonymous new-account flow. To join an existing account, first sign in and keep its private cookie jar. Read its current CSRF token, then submit `request.json` containing the invitation `token`; an optional `username` must match the signed-in account. Do not submit another account\'s password to claim its membership.', '前一个模板适用于匿名创建新账户。已有账户接受邀请时，应先登录并保留私有 Cookie 文件。读取当前 CSRF token 后，提交包含邀请 `token` 的 `request.json`；若指定 `username`，必须与已登录账户一致。不得通过提交其他账户的密码获取其成员身份。'],
  ['Alternative: ', '其他认证方式：'],
  ['Use the authentication scheme declared in the contract.', '使用契约中声明的认证方式。'],
  ['array of ', '数组，元素类型为 '],
];

export function chineseReferenceText(markdown) {
  let inFence = false;
  return markdown.split('\n').map((line) => {
    if (/^\s*```/.test(line)) { inFence = !inFence; return line; }
    if (inFence) return line;
    let text = line;
    for (const [english, chinese] of [...labels].sort((a, b) => b[0].length - a[0].length)) text = text.replaceAll(english, chinese);
    text = text.replace(/^(\d+) HTTP operations\. Every operation and schema below is generated from the service contract and included in site search\.$/, '$1 个 HTTP 操作。以下接口和 Schema 均由服务端契约生成，并已纳入文档搜索。');
    text = text.replace(/^Required\. /, '必填。').replace(/^Optional\. /, '选填。');
    return text;
  }).join('\n');
}

export function remapChineseLinks(markdown, base) {
  // Translation files keep their English route/anchor IDs; localization changes
  // navigation targets only, never a code block, request URL or JSON field.
  let fence = false;
  return markdown.split('\n').map((line) => {
    if (/^\s*(```|~~~)/.test(line)) { fence = !fence; return line; }
    if (fence) return line;
    return line.replace(/\]\(([^)]+)\)/g, (match, target) => {
      for (const prefix of new Set([base, '/orbitjob'])) {
        if (target === `${prefix}/openapi.json`) return `](${base}/openapi.json)`;
        if (target.startsWith(`${prefix}/docs/`) || target.startsWith(`${prefix}/api/`) || target.startsWith(`${prefix}/benchmarks/`)) return `](${base}/zh-cn/${target.slice(prefix.length + 1)})`;
      }
      return match;
    });
  }).join('\n');
}
