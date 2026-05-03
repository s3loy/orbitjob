# 贡献指南

欢迎贡献。在提交 PR 之前，请先开 Issue 讨论你想做的改动——避免白费功夫。

## 开发环境

- Go 1.26+
- PostgreSQL 17
- golangci-lint v2.11+

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## 分支策略

从 `main` 分出，合回 `main`。命名格式：`feat|fix|refactor|chore/<描述>`。一个分支做一件事。

合入方式为 merge commit（不 squash，不 rebase merge）。

## 代码规范

- 与项目现有模式保持一致
- Context-first，依赖注入通过函数变量
- 新依赖：标准库 > 已有依赖 > 评估后提 Issue 讨论
- Domain 类型不引用 HTTP 结构

## 测试

分层测试，从内到外：

1. Domain 单元测试（纯逻辑，无 mock）
2. Use case 测试（mock 依赖）
3. Handler 测试（httptest）
4. Repository 测试（go-sqlmock）
5. 集成测试（真实 PostgreSQL，`//go:build integration` tag）

含业务逻辑的包目标 100% 覆盖率。

提交前跑：

```bash
golangci-lint run
go build ./...
go vet ./...
go test -race ./...
```

## Commit 格式

`type(scope): description`，Conventional Commits。类型：feat/fix/refactor/test/docs/chore。scope 见 CLAUDE.md。

## PR 流程

1. 开 Issue 描述问题和方案
2. 创建分支实现
3. 确保 CI 全部通过
4. 提交 PR，描述 Summary / What changed / How to run
5. 至少一次 review 后合入

## 行为准则

保持专业和友善。我们遵循 [Contributor Covenant](https://www.contributor-covenant.org/)。
