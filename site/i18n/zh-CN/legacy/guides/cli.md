---
title: "CLI 参考"
---

`orbitjob` 是面向已安装 OrbitJob 系统的开发 CLI。它只作为客户端：通过 HTTP 访问 Admin API，通过 kubectl 访问集群，不建立数据库连接。Operator 负责 execution-ledger 写入，Admin 可写管理配置、读历史并发布 run request CR；CLI mutation 复用 API。

<a id="install"></a>

## 安装

```sh
mkdir -p "$(go env GOPATH)/bin"
go build -o "$(go env GOPATH)/bin/orbitjob" ./cmd/orbitjob
```

<a id="configuration"></a>

## 配置

每条命令都接受 `--api-url` 和 `--api-key`。未传 flag 时从环境读取：

| 来源 | API base URL | Key |
|---|---|---|
| Flag，优先级最高 | `--api-url` | `--api-key` |
| 环境变量 | 先 `ORBITJOB_API_URL`，再 `ORBITJOB_API` | `ORBITJOB_API_KEY` |
| 默认 | `http://localhost:8080` | 无，报错；`doctor` 还会尝试读取 Secret |

`make kind-env` 打印 kind 安装所需的 export：

```sh
source <(make kind-env)          # exports ORBITJOB_API_KEY and ORBITJOB_API
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080 &
orbitjob status
```

集群检查（`status`、`doctor`）和仓库脚本一样使用当前 kubeconfig context，没有额外配置文件。

<a id="commands"></a>

## 命令

```text
orbitjob status          install snapshot (kubectl only)
orbitjob doctor          diagnostic checks (API + cluster)
orbitjob runs list       list runs                GET  /api/v1/instances
orbitjob runs get ID     one run + attempt trail  GET  /api/v1/instances/ID
orbitjob runs cancel ID  request a stop           POST /api/v1/instances/ID/cancel
orbitjob jobs list       list definitions         GET  /api/v1/jobs
orbitjob jobs get ID     one active revision      GET  /api/v1/jobs/ID
orbitjob jobs trigger ID publish a manual run    POST /api/v1/jobs/ID/trigger
orbitjob checks list     list checks              GET  /api/v1/checks
orbitjob checks get ID   one check                GET  /api/v1/checks/ID
```

List filter 与 API 契约一致，例如 `runs list --phase Running --limit 20 --offset 0`、`jobs list --limit 10`、`checks list --status active`。ID 使用 list 命令输出的账本数值 ID。`jobs trigger` 接受可选 `--idempotency-key`，使重试返回同一次运行，而不是创建第二次运行。

默认每条命令输出固定宽度表格，单对象采用详情视图，兼顾人工阅读与 grep。API 命令的 --json 输出 response body 并追加换行，不保证逐字节 transport 保留。Status/doctor 则输出诊断行或 status/detail JSON array，两种格式保持相同失败退出码。

<a id="status"></a>

### status 命令

通过 kubectl 读取安装快照：Helm release 的状态 Secret 与 revision、三个 Deployment 及其重启次数、release 所带迁移版本（`<release>-migrations` ConfigMap 的 key，即 pre-install Job 应用的文件）、CRD，以及监控 release。读取的是可见最高 Helm revision，不等同 deployed status；迁移版本来自 ConfigMap filename，不等同数据库实际 schema_migrations。须分别检查 hook/applied version。缺两类 Workflow CRD 当前仅 WARN，不证明当前 Operator 可无它们运行，chart 实际提供四类 CRD。

Restart count 需要可读非空 Pod list、有效 container status 与非负 integer；RBAC/API、无 counter 或 malformed response 均 FAIL，不能把观察失败打印为 0 restarts。

```text
$ orbitjob status
[OK] helm release orbitjob revision 13 in namespace orbitjob-system
[OK] deployment orbitjob-admin-api: 1/1 ready, 0 restarts
...
WARN crd workflowruns.workloads.orbitjob.io: not installed (workflow support)
[OK] monitoring release kube-prometheus-stack present in namespace monitoring
```

<a id="doctor"></a>

### doctor 命令

包含 status 的全部检查，另检查 API 认证路径、leader lease 和 RBAC：

- API key 按 `--api-key`、`ORBITJOB_API_KEY`、release namespace 中的 `bootstrap-api-key` Secret 依次解析；输出只显示来源，不显示密钥。
- `GET /api/v1/tenants` 必须返回 200。
- Leader lease orbitjob-operator-singleton 必须有 holder，但不检查 renewal age，也不证明 holder 有进展。
- 对 `--namespaces` 指定的 namespace，以当前 kubeconfig identity（不是 workload ServiceAccount）逐一通过 kubectl auth can-i 检查 JobRun CR 权限。未传时，`doctor` 从已部署 Operator 的 `OPERATOR_NAMESPACE_TENANTS` 读取 namespace。Workflow 资源仅在相应 CRD 已安装时检查。

任何 `[FAIL]` 都会使 `orbitjob doctor` 非零退出，WARN 不导致失败。它检查 API 可达授权与可见配置，不执行 workload，不证明业务恢复，也不认证安装健康。

<a id="exit-codes"></a>

## 退出码

| 退出码 | 含义 |
|---|---|
| 0 | 成功；doctor 没有 `[FAIL]` |
| 1 | 使用错误、连接被拒绝（附带 port-forward 的 `[FAIL]` 提示）、5xx 或其他未分类失败 |
| 3 | 请求对象不存在，API 返回 404 |
| 4 | Key 无权执行操作，API 返回 403 或 401 |

错误输出保留服务端 code 与 message，例如 `admin API returned 404 NOT_FOUND: resource not found`。客户端不打印认证请求头；密钥解析只报告凭据来源。

<a id="scope"></a>

## 范围

当前 CLI 不提供 shell completion、watch/tail、daemon、直接数据库访问，也不执行 cancel 和 trigger 以外的写操作。Flag 必须放在具体命令后，例如 `orbitjob runs list --api-key ...`；子命令前没有 global flag。

使用 `orbitjob status -h`、`orbitjob runs list -h` 或对应末级命令的 `-h` 查看参数；不支持顶层 `orbitjob -h` 帮助命令。
