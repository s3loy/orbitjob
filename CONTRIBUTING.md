# 贡献指南

欢迎贡献。提交较大功能或接口变更前，请先开 Issue，写清问题、边界和验证方式。小型修复可以直接提交 PR。

## 开发环境

- Go 1.26.5
- PostgreSQL 17
- golangci-lint v2.11.3
- Docker Compose v2（部署相关改动）
- Helm 3、kind、kubectl（Chart 或 Kubernetes 改动）

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## 分支策略

从 `dev` 创建工作分支，PR 合回 `dev`。维护者再按发布节奏把 `dev` 合入 `main`。

分支名：

```text
feat/<description>
fix/<description>
refactor/<description>
chore/<description>
```

使用 kebab-case。一个分支处理一个主题。

## 代码边界

依赖方向：

```text
platform <- core
platform <- admin
core 不导入 admin
admin/http -> admin/app -> core/domain
```

提交代码时遵守：

- 使用 Context-first API，通过函数变量注入外部依赖
- Domain 不引用 HTTP 类型或 admin DTO
- 新依赖优先选择标准库，其次复用现有依赖
- Admin API 错误使用 `{error:{code,message,field}}`
- 写操作放进事务；批量抢占使用 `FOR UPDATE SKIP LOCKED`
- 并发更新使用 `version` 乐观锁
- 新执行路径同步增加 metrics、结构化日志或 trace 接入点
- 认证、输入校验和权限检查随功能一起提交

## 数据库 migration

文件名使用四位序号：

```text
NNNN_description.up.sql
NNNN_description.down.sql
```

不可逆 migration 的 down 文件名加 `_irreversible`，并在文件内解释原因。不要修改已经发布或写入 `schema_migrations` 的 migration；runner 会检查 SHA-256 checksum。

修改 `db/migrations/*.up.sql` 后同步 Helm Chart 副本：

```bash
make helm-migrations-sync
make helm-migrations-check
```

role、RLS、ownership 或 `SECURITY DEFINER` 函数变更需要说明：

- 哪个 PG role 获得权限
- 是否绕过或受 RLS 约束
- 为什么不能使用普通 tenant-scoped query
- rollback 是否可逆

## 测试

测试从内到外分层：

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

数据库、repository、migration、bootstrap 或 RLS 改动还要运行：

```bash
TEST_DATABASE_DSN='postgres://...' make integration
```

Helm 或 Kubernetes 改动运行：

```bash
make helm-check
make kind-v020-verify
```

`kind-v020-verify` 会创建本地集群，耗时高于单元测试。PR 中写明是否实际运行；不要把“未运行”写成“通过”。

性能敏感路径需要 benchmark：

```bash
make bench
```

## OpenAPI

HTTP route、请求字段、enum 或错误响应变化需要更新 `api/openapi.yaml`：

```bash
make openapi-gen
make openapi-check
```

运行时文档位于 `GET /openapi.json`。如果 handler 行为暂时无法由 OpenAPI 表达，请在 PR 和 `USAGE.md` 中明确写出差异。

## 文档

代码和文档在同一个 PR 更新：

- 项目定位、启动方式：`README.md`、`README.en.md`
- 完整操作步骤和 curl 示例：`USAGE.md`
- 安全边界：`SECURITY.md`
- 接口 schema：`api/openapi.yaml`

README 不记录版本路线。路线和架构计划放在 Issue、PR 或维护者指定的设计文档中。

## Commit 格式

使用 Conventional Commits：

```text
type(scope): description
```

描述使用英文小写，不加句号，不超过 72 个字符。常用 type：`feat`、`fix`、`refactor`、`test`、`docs`、`chore`、`style`、`perf`。

| Scope | 用途 |
|---|---|
| `core/domain` | 领域规则 |
| `core/store` | 核心写库 |
| `core/app` | 核心用例 |
| `admin/http` | HTTP handler 与 middleware |
| `admin/store` | Admin 读库 |
| `admin/app` | Admin 用例 |
| `schedule` | scheduler |
| `dispatcher` | dispatcher |
| `worker` | worker 与 handler |
| `check` | Check 与 CheckRun |
| `slo` | SLI、SLO 与错误预算 |
| `bootstrap` | 首次初始化 |
| `deploy` | Docker、Compose、Helm 与 kind |
| `platform` | migration、协调层与基础设施 |
| `ci` | CI/CD |
| `docs` | 文档 |

不要把互不相关的功能塞进一个 commit，也不要把一个逻辑单元拆成逐文件 commit。

## PR 内容

PR 目标分支选 `dev`。描述至少包含：

```markdown
## Summary
- 解决什么问题
- 用户或运维行为发生什么变化

## Changes
- 主要代码、数据库和部署改动

## Security and migration notes
- role/RLS/secret/rollback 影响；无则写 None

## Verification
- [x] make check
- [x] make test-cover
- [ ] make integration（说明未运行原因）
- [ ] make helm-check / make kind-v020-verify（按改动选择）
```

把不兼容变更、不可逆 migration、Secret key 名称和升级顺序放在 PR 正文，不要只留在 review comment。

## 行为准则

保持专业和友善。我们遵循 [Contributor Covenant](https://www.contributor-covenant.org/)。
