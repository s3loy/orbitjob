---
title: "开发验证"
---

<a id="16-development-verification"></a>

```bash
make test
make test-cover
make test-race
TEST_DATABASE_DSN='<dedicated disposable database DSN>' make integration
make check
make helm-check
```

`make check` 运行 golangci-lint、`go vet`、race test、OpenAPI 同步检查与 `go mod tidy` 检查。

`make integration` 不创建 PostgreSQL，必须提供专用 `TEST_DATABASE_DSN`；测试可能重建 schema、配置角色并修改数据。绝不能使用已安装环境的数据库。准确质量检查要求见 [CONTRIBUTING.md](https://github.com/s3loy/orbitjob/blob/main/CONTRIBUTING.md)；已执行检查及集群／负载边界见[验证记录](/orbitjob/docs/verification/)。
