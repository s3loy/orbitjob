# CI 流水线说明

所有 GitHub Actions 工作流位于 [`.github/workflows/`](../.github/workflows/)。
请在 GitHub 上查看运行结果并确认分支规则的执行情况。负载测试是手动触发的参考性
操作，不在 PR 门禁链中。

Go 设置步骤使用 `go-version-file` 从 [go.mod](../go.mod) 读取工具链版本；
CI、基准和负载测试不单独保存版本常量。Govulncheck 同时检查可达模块漏洞及
所选工具链中的标准库。仅更新模块版本不能解决标准库问题。扫描结果属于执行时
的提交、工具链、扫描器和漏洞数据库。

## 运行时机

| 工作流 | 触发条件 | 检查 | 输出 |
|---|---|---|---|
| CI（`ci.yml`） | 推送、PR → `main`/`dev` | 单元与 race 测试、每包 60% 覆盖率门槛、OpenAPI 时效性、Console 验证、`make build-all`、`linux/amd64` **和** `linux/arm64` 的已配置镜像目标（不推送）；`CI / Gate` 要求此链成功 | `binaries` 产物（7 天）、Codecov 上传 |
| Documentation（`docs.yml`） | 选择 `main` 后手动触发 | 验证所选提交并部署至 GitHub Pages | 已验证站点产物（1 天）、Pages 产物、浏览器失败诊断（7 天） |
| Documentation verification（`docs-verification.yml`） | 手动部署至 GitHub Pages 或正式发布时，以一个源码提交调用 | OpenAPI 时效性、英文和中文主题及 API 构建、链接、本地化搜索和浏览器可用性 | 浏览器失败诊断（7 天），以及部署工作流按需请求的已验证站点产物 |
| Console（`console.yml`） | 由 CI 和发布验证调用 | 锁定依赖、生成契约、TypeScript、ESLint、单元测试、生产构建、格式以及 fixture 和模拟 HTTP 浏览器测试 | 失败诊断（7 天） |
| Integration（`integration.yml`） | 推送、PR → `main`/`dev`、手动、发布验证 | 八个包：迁移、Postgres 辅助函数、Admin/Core 仓储、引导、Admin HTTP、负载工具隔离和已安装 fixture 的凭据退役，使用工作流 PostgreSQL 服务镜像和已配置部署角色 | — |
| Lint（`lint.yml`） | 推送、PR → `main`/`dev`、手动 | golangci-lint | — |
| Govulncheck（`govulncheck.yml`） | 推送、PR → `main`/`dev`、手动、发布验证 | 使用运行时配置的扫描器和数据库，检查 `linux/amd64` 和 `linux/arm64` 的可达漏洞 | — |
| Dependency Review（`dependency-review.yml`） | PR → `main`/`dev` | 根据 action 的高危失败阈值检查依赖 diff | — |
| Benchmark（`benchmark.yml`） | 推送、PR → `main` | 推送按提交存储 `bench.txt`；PR 发布比较评论 | 基准历史（由工作流提交） |
| Load Test（`loadtest.yml`） | 手动 | 安装 kind 并尝试采集参考性端到端证据；工作流成功不是验收结论 | `loadtest-<profile>-<run_id>` 产物（14 天）及运行页面作业摘要 |
| Release verification（`verification.yml`） | 由正式发布和预发布以一个源码提交调用 | 检查后端质量、覆盖率、Helm、PostgreSQL、漏洞和 Console，正式发布时还检查文档。随后执行安装生命周期及完整已安装 Console 测试 | 可安全公开的已安装验证回执，正式发布的文档浏览器失败诊断（7 天） |
| Pre-release Images（`prerelease.yml`） | 推送到 `dev`、手动 | 共享发布验证，随后将配置的组件矩阵以 `rc-<run>-<sha>` 推送到 GHCR；不创建仓库标签 | 带版本的预发布镜像和可变 `rc` 别名 |
| Release（`release.yml`） | 标签 `v*` | 检查标签与 chart 版本一致，要求标签解析为配置默认分支的当前提交，且版本标签未被使用；完成共享发布验证，发布带版本镜像并打包 chart；不提供 `latest` 别名 | `helm-chart` 产物（90 天）、已发布镜像 |

[Makefile](../Makefile) 中的 `DOCKER_COMPONENTS` 定义 PR 构建和
`make docker-build` 的镜像目标，包括执行日志归档。正式发布和预发布工作流
定义各自发布矩阵；组件变更必须保持这些矩阵与 chart 一致。镜像构建、安装验证
和镜像仓库发布是独立步骤。

发布验证的每个必需作业均检出同一提交。正式发布的 Documentation verification
使用只读仓库权限，并确认检出结果与请求的完整提交 SHA 一致。OpenAPI、构建和
浏览器检查全部通过后，安装作业才会启动。任何一项失败都会阻止正式版镜像的发布。
从 `main` 手动部署 GitHub Pages 时，Documentation 工作流执行相同的检查。

