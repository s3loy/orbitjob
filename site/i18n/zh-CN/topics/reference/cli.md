# orbitjob 命令行工具

`orbitjob` 调用 Admin API 执行作业、运行和 Check 操作，并使用 kubectl 进行
安装诊断。它从不建立数据库连接。超出其命令集的编写和管理操作使用 Console
或 API。

## 安装

在源码检出目录中，使用 Go 1.27.3 或更高版本，与 [`go.mod`](../go.mod) 中
的最低版本要求一致：

```sh
mkdir -p "$(go env GOPATH)/bin"
go build -o "$(go env GOPATH)/bin/orbitjob" ./cmd/orbitjob
```

将该目录添加到 `PATH`，或通过绝对路径调用二进制文件。API 命令需要安装实例的
API 地址，以及具有相关授权的命名密钥。`status` 和 `doctor` 的集群部分还需要
kubectl，以及允许检查该安装实例的 kubeconfig 身份。

## 配置

连接标志属于各个末级命令。将它们放在子命令之后，例如
`orbitjob jobs list --api-url https://orbitjob.example.com`。
CLI 没有配置文件或全局标志前缀。

| 设置 | 优先级 |
|---|---|
| API 基础 URL | 非空 `--api-url`，其次为去除首尾空白的 `ORBITJOB_API_URL`，再为去除首尾空白的 `ORBITJOB_API`，最后为 `http://localhost:8080` |
| API key | 非空 `--api-key`，其次为 `ORBITJOB_API_KEY`；缺少凭据时 API 命令失败 |
| doctor 密钥回退 | 查找标志和环境变量后，读取发布命名空间中的 `bootstrap-api-key` |

URL 变量只包含基础 URL；`ORBITJOB_API_KEY` 包含密钥。使用环境变量或常规
秘密管理机制，避免将密钥字面值写入共享 shell 历史。

集群诊断使用当前 kubeconfig 上下文。默认命名空间为 `orbitjob-system`、发布名
为 `orbitjob`、监控命名空间为 `monitoring`。使用 `--namespace`、`--release`
和 `--monitoring-namespace` 选择其他安装实例。`status` 虽然接受共享连接
标志，但不使用 API key。`doctor` 可以读取安装引导凭据；仅在具有安装管理权限时
运行，或显式提供预期的专用密钥。

对于仓库配置的 kind 安装实例，`make kind-env` 输出导出命令，端口转发提供本地
访问：

```sh
source <(make kind-env)
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080
```

保持端口转发运行。在另一个 shell 中，先加载相同导出变量，再调用 API 命令。
[本地部署](local-deployment.md)说明该源码开发安装实例及其前提。

## 命令

| 命令 | 操作 |
|---|---|
| `status` | 通过 kubectl 检查安装对象 |
| `doctor` | 执行安装检查，以及 API 认证、Lease 和 RBAC 检查 |
| `jobs list` | `GET /api/v1/jobs` |
| `jobs get ID` | `GET /api/v1/jobs/{id}` |
| `jobs trigger ID` | `POST /api/v1/jobs/{id}/trigger` |
| `runs list` | `GET /api/v1/instances` |
| `runs get ID` | `GET /api/v1/instances/{id}`，包括 Attempt |
| `runs cancel ID` | `POST /api/v1/instances/{id}/cancel` |
| `checks list` | `GET /api/v1/checks` |
| `checks get ID` | `GET /api/v1/checks/{id}` |

作业 ID 是活动修订版本 ID。运行 ID 是台账实例数字 ID。Check ID 标识已存储的
Check 定义。使用相应列表响应中的 ID。触发返回 JobRun 引用和触发标识，不包含
台账运行 ID；Operator 准入随后异步创建该行。

列表命令接受 `--limit` 和 `--offset`；API 默认返回 50 项，允许 1–100 项。
`runs list` 还接受 `--phase`；`checks list` 接受 `--status active|paused`。
CLI 提供的过滤器少于 HTTP API。例如：

```sh
orbitjob jobs list --limit 20
orbitjob runs list --phase Running --limit 20 --offset 0
orbitjob checks list --status active
```

## 触发与取消

为一次预期触发保留稳定的键：

