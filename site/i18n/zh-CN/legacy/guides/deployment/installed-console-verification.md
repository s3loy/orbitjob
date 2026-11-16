---
title: "已安装 Console 验证"
---

`web/installed.config.ts` 测试套件使用已安装 Admin 镜像提供的 Console、真实 PostgreSQL 数据库和真实 Kubernetes 工作负载。除前述安装前置条件外，还需要 Python 3、前端工具链和 Chromium。使用专用的一次性 kind 集群 `orbitjob-console-e2e`、命名空间 `orbitjob` 和独立 kubeconfig。验证程序拒绝其他 context、非回环 API origin，以及来自另一集群实例的凭据。

从仓库根目录创建集群，再运行安装检查。预先创建的集群会在检查成功后保留：

```bash
mkdir -p .runtime/console-kind
chmod 700 .runtime/console-kind
kind create cluster --name orbitjob-console-e2e \
  --kubeconfig "$PWD/.runtime/console-kind/kubeconfig"
export KUBECONFIG="$PWD/.runtime/console-kind/kubeconfig"
KIND_CLUSTER_NAME=orbitjob-console-e2e make kind-verify TAG=console-e2e
npm --prefix web ci
(cd web && npx playwright install chromium)
python3 scripts/console-installed.py prepare \
  --kubeconfig "$KUBECONFIG" \
  --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
python3 scripts/console-installed.py run \
  --kubeconfig "$KUBECONFIG" \
  --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
```

每条验证命令自行管理回环端口转发；`--port 0` 选择空闲本地端口。准备阶段向每个节点加载测试工作负载镜像，创建独立的租户、资源组与权限 fixture，并通过真实 API 和 Operator 生成执行历史，不伪造执行账本行。
Bootstrap 凭据通过 API 查找安装的租户。在此受保护的一次性安装内，fixture 辅助程序随后使用 owner SQL，在 API 密钥之外配置专用个人账户和显式策略成员资格。浏览器测试使用这些账户登录；密钥用于直接 API 探针，包括资源组和平台场景。此 fixture 流程独立于生产账户设置和邀请处理。
准备阶段要求安装尚未完成首次设置且不存在账户用户；配置后将其标记为已完成首次设置。再次运行时使用新的专用数据库。

私有 `credentials.json` 包含顶层 `username`、`password` 和 `userId`，以及每个受支持租户权限配置对应的 `accountsByKey[rawKey] = { username, password, userId }`。这些字段使套件能够选择匹配的浏览器和 API 主体，不表示允许通过 API 密钥登录。资源组、平台、已撤销和已过期密钥配置仅用于 API。状态目录权限必须为 0700，凭据文件为 0600。已有凭据清单或 fixture 名称冲突时，准备阶段拒绝继续。

验收必须运行不带 `--spec` 的完整套件。单个 spec 可用于诊断，但不满足完整套件检查要求。验证程序要求每个选定测试一次通过，不得有跳过、预期失败或重试，并检查记录的源码输入在运行期间没有变化。每次调用保留独立结果目录和 `verification.json` 回执。结果、凭据与原始响应均保存在私有状态目录；CI 只上传脱敏回执。Tracing 和视频已禁用，因为它们可能记录凭据、会话 cookie 和能力链接。

套件覆盖执行关联、幂等性、重试、取消、权限与租户边界、定义、Function、Workflow、监控、日志和管理。响应来自已安装 API。写响应不明确测试会让真实写入完成，再中断向浏览器交付响应，这是有意实施的网络故障注入。取消测试还会保留测试 Job finalizer 并暂时停止 Operator，以验证未确认结果及其恢复。这些操作必须在隔离安装中执行。仅解除保留并不能证明取消成功。

定义与日志路由分别需要独立的 Apply/ReadLogs 授权，见 [Console 操作](/orbitjob/docs/usage/web-console/)。Admin 与 Operator 使用相同的 `operator.namespaceTenants` 映射。独立 Admin 未配置此映射时，这些附加路由保持禁用。执行 Job 必须具有预期的 JobRun controller reference，日志读取器才会授权访问。

验证后，先停用 fixture 定义并撤销其密钥，再删除专用集群：

```bash
python3 scripts/console-installed.py cleanup \
  --kubeconfig "$KUBECONFIG" \
  --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
kind delete cluster --name orbitjob-console-e2e
```

清理在删除所拥有的定义前检查资源 UID，并保留历史执行证据。运行中断时，先检查其私有结果，只解除本次运行自己的 finalizer，再执行清理。已安装验证证据必须与 Demo 和模拟 HTTP 的浏览器结果区分保存。
