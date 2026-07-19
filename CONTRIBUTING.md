# 贡献指南

欢迎参与 OrbitJob 开发。提交重大功能或接口变更前，请先创建 Issue，说明问题范围、边界条件与验证方案。小型修复可直接提交 PR。

## 开发环境

- Go 1.26.5
- PostgreSQL 17
- golangci-lint v2.11.3
- Docker Compose v2（部署相关变更）
- Helm 3、kind、kubectl（Chart 或 Kubernetes 变更）

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## 分支策略

从 `dev` 分支创建开发分支，PR 合回 `dev`。维护者按发布节奏将 `dev` 合入 `main`。

分支命名规范：

```text
feat/<description>
fix/<description>
refactor/<description>
chore/<description>
```

使用 kebab-case，一个分支聚焦一个主题。

## 代码规范

依赖方向：

```text
platform <- core
platform <- admin
core 不导入 admin
admin/http -> admin/app -> core/domain
```

提交代码需满足：

- Context-first API，通过函数变量注入外部依赖
- Domain 层不引用 HTTP 类型或 admin DTO
- 新依赖优先选择标准库，其次复用已有依赖
- Admin API 错误使用统一结构 `{error:{code,message,field}}`
- 写操作放入事务；批量抢占使用 `FOR UPDATE SKIP LOCKED`
- 并发更新使用 `version` 乐观锁
- 新代码路径同步增加 metrics、结构化日志或 trace 接入点
- 认证、输入校验与权限检查随功能同步提交

## 数据库 Migration

文件名使用四位序号：

```text
NNNN_description.up.sql
NNNN_description.down.sql
```

不可逆 migration 的 down 文件名添加 `_irreversible` 后缀，并在文件内说明原因。已发布或已写入 `schema_migrations` 的 migration 不可再修改；runner 通过 SHA-256 checksum 检测变更。

修改 `db/migrations/*.up.sql` 后同步 Helm Chart 副本：

```bash
make helm-migrations-sync
make helm-migrations-check
```

涉及 role、RLS、ownership 或 `SECURITY DEFINER` 函数的变更需说明：

- 哪个 PG role 获得权限
- 是否绕过或受 RLS 约束
- 使用 `SECURITY DEFINER` 而非 tenant-scoped query 的原因
- rollback 是否可逆

## 测试

测试分层从内到外：

1. Domain 单元测试：纯逻辑
2. Use case 测试：mock 外部依赖
3. Handler 测试：`httptest`
4. Repository 测试：`go-sqlmock`
5. 集成测试：真实 PostgreSQL，`//go:build integration`
6. 部署验证：Compose、Helm 或 kind

含业务逻辑的 Go 包以 100% 语句覆盖率为合入目标。提交前运行：

```bash
make check
make test-cover
```

涉及数据库、repository、migration、bootstrap 或 RLS 的变更还需运行：

```bash
TEST_DATABASE_DSN='postgres://...' make integration
```

涉及 Helm 或 Kubernetes 的变更运行：

```bash
make helm-check
make kind-v020-verify
```

`kind-v020-verify` 会创建本地 kind 集群，耗时高于单元测试。PR 中需注明是否实际运行；不可将"未运行"标记为"通过"。

性能敏感路径需附带 benchmark：

```bash
make bench
```

## OpenAPI

HTTP route、请求字段、enum 或错误响应变更后需更新 `api/openapi.yaml`：

```bash
make openapi-gen
make openapi-check
```

运行时文档位于 `GET /openapi.json`。若 handler 行为暂时无法由 OpenAPI 表达，需在 PR 描述和 `USAGE.md` 中明确标注差异。

## 文档

代码与文档在同一 PR 中更新：

- 项目定位与启动方式：`README.md`、`README.en.md`
- 操作步骤与 curl 示例：`USAGE.md`
- 安全边界：`SECURITY.md`
- 接口 schema：`api/openapi.yaml`

README 不记录版本路线。路线与架构计划记录在 Issue、PR 或维护者指定的设计文档中。

## Commit 格式

使用 Conventional Commits：

```text
type(scope): description
```

描述使用英文小写，不加句号，不超过 72 字符。常用 type：`feat`、`fix`、`refactor`、`test`、`docs`、`chore`、`style`、`perf`。

| Scope | 用途 |
|---|---|
| `core/domain` | 领域规则 |
| `core/store` | 核心写库 |
| `core/app` | 核心用例 |
| `admin/http` | HTTP handler 与 middleware |
| `admin/store` | Admin 读库 |
| `admin/app` | Admin 用例 |
| `schedule` | scheduler 进程 |
| `dispatcher` | dispatcher 进程 |
| `worker` | worker 与 handler |
| `check` | Check 与 CheckRun |
| `slo` | SLI、SLO 与错误预算 |
| `bootstrap` | 首次初始化 |
| `deploy` | Docker、Compose、Helm 与 kind |
| `platform` | migration、协调层与基础设施 |
| `ci` | CI/CD |
| `docs` | 文档 |

不可将无关功能合并到同一 commit，也不可将一个逻辑单元拆分为逐文件的多个 commit。

## PR 内容

PR 目标分支为 `dev`。描述至少包含：

```markdown
## Summary
- 解决的问题
- 用户或运维侧的变更

## Changes
- 主要代码、数据库、部署变更

## Security and migration notes
- role/RLS/secret/rollback 影响；无则写 None

## Verification
- [x] make check
- [x] make test-cover
- [ ] make integration（注明未运行原因）
- [ ] make helm-check / make kind-v020-verify（按变更选择）
```

不兼容变更、不可逆 migration、Secret key 名称和升级顺序需写入 PR 正文，不可仅放在 review comment 中。

## 行为准则

保持专业与友善。本项目遵循 [Contributor Covenant](https://www.contributor-covenant.org/)。