```sh
JOB_ID=42
REQUEST_KEY='invoice-check-2026-11-16-001'
orbitjob jobs trigger "$JOB_ID" --idempotency-key "$REQUEST_KEY" --json
```

将 ID 替换为预期活动修订版本。重复使用相同且仍保留的目标及请求身份，会核对
该触发项。省略键会使不同请求创建不同身份；即使使用同一键，更改修订版本也可能
创建不同工作。响应丢失后，必须先使用原始身份核对，再请求替代工作。请参阅
[触发并观察](tasks/trigger-and-observe.md)。

取消报告请求时的阶段，随后需要继续观察：

```sh
RUN_ID=73
orbitjob runs cancel "$RUN_ID" --json
orbitjob runs get "$RUN_ID" --json
```

只有已确认的 `Canceled` 阶段才能确定所请求的停止已完成。`CancelUnknown` 和
`ExecutionUnknown` 仍是非终态。命令不等待终止。请参阅
[取消运行](tasks/cancel-a-run.md)。

## 输出与帮助

API 命令输出表格或详情字段。`--json` 输出 API 响应体并附加一个末尾换行符，
不保证与传输字节完全一致。`status` 和 `doctor` 输出诊断行或由 `{status, detail}`
记录组成的 JSON 数组。JSON 不改变它们失败时的退出码。

对受支持的末级命令使用帮助：

```sh
orbitjob status -h
orbitjob doctor -h
orbitjob runs list -h
orbitjob jobs trigger -h
```

末级 `-h` 输出标志并成功退出，不连接 API。顶层 `orbitjob -h` 和只有命令组
的 `orbitjob jobs -h` 不是受支持的帮助命令。ID 命令接受标志前或后的 ID。
列表命令遵循 Go 标志解析，因此应将标志放在任何非预期位置参数之前。

## 解读安装诊断

`status` 读取最高的可见 Helm 发布 Secret 修订版本、Admin/Operator/Scheduler
三个 Deployment、观察到的 Pod 重启次数、`<release>-migrations` 中随包分发的
迁移文件名、四个 CRD 和 `kube-prometheus-stack` 发布。它不检查 Helm 发布的
已部署状态，也不检查数据库中已应用的 `schema_migrations`。升级时应单独验证
hook 结果和已应用版本。

重启检查要求 Pod 列表非空、可读，并具有有效容器状态计数器。证据缺失或格式
错误会导致失败，不会报告为零次重启。本 CLI 将缺少 Workflow CRD 和监控视为
警告。chart 仍随包分发全部四个 CRD；容忍警告不能证明当前安装实例有效。

`doctor` 包含上述检查，并且：

- 报告凭据来源，不输出凭据。
- 要求 `GET /api/v1/tenants` 返回 200。专用工作负载密钥缺少租户列表权限时，
  可能在其他任务正常的同时无法通过该检查。
- 检查 `orbitjob-operator-singleton` 是否有持有者，不评估续约时间或执行进度。
- 以当前 kubeconfig 身份，对 JobRun 以及已安装时的 Workflow 资源执行
  `kubectl auth can-i`。`--namespaces` 选择工作负载命名空间；否则读取已部署
  Operator 的命名空间映射。该操作不测试工作负载 ServiceAccount 的权限。

通过诊断仅描述可见配置和访问情况，不执行工作负载、不验证故障恢复，也不构成
安装验收。

## 退出码与范围

| 代码 | 含义 |
|---|---|
| 0 | 命令成功；诊断没有 `[FAIL]` 行 |
| 1 | 用法、传输、服务端、解码或其他未分类错误；诊断失败 |
| 3 | API 404 |
| 4 | API 401 或 403 |

API 错误保留服务端的错误代码和消息。客户端从错误输出中省略 Authorization
请求头。连接被拒绝错误包含端口转发提示。仅有警告不会使 `status` 或 `doctor`
失败。

CLI 不提供持续观察、日志跟随、shell 补全、守护进程、直接 SQL，也不提供触发和
取消之外的修改操作。[日志](tasks/read-logs.md)、[编写](tasks/manage-jobs.md)、
[Function 与 Workflow](tasks/functions-and-workflows.md)以及访问管理使用 API
或 Console。
