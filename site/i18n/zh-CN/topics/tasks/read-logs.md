# 读取执行日志

**Execution logs** 面板汇总保留的平台事件，以及跨 Attempt 的容器
stdout/stderr。查看输出时，同时读取采集状态和缺口标记；采集与执行结果是独立
观测。

## 开始前

作业运行归档要求 `instance:ReadLogs`；Function 归档要求对实际 Function
父资源具有 `function:ReadLogs`。普通运行或 Function 读取权限不授予日志访问。
实例归档路由要求凭据不受资源组限制。限定分组范围的 Function 凭据使用
Function 历史 URL，并依据 Function 已持久化的分组归属授权。

在 **Jobs** 或 **Runs** 中打开已记录的运行，或在 Function 历史中打开终态调用。
归档必须已注册且可访问。如果父定义不可用，仅保留文件不能维持 Function 日志
URL 的可访问性。

## 在 Console 中查看输出

打开 **Execution logs**。选择单个或全部 Attempt，按阶段过滤并搜索保留的文本。
重新连接时，从最后游标继续跟随；暂停跟随会停止浏览器轮询，采集继续运行。
需要时复制或下载保留的输出。

命令证据记录已审核镜像和 argv、发出的输出，以及观察到的退出码或信号。
只有工作负载打印或跟踪内部 shell 命令时，这些命令才会出现。应将输出作为敏感
文本处理。

归档封存不能证明完整覆盖来源。节点丢失、队列驱逐、来源轮转和容量限制都可能
遗漏输出。范围未知的缺口必须保持未知。空页可能表示没有匹配的保留记录，不能
证明工作负载没有产生输出。

## 通过 API 读取归档

使用[调用 API](call-api.md)中的认证 API 设置。将 `73` 替换为实际实例 ID：

```sh
RUN_ID=73
curl --silent --show-error --fail-with-body \
  -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  "$ORBITJOB_API_URL/api/v1/instances/$RUN_ID/logs/archive?after=0&limit=500"
```

后续读取使用响应中的 `next` 游标，并在重新连接时保留该游标。查询参数包括
`after`（非负值）、`limit`（1–1,000）、`attempt`（正 Attempt 编号）和
`search`（最多 256 个 Unicode 码点）。搜索只覆盖保留的证据。Function 路由为
`/api/v1/functions/{function_id}/runs/{history_uuid}/logs/archive`。

在查询字符串前追加 `/download` 可导出文本。下载包含省略标记和显式结束标记。
成功 HTTP 状态之后发生传输中断，不能证明导出完整。

独立的 `/attempts/{attempt}/logs` 端点从保留的 Kubernetes 资源读取有界快照。
它不保证持久化保留，并可能在 Kubernetes 垃圾回收后不可用。`attempt` 路径
使用台账中的正 Attempt 编号，不是行 ID。

## 保留与删除

默认情况下，归档从完成和采集封存两者中较晚的时间起保护 15 天。后续接受的输出
延长保护期；输出上限仍适用。默认逻辑执行预算为 32 MiB，由全部 Attempt、Pod
和流共享。超限输出保留最初 8 MiB 及最近 24 MiB 窗口，并显示省略情况。
租户、工作流和安装实例预算请参阅
[归档限制](../execution-logs.md#installation-and-capacity)。

**Delete retained logs** 要求独立的 `instance:DeleteLogs` 或
`function:DeleteLogs` 操作。它提交经过审计的清理请求，可以在保护期内移除
终态归档。清理异步执行，保留执行摘要。删除日志不会取消执行或停用来源定义。

归档不可用或发生完整性错误时，保留报告的身份并联系安装实例管理员。不要用空
归档替换损坏证据。请参阅[归档恢复](../execution-logs.md#integrity-and-recovery)。
