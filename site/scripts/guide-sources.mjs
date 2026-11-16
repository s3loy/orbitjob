import assert from 'node:assert/strict';
import path from 'node:path';

export const groups = [
  ['getting-started', 'Getting started', 'Choose a path and prepare for your first execution.'],
  ['concepts', 'Concepts', 'Understand execution, ownership and authorization.'],
  ['tasks', 'Tasks', 'Complete a specific operation on your installation.'],
  ['tutorials', 'Tutorials', 'Work through a complete example.'],
  ['reference', 'Reference', 'Look up interfaces, fields and configuration.'],
  ['setup', 'Installation', 'Prepare and install a consistent set of components.'],
  ['operations', 'Operations', 'Observe, upgrade and recover an installation.'],
  ['contribute', 'Contributing', 'Build, test and maintain the project.'],
].map(([id, title, description]) => ({ id, title, description }));

export const chineseGroups = {
  'getting-started': ['开始使用', '选择使用路径，准备首次执行。'],
  concepts: ['概念', '理解执行行为、资源归属和授权边界。'],
  tasks: ['任务', '完成安装环境中的具体操作。'],
  tutorials: ['教程', '按步骤完成一个完整示例。'],
  reference: ['参考', '查询接口、字段和配置约束。'],
  setup: ['安装', '准备环境并安装版本一致的组件。'],
  operations: ['运维', '观察、升级和恢复安装环境。'],
  contribute: ['参与贡献', '构建、测试并维护项目。'],
};

// Topic identities remain independent of source filenames and headings.
export const guides = [
  ['docs/getting-started.md', 'getting-started'],
  ['docs/architecture.md', 'concepts/architecture'],
  ['docs/concepts/execution-model.md', 'concepts/execution-model'],
  ['SECURITY.md', 'concepts/security'],
  ...['manage-jobs', 'trigger-and-observe', 'cancel-a-run', 'read-logs', 'manage-access', 'call-api', 'checks-and-slos', 'functions-and-workflows'].map(id => [`docs/tasks/${id}.md`, `tasks/${id}`]),
  ['docs/tutorials/first-job.md', 'tutorials/first-job'],
  ['docs/cli.md', 'reference/cli'],
  ['docs/database-setup.md', 'reference/database'],
  ['docs/execution-logs.md', 'reference/execution-logs'],
  ['docs/reference/configuration.md', 'reference/configuration'],
  ['docs/reference/http-conventions.md', 'reference/http-conventions'],
  ['docs/reference/access-policies.md', 'reference/access-policies'],
  ['docs/reference/workload-resources.md', 'reference/workload-resources'],
  ['docs/setup/prerequisites.md', 'setup/prerequisites'],
  ['docs/setup/installation.md', 'setup/installation'],
  ['docs/accounts.md', 'operations/accounts'],
  ...['upgrading', 'backup-and-recovery', 'monitoring', 'troubleshooting'].map(id => [`docs/operations/${id}.md`, `operations/${id}`]),
  ['docs/known-issues.md', 'operations/limitations'],
  ['docs/local-maintenance.md', 'operations/maintenance'],
  ['CONTRIBUTING.md', 'contribute/overview'],
  ['docs/local-deployment.md', 'contribute/local-development'],
  ['docs/loadtest-guide.md', 'contribute/load-testing'],
  ['docs/ci.md', 'contribute/ci'],
  ['docs/project-site.md', 'contribute/documentation'],
].map(([source, id]) => ({ source, id, group: id.split('/')[0] }));

export function validateGuides(entries = guides) {
  const ids = new Set();
  const sources = new Set();
  for (const { source, id, group } of entries) {
    assert(/^[a-z0-9-]+(?:\/[a-z0-9-]+)*$/.test(id), `Invalid topic identity: ${id}`);
    assert(!ids.has(id), `Duplicate topic identity: ${id}`);
    assert(!sources.has(source), `Duplicate content source: ${source}`);
    assert(groups.some(({ id }) => id === group), `Unknown documentation group: ${group}`);
    assert(!source.startsWith('/') && !source.split('/').includes('..'), `Unsafe content source: ${source}`);
    ids.add(id); sources.add(source);
  }
}

export function rewriteLinks(text, source, routes, base, sourceRef = 'dev') {
  let fence;
  return text.split('\n').map(line => {
    const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (marker) {
      if (!fence) fence = { marker: marker[1][0], length: marker[1].length };
      else if (marker[1][0] === fence.marker && marker[1].length >= fence.length && !marker[2].trim()) fence = undefined;
      return line;
    }
    if (fence) return line;
    return line.replace(/(`+)[^`]*\1|(!?\[[^\]\n]+\])\(([^\s)]+)\)/g, (match, code, label, target) => {
      if (code || /^(?:[a-z][a-z\d+.-]*:|\/\/|#)/i.test(target)) return match;
      const position = target.search(/[?#]/);
      const file = position < 0 ? target : target.slice(0, position);
      const suffix = position < 0 ? '' : target.slice(position);
      if (file.startsWith('/')) return match;
      const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(source), file));
      if (routes.has(resolved)) return `${label}(${routes.get(resolved)}${suffix})`;
      if (resolved === 'api/openapi.yaml') return `${label}(${base}/openapi.json)`;
      return `${label}(https://github.com/s3loy/orbitjob/blob/${sourceRef}/${resolved}${suffix})`;
    });
  }).join('\n');
}
