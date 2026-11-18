# 贡献指南

重大功能或接口变更应先与维护者讨论，说明问题范围、边界情况和验证计划。
外部贡献需要审核时，可以通过 Issue 或 PR 进行讨论。

Issue 和拉取请求使用少量多维标签：`kind/*` 描述工作类型，`area/*` 标识受影响
子系统，`priority/*` 记录维护者排期。拉取请求根据变更路径获得客观的 `area/*`
标签。优先级、安全影响、发布说明资格和破坏性变更始终由维护者判定。

## 开发环境

- Go：使用 [`go.mod`](go.mod) 中的版本；镜像构建阶段见 [`Dockerfile`](Dockerfile)。
- 集成测试使用的 PostgreSQL：在一次性实例上使用
  [Integration 工作流](.github/workflows/integration.yml)中的服务镜像。
- golangci-lint：安装 [Lint 工作流](.github/workflows/lint.yml)固定的版本，
  不要假定最新发布与 CI 一致。
- chart 或 Kubernetes 变更需要 Helm、kind 和 kubectl。
  [Load Test 工作流](.github/workflows/loadtest.yml)记录其 Helm/kind 固定版本；
  [本地指南](docs/local-deployment.md)说明前提和副作用。

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## 分支策略

使用 `dev` 作为共享开发分支。常规维护者工作不要求单独的主题分支或自行提交 PR。
`main` 保持为发布分支。适用质量门禁通过后，发布和晋升到 `main` 由维护者另行
执行。

外部贡献仍可使用主题分支，并向 `dev` 提交 PR。明确需要隔离时，使用简短的
kebab-case 分支名称。推送、合并和发布需要单独的维护者授权。切换分支或集成
工作时，保留现有未提交更改。

## 代码约定

依赖方向：

```text
platform <- core
platform <- admin
core does not import admin
admin/http -> admin/app -> core/domain
```

使用以下命令检查生产代码导入：

```bash
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/core/... \
  | grep -E 'orbitjob/internal/(admin|operator)'
```

预期没有匹配；输出为空时 grep 退出码为 1。


提交的代码必须：

- 使用 context 为首个参数的 API，通过函数变量注入外部依赖
- 保持领域层不依赖 HTTP 类型和 Admin DTO
- 新依赖优先使用标准库，其次复用已有依赖
- 在 Admin API 中使用统一错误结构 `{error:{code,message,field}}`
- 将写入置于事务内；批量认领使用 `FOR UPDATE SKIP LOCKED`
- 对并发更新使用 `version` 乐观锁
- 为新代码路径添加指标、结构化日志或追踪 hook
- 与功能一起实现认证、输入校验和权限检查

## 数据库迁移

文件名使用四位序号：

```text
NNNN_description.up.sql
```

版本必须从 `0001` 起连续。执行器只应用前向迁移，忽略 `.down.sql` 文件；
down 文件不是回滚机制。已发布或已记录在 `schema_migrations` 中的迁移绝不能
修改；执行器通过 SHA-256 校验和检测变化。应添加新的前向迁移，并记录任何
备份恢复或兼容性边界。

编辑 `db/migrations/*.up.sql` 后，同步 Helm chart 副本：

```bash
make helm-migrations-sync
make helm-migrations-check
```

涉及角色、RLS、所有权或 `SECURITY DEFINER` 函数的变更必须说明：

- 哪个 PG 角色获得权限
- RLS 是被绕过还是被执行
- 为何使用 `SECURITY DEFINER` 而非限定租户范围的查询
- 回滚是否可逆

