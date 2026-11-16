---
title: "重置"
---

```bash
go run ./scripts/loadtest reset --confirm
```

只删除同时具有 `load-` slug 和 prepare 写入的 `orbitjob.io/managed-by=orbitjob-loadtest` 元数据标记的租户。在同一数据库事务内，先删除依赖历史、审计行与 SLI 去重事实，再删除这些租户。未标记租户继续保留以供明确的人工审阅，包括没有归属元数据的旧 fixture。仅有名称或 ID 不能证明归属。

重置先删除带标签的负载命名空间并等待其工作负载停止，再清理所拥有的数据库行。共用 fixture 基础设施与 `monitoring` 命名空间仍由各自安装和删除命令管理。

Operator Deployment 的命名空间表保留已删除命名空间的条目。这些条目无实际影响，因为不存在的命名空间不会被调度工作。准备同一个尚未启动的运行时，会复用持久化租户身份；新运行会为其新租户增加映射。

此操作不修改 `test/load/runs/`。使用 `clean --run-id "$RUN_ID" --confirm` 仅删除该运行的本地产物，或在不再需要这些存储时手动删除目录。
