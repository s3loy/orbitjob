---
title: "负载测试"
---

负载测试工具有独立指南：[`loadtest-guide.md`](/orbitjob/docs/load-testing/profiles/)。

配置名称为 `smoke`、`standard` 和 `long`。配置变体可以使用不同文件名；内部 `profile` 必须与命令标志匹配：

```bash
make loadtest-smoke    # validates the smoke preflight; does not run its 30m schedule
make loadtest-long     # validates the long preflight; does not run its 8h schedule
```

`make loadtest-smoke` 验证配置、镜像锁与本地 Docker 容量，不执行工作负载，也不修改集群：

```
preflight: profile=smoke Docker capacity >= 6 CPU / 8 GiB
preflight: configuration and image lock valid
```

配置位于 `test/load/config/`（`smoke`、`smoke-faults`、`standard`、`standard-dynamic`、`long`）；工具为每次运行生成并持久保存四个租户 ULID。`make loadtest-full` 执行预检和生成，然后输出人工审阅提示；它不会执行资格验证。配置与资格验证运行见 [`loadtest-guide.md`](/orbitjob/docs/load-testing/profiles/)。
