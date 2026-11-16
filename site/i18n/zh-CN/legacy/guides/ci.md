---
title: "持续集成"
---

所有 workflow 位于 `.github/workflows/`。下文描述 PR 与 push 配置，不证明 GitHub 实际结果或远端 branch rule 已生效。压测是独立、手动触发的参考验证，不属于 PR 质量门。

Go 配置步骤通过 `go-version-file` 读取 `go.mod` 的工具链版本；CI、基准测试和压测不分别维护版本常量。Govulncheck 同时检查可达的模块漏洞与所选工具链的标准库漏洞；只升级模块不能修复标准库问题。扫描结果对应执行时的 commit、工具链、扫描器和漏洞库。

<a id="what-runs-when"></a>

## 运行时机与验证范围

| Workflow | 触发方式 | 验证内容 | 产出 |
|---|---|---|---|
| CI（`ci.yml`） | push、PR → `main`/`dev` | 单元与 race 测试；各 package 60% 覆盖率；OpenAPI 一致性；`make build-all`；已配置镜像目标的 `linux/amd64`、`linux/arm64` 构建，不推送；`CI / Gate` 汇总此依赖链 | `binaries` artifact（7 天）、Codecov 上传 |
| Integration（`integration.yml`） | push、PR → `main`/`dev`、手动 | 在 workflow 指定 PostgreSQL image 上运行七组测试：migration、Postgres helper、admin/core repository、bootstrap、admin HTTP、load-tool isolation，并配置部署角色 | — |
| Lint（`lint.yml`） | push、PR → `main`/`dev`、手动 | golangci-lint | — |
| Govulncheck（`govulncheck.yml`） | push、PR → `main`/`dev`、手动 | 按执行时 scanner 与漏洞库报告可达漏洞 | — |
| Dependency Review（`dependency-review.yml`） | PR → `main`/`dev` | 依赖变化未引入高危漏洞 | — |
| Benchmark（`benchmark.yml`） | push、PR → `main` | push 保存各 commit 的 `bench.txt`；PR 发布对比评论 | 由 workflow 提交的 benchmark 历史 |
| Documentation（`docs.yml`） | push/PR → `main`/`dev`、手动、main Benchmark 成功完成 | 完整静态 API/schema 页面、从源码生成的指南、搜索与浏览器可用性；仅 `main` 部署 | `github-pages` artifact；保留已有 benchmark 历史 |
| Load Test（`loadtest.yml`） | 手动 | 安装 kind 并尝试收集端到端参考证据；workflow 成功不代表 qualification verdict | `loadtest-<profile>-<run_id>` artifact（14 天）及运行页 job summary |
| Pre-release Images（`prerelease.yml`） | push 到 `dev`、手动 | 通过质量门后，将配置组件矩阵中的镜像推送到 GHCR，tag 为 `rc-<run>-<sha>`；全部成功后更新 `rc` 别名，不创建仓库 tag | 发布的 `rc` 镜像 |
| Release（`release.yml`） | tag `v*` | 要求 tag 与 chart version 一致，指向配置的 default branch 当前 commit，版本镜像 tag 均未使用；运行质量与 Helm 检查并发布版本镜像、chart，无 latest 别名 | `helm-chart` artifact（90 天）、发布镜像 |

Makefile 的 `DOCKER_COMPONENTS` 定义 PR 构建和 `make docker-build` 的镜像目标，包含执行日志归档组件。Release/prerelease workflow 定义发布矩阵；组件变化时须同步矩阵和 chart。镜像构建、安装验证与镜像仓库发布是独立步骤。

PR context 为 `CI / Gate`、`postgres-integration`、`golangci-lint`、`govulncheck`、`dependency-review`。哪些必过由远端 ruleset 决定。`CI / Gate` 只汇总 ci.yml 依赖链，不包含独立 integration/lint/security workflow；benchmark 是 review evidence。Push 使用单独的 post-merge context，不能替代同 commit 的 PR context。自动 label 只按路径分配 area，优先级、安全、release note 和 breaking change 由维护者判断。

Hosted load workflow 仅提供 smoke/standard，job budget 分别为 90/350 分钟，不提供八小时 long；后者需要独立、隔离且持续运行的主机，见 `docs/loadtest-guide.md`。

Settle 使用本次 tenant/revision mapping，每次读设置事务局部 RLS；查询失败或观察缺失保留 unknown，不算零。等待耗尽后仍做独立 verify，settle 不证明所有 CR 已完成。

Load Test workflow 成功说明工具步骤完成。Verifier 即使产品结论为 `FAIL` 或 `INCONCLUSIVE` 也会成功退出，必须读取 `result.json` 的结论与各项检查。当前没有独立业务恢复探针，故障后就绪不能产生资格验证 `PASS`。已执行检查见[验证记录](/orbitjob/docs/verification/)，资格验证缺口见[已知问题](/orbitjob/docs/known-issues/)。

<a id="manual-triggers"></a>

## 手动触发

```bash
gh workflow run loadtest.yml -f profile=standard   # ad-hoc load profile
gh workflow run integration.yml                    # re-run integration suites
```

Actions UI 也提供相同入口。正式 release 只能由 tag push 触发。只要某个 release 镜像已存在，该版本就算已使用；部分发布失败时，应换用新版本，不能覆盖原 tag。

<a id="artifacts"></a>

## Artifact

所有 artifact 均附在 workflow 运行页（**Actions → run → Artifacts**）。`loadtest` 还会生成包含 profile、verdict、运行时长和 artifact 链接的 job summary，并嵌入完整报告。保留时间：二进制 7 天、压测运行 14 天、打包 chart 90 天。

<a id="local-equivalents"></a>

## 本地对应命令

| CI 操作 | 本地对应操作 |
|---|---|
| `make test`、`make test-race`、`make test-cover-check`（60%） | 相同 |
| Integration suites | `TEST_DATABASE_DSN='postgres://...' make integration` |
| `make openapi-check` | 相同；契约改变后有意执行 `make openapi-gen` |
| golangci-lint | 按 lint.yml pin 使用 `make lint` |
| 组件镜像双架构构建 | `make docker-build`，只构建主机架构 |
| Benchmark 保存与对比 | `make bench` |
| 文档构建与可用性 | `npm --prefix site run build` 后运行 `npm --prefix site run test:browser`；准备与发布见 [project-site.md](/orbitjob/docs/project-site/) |
| 压测档位 | `scripts/quickstart.sh` 后运行 `scripts/loadtest-ci.sh smoke`，访问真实 kind 集群 |

`make check` 组合 lint、vet、race、OpenAPI freshness 与 `go mod tidy -diff`，tidy 不修改模块或要求 clean Git tree。Coverage、tagged integration、Helm 是独立 target；release/prerelease 显式执行 make check。

`make integration` 顺序、无 cache 运行相同七组 package；空 DSN 在 Go 启动前失败。使用独立 PostgreSQL：测试可重建 public，登录 role 是 server-wide。Load runner 会先 reset 目标安装中全部工具自有 tenant/namespace，执行前须复查范围。

`make bench` 传递进程退出码，先删除旧 bench.txt，只有成功且有 numeric measurement 才发布新文件。失败、空运行或部分输出不能作为 baseline。

Load summary 绑定 setup 前记录的 attempted run directory，不借旧 PASS。独立 trigger receipt 与 ledger 一一核对，reject/skip/missing projection 保持 INCONCLUSIVE；执行配置和 source identity 在开始时保留，详见[压测验证](/orbitjob/docs/load-testing/running/#5-verify)。
