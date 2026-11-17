# 文档维护

静态文档站发布英文和简体中文主题，以及 HTTP、CRD 和 Chart 参考。文档站独立于已安装的 Console，不接收安装环境的凭据。英文位于站点根路径，中文位于配置的基础路径下的 `/zh-cn/`。

## 内容与导航

在仓库的 Markdown 文件中维护完整主题。[guide-sources.mjs](../site/scripts/guide-sources.mjs) 中的主题列表为每个来源分配页面标识和所属分区。构建时读取完整主题，不摘取 README 片段，也不按 USAGE 标题拆页。README 是仓库入口，USAGE 是任务索引。文档站有独立的首页，为读者提供不同的使用路径。

写作前先确定内容类型：

- 任务说明完成一个结果所需的权限、输入、操作顺序、成功条件和失败处理。
- 教程将步骤组成可运行的示例，并包含清理操作。
- 概念说明行为、关系和限制，并链接到相关操作。
- 参考说明字段、命令、约束或错误行为。

在维护的来源文件之间使用相对 Markdown 链接。每篇主题只保留一个 H1，标题应说明内容，前提条件应放在需要它们的步骤之前。生成器将已发布主题映射到站点路由，将其他仓库文件链接到开发分支。修改标题时应检查链接目标。新路由遵循主题层次，不保留未上线的旧路由。

产品行为必须与实现核对。配置以解析器、Chart、Schema 或清单为依据；API 行为以所属处理器和校验逻辑为依据；运维结论需要相应环境的检查。现有正文和代码注释都可能过时。未解决的行为应记入[已知问题](known-issues.md)，并在[验证记录](verification.md)中区分当前证据与历史观察。

## 本地检查

使用与 [site/package.json](../site/package.json) 兼容的 Node.js，当前要求为 `>=22.23.4`。在仓库根目录执行：

```bash
npm --prefix site ci
make openapi-check
npm --prefix site run build
npm --prefix site exec -- playwright install chromium
npm --prefix site run test:browser
```

生产构建会运行脚本测试，准备主题和 API 来源，构建 Astro 与 Pagefind，组装制品，并检查页面、Schema、操作覆盖、语言、本地链接和仓库目标。浏览器测试覆盖导航、请求示例、键盘搜索、代码复制、响应式布局和 API 浏览器失败恢复。这些检查不会执行安装指南，也不能证明符合所有无障碍要求。

预览生产制品：

```bash
npm --prefix site run preview -- --port 18432 --ignore-lock
```

打开 `http://127.0.0.1:18432/orbitjob/`。浏览器测试使用端口 18433。验证其他基础路径时，构建和测试应使用相同的 `SITE_BASE`：

```bash
SITE_BASE=/preview npm --prefix site run build
SITE_BASE=/preview npm --prefix site run test:browser
```

`site/src/content/docs/generated` 和 `site/src/content/docs/zh-cn/generated` 下的生成 Markdown、目录数据、OpenAPI JSON、搜索索引以及 `site/dist` 都是构建输出。应编辑它们的所属输入，再重新生成。检查已有输出不会重新构建已修改的输入。

## HTTP 参考与可下载助手

HTTP 契约根据路由注册和模型生成。分发的 CRD Schema 与 Helm 默认值有独立的生成参考；其中的字段不能替代语义检查或安装前提。修改这些输入后应重新生成。修改 HTTP 时执行：

```bash
make openapi-gen
make openapi-check
npm --prefix web run generate
npm --prefix site run build
```

对照实际处理器核对描述、认证、权限要求、示例和错误响应。生成文件是最新的，只能说明它与生成器一致。站点提供可搜索的操作与 Schema 参考，以及本地打包的 API 浏览器。API 浏览器禁用请求执行、凭据输入和外部服务。

Console 的工作负载助手包也包含当前文档、CRD 和 OpenAPI。新增工作负载编写所需的主题时，应更新生成器的来源列表；使用 `npm --prefix web run generate` 重新生成，并用 `npm --prefix web run types:check` 检查。保持包内相对链接和来源哈希一致。只有工作流或支持的字段发生变化时，才修改助手说明。

## 中文维护

先完成并验证英文主题。每篇发布主题都在 `site/i18n/zh-CN/topics/` 下有完整中文对应稿，使用 `guide-sources.mjs` 中的标识。逐段核对权限、条件、正常与失败行为、数值和限定条件。保留 API 字段、界面标签、命令、示例和链接目标等字面内容。生成器按对应英文来源解析中文来源中的链接，并将指向已发布主题的链接转到中文页面。非主题仓库链接保留原有语言。中文标题保留英文标题的定位标识，使跨主题链接和语言切换到达相同位置。

将 `site/i18n/zh-CN/api/current.json` 中的 HTTP 描述与完全一致的英文键核对。翻译只改变显示文本；路径、字段、Schema、示例、枚举和认证要求保留契约值。中文 API 浏览器使用本地化的显示副本；下载链接提供原始 OpenAPI 契约。两种语言中的 Helm 值和 CRD 字面内容保持不变。

完成语义审核后，记录已审核的输入：

```bash
node site/scripts/translation.mjs --record-reviewed
npm --prefix site run build
npm --prefix site run test:browser
```

审核基线记录来源和译文的哈希。输入发生变化时，应重新审核，再更新基线。哈希和代码块匹配不能证明语义等价。构建与浏览器检查覆盖两种语言，包括导航、页面标识、搜索和语言切换。`site/i18n/zh-CN/legacy/` 下的早期译文是历史工作材料，不参与发布。

## CI 与发布

Documentation 工作流通过共享 Documentation verification 工作流验证 `main`、`dev` 的修改和拉取请求。共享工作流检出请求的完整提交 SHA，确认检出结果，使用只读仓库权限检查 OpenAPI 是否最新、构建站点并运行浏览器检查。正式发布和预发布验证要求在所选源码提交上通过相同检查，然后才能执行安装和镜像发布。共享工作流不上传 Pages 制品，也不部署站点。失败诊断保留七天。

独立 Documentation 工作流请求已验证站点制品，保留一天。只有该工作流的 `main` 路径会将该制品上传到 Pages 并进入部署任务；部署任务使用单独的 Pages 写权限。main Benchmark 成功后，工作流先解析当前 `main` 提交，再执行验证，避免延迟完成的结果重新发布旧源码。工作流配置不能证明公开部署已存在或已经通过；宣布访问地址前应检查实际运行结果。

可以通过 `BENCHMARK_ARCHIVE` 纳入已有的基准归档。测量结果始终保留其来源、硬件和配置限制；重新构建文档不会产生新的性能结果。源码修改可以独立于发布接受审阅。