[迁移和兼容性要求](docs/database-setup.md#migration-execution-and-compatibility)
要求在迁移和部署前停止旧写入方。仅执行 Helm 滚动升级不能实现该边界。
请参阅[数据库设置](docs/database-setup.md#writer-cutover)。

## 测试

测试层次由内向外为：

1. 领域单元测试：纯逻辑
2. 用例测试：模拟外部依赖
3. 处理器测试：`httptest`
4. 仓储测试：`go-sqlmock`
5. 集成测试：真实 PostgreSQL，`//go:build integration`
6. 部署验证：Helm 或 kind

`internal/{core,admin}/{domain,app,store}` 下的业务逻辑和仓储包必须达到 60%
语句覆盖率合并门槛。基础设施、适配器、命令和 Operator 包没有数值门槛。
只有声明且没有可执行语句的包记录在 `scripts/coverage-baseline.txt` 中并经过
验证；它们会被报告，但没有有意义的百分比。提交前运行：

```bash
make check
make test-cover-check
```

涉及数据库、仓储、迁移、引导或 RLS 的变更还要求：

```bash
TEST_DATABASE_DSN='postgres://...' make integration
```

integration 目标拒绝空 DSN，并串行运行共享数据库相关包，不使用缓存结果。
不提供 DSN 而直接调用带标签的 Go 测试，仍可能跳过个别测试；这不构成集成证据。

涉及 Helm 或 Kubernetes 的变更要求：

```bash
make helm-check
make kind-verify
```

`kind-verify` 创建或复用 `orbitjob-verify`（可通过 `KIND_CLUSTER_NAME` 覆盖），
切换 kubeconfig 上下文，并修改其数据库和 Helm 发布。它执行升级、迁移失败、
卸载和重新安装路径，因此必须使用一次性集群。脚本创建的集群在成功时移除、
失败时保留；现有集群保留。在 PR 中说明是否实际运行，以及针对哪个检出版本。
Helm 渲染和本地测试不能证明真实安装或升级已通过。

### 前端检查

对于 `web/` 变更，在该目录运行 `npm ci`、`npm run check`、
`npm run format:check` 和 `npm run test:browser`。软件包脚本和锁文件定义
前端工具链。浏览器安装和 API 代理请参阅
[前端开发](docs/local-deployment.md#change-check-and-redeploy)。生成声明遵循
`api/openapi.yaml`；`npm run generate` 更新声明，`types:check` 检测偏移。
演示或模拟 HTTP 的浏览器证据不能替代已安装集群的执行检查。涉及 API、路由或
仓储变更时，也保留 Go 门禁。嵌套的 `web/go.mod` 将 npm 依赖排除在根 Go 包
发现之外。真实集群套件由 `scripts/console-installed.py` 准备和运行，见
[已安装 Console 验证](docs/local-deployment.md#isolated-installation-and-console-checks)。
它使用显式的一次性 kind kubeconfig 和私有凭据文件；禁用 trace 和视频，防止
Authorization 进入测试录制。

### 基准测试

`make bench` 测量台账的纯热点路径：触发标识派生、运行阶段转换、Check 和探测
配置规范化、Kubernetes Job 渲染、终态结果到 SLI 的派生，以及领导者选举和锁。
这些是没有数据库或集群依赖的进程内函数。该目标仅覆盖 Makefile 中
`BENCH_PACKAGES` 列出的包，并跳过测试套件；验证由 `make test` 负责。
运行时长取决于主机和基准测试输入。

只有基准测试进程成功并输出数值测量后，才发布 `bench.txt`。生成方失败、空运行
和陈旧产物不能作为基线。Benchmark 工作流在推送到 `main` 时存储基线，并在
基线存在时比较目标为 `main` 的 PR；目标为 `dev` 的 PR 不会自动获得该比较。

`make bench-etcd-memory` 以更高次数重新运行选举基准测试，用于 benchstat 风格
比较。`make bench-etcd` 针对运行中的 etcd 执行相同操作，需要 `ETCD_ENDPOINTS`
（默认 `localhost:2379`）。

性能敏感路径的变更必须附带基准测试：使用代表性输入进行表驱动测试，并使用
`b.ReportAllocs`。说明排除了哪些依赖成本。当前 `make bench` 目标不测量
数据库查询、已部署集群或端到端吞吐量。

## 本地 hook

提交在本地由 [pre-commit](https://pre-commit.com) hook 把关，根据暂存内容运行
CI 的快速检查。一次性设置：

```bash
pip3 install pre-commit
brew install ggshield
ggshield auth login
make hooks
```

macOS 支持通过 Homebrew 安装 `ggshield`；其他平台可以使用 GitGuardian 文档
中的安装方式。`ggshield auth login` 在开发者本地 GitGuardian 配置中存储个人
访问令牌。绝不能将该令牌放入本仓库或受版本控制的 env 文件。

`make hooks` 运行 `pre-commit install`，安装 pre-commit 和 commit-msg hook。
hook 选择暂存路径，但包 vet、OpenAPI 和 chart 检查可能读取检出目录中其他位置
的依赖。会重写文件的门禁（trailing-whitespace、end-of-file-fixer、
mixed-line-ending、go-fmt）在修复后以非零状态退出：检查更改，执行 `git add`
重新暂存，再次提交。

| 门禁 | 触发条件 | 失败条件 |
|---|---|---|
| trailing-whitespace、end-of-file-fixer、mixed-line-ending | 每次提交 | 可修复的空白、文件末尾换行或行尾问题（已修复；需重新暂存） |
| check-yaml | 每次提交 | YAML 文件无法解析（允许多文档；排除 chart 模板） |
| check-added-large-files | 每次提交 | 暂存文件超过 1 MB |
| check-merge-conflict | 每次提交 | 暂存内容包含冲突标记 |
| ggshield | 暂存更改 | GitGuardian 检测到可能的秘密，或扫描无法完成 |
| go-fmt | 暂存 `*.go` | `gofmt` 重写了暂存文件（已修复；需重新暂存） |
| go-vet | 暂存 `*.go` | `go vet` 报告包含暂存文件的包存在问题 |
| no-cjk | 每次提交 | `README.zh.md` 和 `site/` 以外出现 CJK 字符 |
| no-artifacts | 每次提交 | 暂存文件命中了仓库的 `.gitignore` 规则，包括用 `git add -f` 强制暂存的文件 |
| openapi-drift | 暂存 `api/openapi.yaml`、`internal/admin/http/` 或领域 `create_input.go` | `make openapi-check` 报告 spec 偏移 |
| chart-gate | 暂存 `charts/` | `make helm-check` 失败 |
| commit-msg | 每次提交 | 主题为空、超过 72 个字符，或包含 `WIP` 标记 |

较重的套件保留在 CI 中，包括 lint、单元测试、race、覆盖率、集成套件、基准
比较和镜像构建；流水线说明为 `docs/ci.md`。推送前运行 `make check`，不要
期望 hook 执行每项检查。

维护者可以针对有记录的紧急情况授权绕过 hook，例如从损坏工作树中挑选发布
提交，或 hook 自身故障。代理必须报告失败，不能根据提交权限推断该例外。除非
维护者明确指示具体例外，否则保留 Git 签名和 hook；在提交或 PR 中记录任何
已授权绕过。

## CI

流水线说明位于 `docs/ci.md`，描述各工作流何时运行、证明什么，以及产物位置。
进入 `dev` 的 PR 执行：单元测试、race 和覆盖率、集成套件、lint、govulncheck、
依赖审核、两个发布平台的已配置组件镜像构建（不推送），以及 Console 类型、
lint、格式、单元、浏览器和构建检查。正式发布和预发布还要求在精确源码修订版本
上执行共享验证工作流，包括一次性安装和完整真实 Console 执行。Makefile 和
发布工作流定义组件列表。基准比较在进入 `main` 的 PR 上运行。负载测试为手动
触发的参考性 Operator 证据，从不是必需合并检查。

## OpenAPI

更改 HTTP 路由、请求字段、枚举或错误响应后，更新 `api/openapi.yaml`：

```bash
make openapi-gen
make openapi-check
```

运行时文档位于 `GET /openapi.json`。如果处理器行为暂时无法用 OpenAPI 表达，
在 PR 描述和 `USAGE.md` 中明确标注偏差。

## 文档

维护受行为变更影响的完整英文主题。根据所属实现检查正文、命令、前提、权限和
失败处理。README 介绍项目；[USAGE](USAGE.md)索引任务。主题源位于 `docs/`，
安全策略位于 `SECURITY.md`。

站点将显式主题源映射到结构化路由。主题注册、源链接、生成 HTTP 参考和浏览器
检查遵循[文档工作流](docs/project-site.md)。运行：

```bash
make openapi-check
npm --prefix site run build
npm --prefix site run test:browser
```

使用生产产物进行阅读和导航检查。在适当隔离环境中单独执行受影响的安装、API
或恢复流程；站点构建成功不能证明操作正确。引用变化时，更新随包分发的工作
负载助手包。

公开流程必须能够从分发源码树使用，不依赖内部计划、决策记录或原始日志。文档
和注释使用英文。在英文变更时，同时维护 `site/i18n/zh-CN/topics/` 下经审核的
中文对应主题及当前 API 词典。更新翻译审核基线前，检查语义是否等价；哈希不
执行语义审核。

README 不记录版本路线图。需求与当前实现和验证记录分别保留各自用途。

## 提交格式

只有维护者明确请求时，代理才创建本地提交；普通编辑保持未提交。提交请求包含
该范围内所需暂存，但不授权推送或发布。

Conventional Commits：

```text
type(scope): description
```

描述使用小写英文，不带末尾句号；整个主题最多 72 个字符。常用类型：`feat`、
`fix`、`refactor`、`test`、`docs`、`chore`、`style`、`perf`。

| 范围 | 用途 |
|---|---|
| `core/domain` | 领域规则 |
| `core/store` | 核心写入存储 |
| `core/app` | 核心用例 |
| `admin/http` | HTTP 处理器和中间件 |
| `admin/store` | Admin 读取存储 |
| `admin/app` | Admin 用例 |
| `schedule` | Scheduler 进程 |
| `operator` | Operator 进程和 CRD 协调器 |
| `check` | Check 和 CheckRun |
| `slo` | SLI、SLO 和错误预算 |
| `bootstrap` | 首次初始化 |
| `deploy` | Docker 镜像、Helm 和 kind |
| `platform` | 迁移、协调层、基础设施 |
| `ci` | CI/CD |
| `docs` | 文档 |

不要将无关功能合并为一个提交，也不要将一个逻辑单元拆成逐文件提交。

## PR 描述

使用 PR 时，以 `dev` 为目标分支。面向审核者写作，不将其写成发布说明：先说明
变更原因，再解释变化的行为或设计。小变更可能只需一句。较大变更应说明因果、
取舍、迁移影响、已知限制，以及提供必要上下文的 Issue、ADR 或先前 PR。

不要强制使用固定章节结构。避免枚举文件、提交或 diff 中的每项功能，也不要
重复 PR 中可见的 CI 检查。只有测试、基准、手动检查、跳过测试或失败实质上
有助于审核时才提及。仅在标题或列表有助于阅读长描述时使用。

破坏性变更、不可逆迁移、Secret 键名和升级顺序应写在 PR 正文中，不能只写在
审核评论里。PR 解决 Issue 时使用 `Fixes #issue`。作者对每项陈述负责；代理
协助的草稿必须在提交前验证，并用作者自己的表达重写。

## 行为准则

保持专业和友善。遵循 [Contributor Covenant](https://www.contributor-covenant.org/)。
