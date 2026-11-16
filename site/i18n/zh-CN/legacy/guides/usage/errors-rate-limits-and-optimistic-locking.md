---
title: "错误、限流与乐观锁"
---

<a id="13-errors-rate-limits-and-optimistic-locking"></a>

错误结构：

```json
{"error":{"code":"VALIDATION_ERROR","message":"is required","field":"name"}}
```

| HTTP | code | 场景 |
|---:|---|---|
| 400 | `MALFORMED_REQUEST` | 无法解析 JSON 或 Content-Type |
| 400 | `VALIDATION_ERROR` | 字段或领域规则校验失败 |
| 401 | `UNAUTHORIZED` | Bearer key 无效 |
| 403 | `FORBIDDEN` | 权限不足 |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `CONFLICT` | 版本过期、状态冲突或唯一键冲突 |
| 413 | `MALFORMED_REQUEST` | Admin 请求体超过统一大小限制 |
| 429 | `RATE_LIMITED` | 租户令牌桶耗尽 |
| 500 | `INTERNAL_ERROR` | 未映射的错误 |
| 503 | `SERVICE_UNAVAILABLE` | 依赖不可用 |

`field` 是可选字段。客户端应根据 HTTP 状态与机器可读的 `code` 决定处理方式；message 用于说明当前失败原因，不是稳定枚举。授权检查可能有意隐藏其他租户资源是否存在。

限流配置以进程[环境变量示例](https://github.com/s3loy/orbitjob/blob/main/deploy/env/admin-api.env.example)和 Helm 的 [`adminapi.rateLimits`](https://github.com/s3loy/orbitjob/blob/main/charts/orbitjob/values.yaml) 为准。安装配置可能不同于独立进程的代码默认值。`trigger` 令牌桶覆盖普通 Job trigger 与 Instance cancel；当前实现中，Function invoke 和 Workflow 生命周期接口使用 write 令牌桶。`429` 会包含以秒计的 `Retry-After`。至少等待该时长，使用退避，并保留同一次执行意图。不要假设每个 API key 都有独立限额。

| 结果 | 下一步处理 |
|---|---|
| `400` | 修正指出的输入；重复同一无效 payload 无法解决问题 |
| `401` | 加载有效且未过期的凭据；公共健康检查不能验证 key |
| `403` | 与管理员核对准确 action、目标租户/资源组和权限边界；不要扩大查询范围绕过限制 |
| `404` | 核对 ID 类型、父资源和租户；异步投影未完成时可能尚无可读账本行 |
| 带版本写入返回 `409` | GET 当前状态，与审阅过的基线比较，再明确决定是否使用实际最新版本重试 |
| `429` | 遵守 `Retry-After`，在有上限的重试预算内继续退避 |
| 超时 / 写响应丢失 / `5xx` | 将结果视为未知；先读取核对或按文档使用同一身份重放，再考虑另一笔写入 |

读取并审阅当前状态前，不要递增 `version`。Function 更新使用 `expected_version`；类型化定义写入使用 UID/resourceVersion、request ID 和 digest，两者是不同的并发控制契约。写入成功后详情读取被拒绝，并不代表资源未创建；保留回执，不要重复 POST。创建 Tenant、policy、group、API key、Check、SLI 或 SLO 时，没有通用的幂等 header 契约。结果不确定时，先通过获授权的目录与安装审计核对，再尝试创建。文档中 Job/Workflow trigger、Function create/invoke 和 definition write 的身份规则，不会让其他 POST 接口自动具备可重试性。
