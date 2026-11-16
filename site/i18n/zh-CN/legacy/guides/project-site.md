---
title: "文档站维护"
---

`site/` 中的文档站发布安装指南、架构、运维和完整 HTTP API，独立于安装版 Console 构建，发布边界遵循 ADR 0022。[项目状态](/orbitjob/docs/status/)说明实现，[验证记录](/orbitjob/docs/verification/)记录检查与环境。

<a id="sources"></a>

## 内容来源

| 发布内容 | 权威来源 |
|---|---|
| 快速开始 | `README.md` 的 Quick Start |
| 使用与调用指南 | `USAGE.md` |
| 架构、安全、运维与验证 | `site/scripts/prepare-content.mjs` 中显式列出的来源 |
| 静态 API operation/schema 页面与富交互参考 | 从 HTTP registry 生成的 `api/openapi.yaml` |
| 依赖版本 | `site/package.json`、`site/package-lock.json` |
| 发布事件和权限 | `.github/workflows/docs.yml` |

修改这些来源。生成 Markdown、catalog JSON、OpenAPI JSON 和静态资源都是被忽略的派生内容。静态 API 页包含全部 operation、parameter、request example、response 和 schema，并纳入站内搜索。富交互参考使用本地打包的 renderer 展示同一契约，关闭请求执行、认证输入、telemetry 和外部服务。把示例复制到访问自己安装实例的客户端中执行。

<a id="local-build-and-usability-checks"></a>

## 本地构建与可用性检查

使用与 `site/package.json` 兼容的 Node runtime，在仓库根目录运行：

```bash
npm --prefix site ci
npm --prefix site run build
npm --prefix site exec -- playwright install chromium
npm --prefix site run test:browser
```

不需要 `web/` 依赖、数据库、集群或 API 凭据。`site/go.mod` 模块边界避免 Go 递归检查扫描已安装的第三方 Node 依赖，不添加 Go package 或 runtime 服务。构建会重新生成内容，生成静态页面与搜索索引，装配 `site/dist/`，然后检查 operation/schema 完整性及本地链接、锚点。浏览器检查覆盖导航、键盘搜索、代码复制、静态与富交互 API 链接、schema 加载/下载恢复、移动端和主题行为。

完整构建后预览产物：

```bash
npm --prefix site run preview -- --port 18432 --ignore-lock
```

打开 `http://127.0.0.1:18432/orbitjob/`。`--ignore-lock` 让该专用端口上的预览保持前台运行，浏览器套件使用另一个 18433 端口。默认 project base 为 `/orbitjob`；检查其他部署路径时，在整个构建及预览/测试过程一致设置 `SITE_BASE`：

```bash
SITE_BASE=/preview npm --prefix site run build
SITE_BASE=/preview npm --prefix site run test:browser
```

`npm --prefix site run check` 检查已有构建。来源修改后需重新构建，检查旧产物不会更新它。`npm --prefix site run dev` 用于编辑预览，验收以装配后的 production build 为准。

<a id="chinese-wording"></a>

## 维护中文文案

英文页保留原有路径，中文文档从 `/orbitjob/zh-cn/` 进入，对应页面可以切换语言。指南文案在 `site/i18n/zh-CN/guides/` 修改；API 描述在 `site/i18n/zh-CN/api/*.json` 的中文值中修改。自定义导航与页面标签在 `site/src/i18n.ts` 和共享页面模板中修改。`site/src/content/docs/` 中的生成文件会在下次构建时被覆盖。

API 路径、字段名、enum 值、operation ID 和可执行示例保持原样。中文指南构建逐块比较中英文代码块。API 展示文案只翻译描述和标签 metadata；下载的原始契约仍由 `api/openapi.yaml` 渲染为 OpenAPI JSON。缺少 API 译文或修改示例会使文档检查失败。正文也须对照英文来源核查，代码块一致不能证明行为说明一致。调用指南从已翻译的 Usage 章节装配，因此只维护一次文案。改写中文标题时保留显式英文 heading anchor，保证共享链接有效。

<a id="api-contract-maintenance"></a>

## 维护 API 契约

修改 HTTP request、response、parameter 或 permission 后运行：

```bash
make openapi-gen
make openapi-check
npm --prefix web run generate
npm --prefix site run build
npm --prefix site run test:browser
```

修改 Console 契约时需要重新生成前端声明/snapshot，但它不属于文档站构建依赖。不要手工编辑生成 YAML，也不要在 `site/` 维护平行 API 文档。Go 契约回归检查真实 route 注册、operation ID、path/query metadata、引用闭包，并用 domain validation 校验 request example。站点另行检查全部生成 operation/schema 页面。

<a id="github-pages"></a>

## GitHub Pages 发布

Documentation workflow 在 PR 以及 `main`/`dev` 变更时构建。只有 `main` 可以部署，PR 和 `dev` 仅验证。在仓库 **Settings → Pages → Build and deployment → Source** 选择 **GitHub Actions**。部署成功后的默认地址为 `https://s3loy.github.io/orbitjob/`，发布状态以部署 workflow 为准。

Build job 对仓库只有只读权限；只有 deploy job 取得 `pages: write` 和 `id-token: write`。Workflow 上传 `site/dist/` 作为 Pages artifact，不推送生成分支。`.nojekyll` 保证以下划线开头的 Astro 资源可正常提供。Benchmark 成功后可刷新站点，该事件构建最新 `main`，不回退到较旧 benchmark revision。

已有 benchmark 历史保留在 `gh-pages`。若存在 `dev/bench/index.html`，CI 读取 archive 并将其放入 `/orbitjob/dev/bench/`，同时在 benchmark 文档页提供入口。允许没有历史；仓库读取或 fetch 失败会使验证失败，而不是擦除已知历史。需要在本地检查 archive 时，把 `BENCHMARK_ARCHIVE` 指向已有可信的 `dev/bench` 目录再构建。不要传入凭据或不可信 HTML。历史测量保留各自来源和环境；文档构建不产生性能验证结果。
