import { readFile, writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { parse, stringify } from 'yaml';
import { alignHeadingIDs } from './translation.mjs';
const cell = value => String(value ?? '').replaceAll('|','\\|').replaceAll('\n',' ');

export async function generateConfiguration({ repo, output, base, locale = '' }) {
  const zh = locale === 'zh-cn';
  const pages = [];
  async function write(id,title,body) {
    if (zh) {
      const englishFile = path.join(repo, 'site/src/content/docs/generated', `${id}.md`);
      const english = (await readFile(englishFile, 'utf8')).replace(/^---\n[\s\S]*?\n---\n/, '').trim();
      let fence = false;
      body = body.split('\n').map(line => {
        if (line.startsWith('```')) { fence = !fence; return line; }
        if (fence) return line;
        return line
          .replace('Use these defaults with the matching chart. Empty values can require installation-specific input; a rendered chart does not establish database, storage or network readiness. See [configuration]', '请将这些默认值与对应版本的 Chart 配合使用。空值可能需要按安装环境填写；Chart 渲染成功不能证明数据库、存储或网络已经就绪。见[配置参考]')
          .replace(' and [installation]', '和[安装指南]')
          .replace('| Value | Distributed default |', '| 配置项 | 分发默认值 |')
          .replace('## Complete values', '## 完整配置值')
          .replace(' · namespace-scoped ', ' · 命名空间级资源 ')
          .replace('Required means required within the containing object. Admission and reconciliation enforce additional semantic and permission checks. Read the [workload resource reference]', '必填表示该字段在所属对象中必填。准入与协调过程还会执行语义和权限检查。编写定义前，请阅读[工作负载资源参考]')
          .replace(' before authoring definitions. Run resources are normally created through the authorized trigger API.', '。执行资源通常由经过授权的触发 API 创建。')
          .replace('| Field | Type | Required | Constraint or description |', '| 字段 | 类型 | 必填 | 约束或说明 |')
          .replaceAll('| Yes |', '| 是 |').replaceAll('| No |', '| 否 |')
          .replaceAll('Allowed: ', '允许值：')
          .replace('## Complete distributed schema', '## 完整分发 Schema')
          .replace('[Source CRD]', '[源 CRD]');
      }).join('\n');
      body = alignHeadingIDs(english, body);
      title = title === 'Helm values' ? 'Helm 配置值' : title.replace(' schema', ' 资源 Schema');
    }
    const file = path.join(output,`${id}.md`);
    await mkdir(path.dirname(file),{recursive:true});
    await writeFile(file,`---\ntitle: ${JSON.stringify(title)}\nslug: ${zh ? 'zh-cn/' : ''}docs/${id}\n---\n\n${body}\n`);
    pages.push({route:`${base}/docs/${id}/`,title});
  }
  const chart = parse(await readFile(path.join(repo,'charts/orbitjob/values.yaml'),'utf8'));
  let rows = '';
  function values(value,prefix='') {
    for (const [name,item] of Object.entries(value)) {
      const key = prefix ? `${prefix}.${name}` : name;
      if (item && typeof item === 'object' && !Array.isArray(item) && Object.keys(item).length) values(item,key);
      else rows += `| \`${key}\` | \`${cell(JSON.stringify(item))}\` |\n`;
    }
  }
  values(chart);
  await write('reference/helm-values','Helm values', 'Use these defaults with the matching chart. Empty values can require installation-specific input; a rendered chart does not establish database, storage or network readiness. See [configuration]('+base+'/docs/reference/configuration/) and [installation]('+base+'/docs/setup/installation/).\n\n| Value | Distributed default |\n| --- | --- |\n'+rows+'\n## Complete values\n\n```yaml\n'+stringify(chart)+'```');
  for (const resource of ['scheduledjobs','jobruns','workflowjobs','workflowruns']) {
    const source = `charts/orbitjob/crds/workloads.orbitjob.io_${resource}.yaml`;
    const crd = parse(await readFile(path.join(repo,source),'utf8'));
    const version = crd.spec.versions.find(version=>version.name==='v1alpha1');
    let fields = '';
    function schema(value,prefix='') {
      for (const [name,item] of Object.entries(value.properties || {})) {
        const key = prefix ? `${prefix}.${name}` : name;
        fields += `| \`${key}\` | ${cell(item.type || '')} | ${value.required?.includes(name) ? 'Yes' : 'No'} | ${cell(item.enum ? 'Allowed: '+item.enum.join(', ') : item.description || '')} |\n`;
        schema(item,key);
        if (item.items) schema(item.items,`${key}[]`);
      }
    }
    schema(version.schema.openAPIV3Schema);
    const kind=crd.spec.names.kind;
    await write(`reference/kubernetes/${resource}`,kind+' schema', `\`workloads.orbitjob.io/v1alpha1\` · namespace-scoped \`${resource}\`.\n\nRequired means required within the containing object. Admission and reconciliation enforce additional semantic and permission checks. Read the [workload resource reference](${base}/docs/reference/workload-resources/) before authoring definitions. Run resources are normally created through the authorized trigger API.\n\n| Field | Type | Required | Constraint or description |\n| --- | --- | --- | --- |\n${fields}\n## Complete distributed schema\n\n\`\`\`yaml\n${stringify(version.schema.openAPIV3Schema)}\`\`\`\n\n[Source CRD](https://github.com/s3loy/orbitjob/blob/dev/${source})`);
  }
  return pages;
}