[Cloudflare Pages Git 集成](project-site.md#cloudflare-pages)自动从 `main`
构建并部署站点，并从 `dev` 发布预览。Cloudflare 独立于 GitHub Actions 执行
站点构建和制品检查。两个 Pages 平台在构建时导入已有的基准归档。

安装作业创建隔离 kind 集群和 kubeconfig，
运行安装生命周期门禁，再针对该安装准备并运行全部已安装 Console 用例。执行器
拒绝跳过、重试或不完整结果。即使失败，清理也会停用 fixture 凭据并移除自有
集群。不上传凭据或原始请求响应日志。受控业务恢复和旧版本升级演练仍属于额外
发布证据；全新安装生命周期门禁不能证明这些行为。

全部带版本镜像发布后，可变 `rc` 别名逐组件更新。中断可能留下混合别名。安装
实例应使用显式 `rc-<run>-<sha>` 运行标签，并记录每个镜像摘要。重新运行该
工作流可能再次发布相同运行标签；安装必须复现精确已验证产物时，使用记录的摘要。

PR 检查上下文为 `CI / Gate`、`postgres-integration`、`golangci-lint`、
`govulncheck` 和 `dependency-review`。哪些检查为必需项，由远端仓库或规则集
设置决定，不能由这些工作流文件确认。`CI / Gate` 仅汇总 `ci.yml` 中的依赖链，
不包括独立 Integration、Lint 或安全工作流。基准比较属于审核证据。推送事件
使用独立的 `*-post-merge` 检查名称，包括 `CI / Post-merge`，因此 dev 推送
成功不能满足同一提交所需的拉取请求上下文。

拉取请求通过 `.github/labeler.yml` 根据变更路径打标签。路径规则仅分配客观
`area/*` 标签。优先级、安全影响、发布说明资格和破坏性变更标签需要维护者判断。

托管负载工作流仅提供 `smoke` 和 `standard`，作业预算分别为 90 和 350 分钟。
它不提供八小时 `long` 配置；运行该计划需要单独管理的隔离主机。执行和证据
限制请参阅[负载指南](loadtest-guide.md)。

执行器的收敛等待循环使用当前运行已保存的租户和修订版本映射调用
`loadtest settle`。每次读取设置事务局部租户 RLS 上下文；查询失败和观测缺失
保持未知，绝不作为零计数。等待耗尽会产生警告，并继续独立验证；仅等待收敛
不能确定所有 CR 已完成，也不产生验收结论。

Load Test 工作流成功表示测试工具步骤成功。即使产品结论为 `FAIL` 或
`INCONCLUSIVE`，验证器仍写入 `result.json` 并返回成功；应阅读该结论和各项
检查。独立恢复证据检查要求已准备的 canary、来源观测和连续 fixture 日志；缺失
或矛盾观测保持 `INCONCLUSIVE`。仅副本就绪不能产生验收 `PASS`。已执行检查
请参阅[验证记录](verification.md)，验收缺口请参阅[已知问题](known-issues.md)。

## 手动触发

```bash
gh workflow run loadtest.yml -f profile=standard   # ad-hoc load profile
gh workflow run integration.yml                    # re-run integration suites
gh workflow run docs.yml --ref main                # verify and deploy GitHub Pages
```

Actions 界面提供相同的手动触发。正式发布只由标签推送触发。任一发布镜像存在后，
版本即被占用，因此部分发布失败必须使用新版本，不能覆盖现有标签。

## 产物

所有产物附加在工作流运行页面（**Actions → run → Artifacts**）。`loadtest`
运行还提供作业摘要，包含配置、结论、时长和产物链接，并嵌入该次运行的完整报告。
保留时间：二进制 7 天、负载测试运行 14 天、打包 chart 90 天。

`binaries` 产物包含 `make build-all` 构建的 Linux/amd64 运行时命令、配置
工具和 CLI。它是 CI 产物，不是可移植的多平台 CLI 发布归档。发布工作流发布
容器镜像并上传打包 Helm chart，不创建 GitHub Release。

## 本地对应命令

| CI 执行 | 本地执行 |
|---|---|
| `make test`、`make test-race`、`make test-cover-check`（60% 门槛） | 相同 |
| 集成套件 | `TEST_DATABASE_DSN='postgres://...' make integration` |
| `make openapi-check` | 相同；提交 schema 变更前，使用 `make openapi-gen` 主动重新生成 |
| golangci-lint | 使用 `lint.yml` 固定版本运行 `make lint` |
| 组件镜像构建（amd64 + arm64） | `make docker-build`（仅本机架构） |
| 基准存储和比较 | `make bench` |
| Console 验证 | `npm --prefix web ci`、`npm --prefix web run check`、`npm --prefix web run format:check`、`npm --prefix web run test:browser` |
| 安装与真实 Console | `make kind-verify`，再在同一一次性安装中执行[已安装 Console 验证](local-deployment.md#isolated-installation-and-console-checks) |
| 负载配置 | 隔离设置和命令见[负载测试](loadtest-guide.md) |

`make check` 组合 lint、vet、race 测试、OpenAPI 时效性和 `go mod tidy -diff`。
tidy 检查不修改模块文件，也不要求干净 Git 工作树。覆盖率、带标签集成和 Helm
检查是独立目标。常规 PR CI 链不将每个 `make check` 前提作为独立作业调用；
正式发布和预发布使用共享验证工作流，并要求其全部作业完成后才发布镜像。

`make integration` 使用 `-count=1` 串行运行相同八个包。工作流调用同一目标。
空 `TEST_DATABASE_DSN` 会在 Go 启动前失败，因此跳过数据库套件不能满足该
目标。将变量设为一次性 PostgreSQL 服务：部分套件会重建 `public`，受管理的
登录角色则在整个服务端范围生效。请参阅
[本地部署](local-deployment.md#change-check-and-redeploy)。本地负载执行器在
生成新运行前，会重置目标安装实例上全部由工具管理的负载租户和命名空间；调用
前应检查该范围。

`make bench` 传播基准测试进程退出状态，并要求至少一个数值测量。它先移除
陈旧 `bench.txt`，仅在成功且产生测量的运行后发布新文件；空输出和部分失败
不能进入基准历史或比较。

负载摘要绑定到执行器在设置前输出的本次尝试运行目录。早期设置或验证器失败
不能使用先前运行的 PASS 结果。手动触发回执逐个与投影台账行核对；拒绝、跳过
和投影缺失均保持 `INCONCLUSIVE`。工作负载启动时记录执行配置和来源身份，
详情见[负载指南](loadtest-guide.md)。
