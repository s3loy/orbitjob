---
title: "ScheduledJob 参考与执行语义"
---

<a id="11-scheduledjob-reference-and-execution-semantics"></a>

- 每次执行由定义的 `jobTemplate` 渲染成 `batch/v1` Job，在该租户映射的 namespace 中运行。Pod spec 禁用 service-account token 挂载；chart 安装后新建的 namespace 也遵循此规则。
- 平台负责重试：执行次数记录在账本（`attempt`/`max_attempts`）中，`backoffLimit` 交由 Kubernetes 控制同一个 Job 内的 Pod 重试。
- 历史行为：进程内的 `exec`/`http`/`webhook`/`pg_notify`/`container` handler 已随 worker 执行路径移除。当前执行用户代码的唯一方式是运行用户容器镜像。
