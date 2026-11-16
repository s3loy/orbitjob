---
title: "解读输出"
---

Run 数可能超过触发数。Smoke 配置包含 cron 定义，它们会在 `run` 命令驱动的各阶段之前、期间和之后独立按计划触发，每次 occurrence 都对应一行账本。将特定运行的触发计数与其修订版本范围数据库证据比较；其他架构的历史计数不能建立当前基线。

Smoke 配置的 1200 个定义中，`failure` 类占 220 个，因此较多 `Failed` 可能是预期测试结果，不代表系统故障。

<a id="scenario-families"></a>
### 场景类别

场景类别名称描述工作负载行为。每类都有实现该行为的独立工作负载主体；新增类别却没有对应主体时，测试失败（`TestCurlFamiliesReachTheirOwnEndpoint`、`TestNonCurlFamiliesMatchTheirBody`）。

| 类别 | 运行内容 |
|---|---|
| `sha256-json-document` | python，对包含 128 个元素的 JSON 文档计算哈希 |
| `sha256-text` | alpine，对 case id 计算哈希 |
| `select-one` | postgres，通过参数中的连接 URI 执行 `SELECT 1` |
| `invalid-sql` | postgres，执行无法解析的语句并失败 |
| `dns-resolution` | busybox，通过 DNS 解析负载 PostgreSQL |
| `service-account-token-absent` | busybox，仅在投射的 ServiceAccount token 路径不存在时成功 |
| `forced-exit-nonzero`、`exit-nonzero-message` | 输出消息后以非零码退出并失败 |
| `cancel-while-running` | python，持续运行足够长时间以等待取消到达 |
| `plain-success` | python，普通哈希主体 |
| `dns-lookup` | busybox，对负载 PostgreSQL 执行 `nslookup` |

`invalid-sql` 同时出现在 `database` 和 `failure` 中：工作负载主体相同，但分别计入各类别总数，因为两类检查不同方面，一类执行数据库工作，另一类产生失败。

Operator 渲染的 Job 设置 `automountServiceAccountToken: false`。此前需要运维 Role 的 `pods-list` 类别已改为上述 `dns-resolution`。`service-account-token-absent` 直接检查标准投射 token 文件不存在，因此直接证明无 token 工作负载边界，不通过 API 失败来推断。

`curl` 类别分别访问 `deploy/load/fixture-configmap.yaml` 中 fixture 的一个端点：

| 类别 | 端点 | 行为 |
|---|---|---|
| `paginated-api` | `/api/pages?page=1` | 200 |
| `webhook-delivery` | `POST /webhook-origin` | 201，记录幂等键 |
| `idempotent-webhook` | 两次 `POST /ledger` | 先 201，再 `duplicate: true`，只有一条账本记录 |
| `file-download` | `/fixtures/json/events.json` | 200 |
| `rate-limit-then-success` | `/api/rate-limit-then-success` | 两次 429，随后 200 |
| `server-error-then-success`、`retry-until-success` | `/api/fail-then-succeed` | 两次 500，随后 200 |
| `retry-exhausted` | `/api/always-fail` | 每次均为 500 |
| `slow-response-timeout` | `/api/slow` | 延迟响应 30s |
| `invalid-json-content-type` | `POST /api/require-json` | 除非指定 `Content-Type: application/json`，否则返回 415 |
| `tls-handshake` | `GET https://…:8443/healthz` | TLS 握手，无响应体 |
| `tls-json-body` | `GET https://…:8443/api/pages?page=1` | TLS 握手及响应体 |
| `fixture-json-download` | `/fixtures/json/events.json` | 200 |

Fixture 使用 `deploy/load/make-tls-cert.sh` 生成的自签名证书，在 8443 提供 HTTPS。客户端传入 `--insecure`，因此 `tls-*` 类别只能证明握手完成，不能证明证书链验证成功。

Fixture 运行单副本，因为 attempt 计数与副作用 journal 存放在进程内存。多副本会分散状态，使重试场景依赖每次请求落到哪个副本。
