# 本地开发

构建当前源码，并在单节点 kind 集群中运行。此路径供贡献者测试包含 Console、Operator、PostgreSQL 和保留执行日志的安装。管理员管理的集群应使用[安装流程](setup/installation.md)。

命令会创建或复用 `orbitjob-dev`。如果集群包含保留数据或较旧安装，应在运行安装或升级 Chart 的命令前完成[升级准备](operations/upgrading.md)。快速开始辅助工具不会在迁移钩子前停止旧写入方。

## 前提条件

Docker 主机至少需要 4 vCPU、8 GiB 内存和初始 40 GiB 可用底层存储。数据库申请 5-GiB PVC；执行日志使用单独的 10-GiB 归档 PVC 和节点采集器状态。这些要求覆盖本地组合配置，不是生产容量规划承诺。

安装 Go 1.27.4+、支持 BuildKit 的 Docker、kind、Helm 4、与集群 Kubernetes 次版本匹配的 kubectl、Bash、GNU make、netcat、OpenSSL 和 `shasum`。托管安装工作流固定使用 kind 0.32.0 和 Helm 4.2.4 作为参考输入。主机上的 Console 开发还需要 npm，以及工具链支持的 Node.js 版本：22.x 中的 22.13.0 及后续版本、24.x，或 26.0.0 及后续版本。数据库集成检查需要 PostgreSQL 客户端工具，已安装 Console 和网络检查需要 Python 3。存储、网络、权限和证书约束参见[安装前提条件](setup/prerequisites.md)。

## 获取源码

```bash
git clone --branch dev https://github.com/s3loy/orbitjob.git
cd orbitjob
```

从该目录运行仓库命令。当前 Chart 版本不能证明已发布镜像包含本次检出的 Console 或迁移；以下流程从本源码构建全部七个组件镜像。

启动 Docker，然后运行：

```bash
docker info
bash scripts/preflight-local.sh
```

预检可能启动一个临时只读 PostgreSQL 镜像容器，以检查 Docker 文件系统容量。对于 Docker Desktop，它还会检查存放虚拟磁盘的主机文件系统。如果磁盘位于标准 macOS 位置之外，应将 `ORBITJOB_DOCKER_STORAGE_PATH` 设置为同一文件系统上的已有路径。在 Docker 主机运行检查；远程客户端文件系统不能证明远程磁盘的可用空间。

预检失败时应停止。不要通过降低存储预算或删除安装数据来绕过容量检查。

## 创建集群与数据库

```bash
make kind-up
make kind-db
bash scripts/preflight-local.sh --cluster
```

`kind-up` 在 `orbitjob-dev` 不存在时创建它，选择 `kind-orbitjob-dev` 并检查节点就绪状态。它不准备 PostgreSQL。

`kind-db` 在本地 PostgreSQL 17 Deployment 不存在时部署其清单，等待就绪，使用临时主机端口转发，并初始化受限引导账户。它将生成凭据保存在 `.runtime/kind/` 下，并在 `orbitjob-system` 中创建含集群内 DSN 的 `orbitjob-database`。PostgreSQL 位于命名空间 `orbitjob`。重新运行会复用已有状态和引导 API key；这不是通用轮换或数据库切换流程。

集群预检检查 kubectl 与服务器次版本一致，并检查数据库实际挂载卷申请至少为 5 GiB。项目尚未提供适用于较旧、较小数据库卷申请的受支持原地迁移流程。如果数据库含有需要保留的数据，应在此停止，为该安装制定存储迁移方案。遵循[备份与恢复前提](operations/backup-and-recovery.md#before-you-begin)：取得数据库维护和 Kubernetes 访问权限，确定协调停止边界，将完整备份保存到独立存储，并验证隔离恢复。调整或替换卷申请、继续安装前，应明确目标存储、数据传输、写入方切换、验证和恢复步骤。

## 构建与加载镜像

```bash
make docker-build TAG=dev
make kind-load TAG=dev
```

这些目标构建并加载 Admin API、Scheduler、Operator、migrator、bootstrap、归档和准入镜像。Admin 镜像包含 Console 和 `orbitjob-identity`。`kind-load` 还会导入固定版本的 Fluent Bit 采集器。每个 kind 节点都必须具备 Chart 选定的精确镜像引用。此 Docker 构建不需要主机 Node.js。

## 安装开发 Chart

对于新安装：

```bash
helm install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --wait-for-jobs --timeout 10m
```

values 文件选择 `global.imageTag: dev`、拉取策略 `Never`，以及命名空间与租户映射 `default=00000000000000000000000001`。启用 Operator 时，Chart 要求显式映射。工作负载不能通过自行写入 CR 注解来选择其他租户。禁用 Operator 会移除定义协调和执行能力，不构成完整工作负载安装。

开发 values 显式允许回环地址上的 HTTP 浏览器会话，不配置公开 HTTP Console。默认日志采集器配额和 NetworkPolicy 设置属于开发声明；选择生产设置前，应验证真实文件系统边界和流量。

检查所有长期运行资源：

```bash
for component in admin-api scheduler operator admission; do
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
kubectl get pods,jobs,pvc -n orbitjob-system
```

## 访问 API 与 Console

在单独终端保持以下端口转发运行：

```bash
kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080
```

在另一终端加载引导集成 API key 并查询 API：

```bash
source <(make kind-env)
curl --fail-with-body -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:18080/api/v1/tenants
```

租户列表包含 ID 为 `00000000000000000000000001` 的活动 Default 工作区。未认证请求返回 401。API key 应保密，并为普通脚本签发更窄权限的 API key。`kind-env` 读取 Secret `bootstrap-api-key` 的字段 `api-key`；它不会登录 Console。

新安装应使用 origin `http://localhost:18080` 遵循[首位管理员设置](accounts.md#create-the-first-administrator)。打开运维人员签发的一次性链接，创建个人用户名和密码账户，并在 `http://localhost:18080/console/` 登录。已被认领的安装应使用已有账户登录或文档规定的运维恢复流程，不要再次执行初始设置。

按[用户指南](../USAGE.md)创建并触发受控的首个工作负载。确认活动修订版本、终态运行和 Attempt 身份，以及保留日志。`/healthz` 表示 Admin 进程存活，不检查数据库，也不能证明工作负载路径可用。

## 安装快捷方式

对于新的可丢弃环境，辅助工具执行相同的集群、数据库、镜像和 Chart 步骤：

```bash
bash scripts/quickstart.sh
```

它还会重启 Admin API、Scheduler、Operator、归档和采集器，以使用重新构建的 `:dev` 镜像，然后将私有凭据导出写入 `.kind/env.sh`。它打印 API 端口转发和检查命令，供运维人员运行。完成后应执行前述访问和工作负载检查。

辅助工具会复用已有集群并执行 `helm upgrade --install`。对于需要保留的安装，应先遵循[升级](operations/upgrading.md)和[本地维护](local-maintenance.md)。辅助工具的就绪消息不能证明与所有旧模式或资源格式兼容。

## 修改、检查与重新部署

运行[贡献指南](../CONTRIBUTING.md)中适用的质量检查。常规 Go 检查为：

```bash
make check
make test-cover-check
```

前端变更应在 `web/` 中使用 `npm ci` 和 `npm run check`；浏览器套件有独立设置。按下文说明，在单独服务器运行数据库集成检查。

已安装模式和运行中的写入方已经满足兼容性要求时，重新构建并加载七个镜像，然后升级：

```bash
make docker-build TAG=dev
make kind-load TAG=dev
helm upgrade orbitjob ./charts/orbitjob \
  --namespace orbitjob-system -f deploy/kind/values-dev.yaml \
  --wait --wait-for-jobs --timeout 10m
for component in admin-api scheduler operator admission; do
  kubectl rollout restart -n orbitjob-system "deployment/orbitjob-$component"
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout restart -n orbitjob-system statefulset/orbitjob-log-archive daemonset/orbitjob-log-collector
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
```

复用 `:dev` 不会改变 Pod 模板。仅 Helm 升级可能继续保留旧镜像内容。重建准入镜像后，应与其他镜像使用方一起重启准入 Deployment。滚动发布后检查真实受影响用户流程。模式、CRD、身份或写入方约定改变时，应使用维护流程。

## 隔离 PostgreSQL 检查

`make integration` 要求 `TEST_DATABASE_DSN`，按顺序无缓存运行各包。测试可能重建模式、截断行，并改变集群范围角色密码。在安装服务器使用不同数据库名称不能隔离这些角色变更。使用专用可丢弃 PostgreSQL 服务器。

对于仓库的 PostgreSQL 17 集成镜像：

```bash
TEST_PG_IMAGE="$(sed -n 's/^        image: //p' .github/workflows/integration.yml)"
docker run -d --name orbitjob-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_DB=orbitjob_test -e POSTGRES_PASSWORD=postgres "$TEST_PG_IMAGE"
docker exec orbitjob-test-pg pg_isready -U postgres -d orbitjob_test
```

等待 `pg_isready` 报告接受连接，然后运行：

```bash
export TEST_DATABASE_DSN='postgres://postgres:postgres@127.0.0.1:55432/orbitjob_test?sslmode=disable'
go run ./scripts/provision-test-roles
make integration
```

DSN 为空时，Make 目标在测试启动前失败。直接运行带标签的 Go 命令时，如果没有该变量，可能跳过数据库测试；跳过测试不是数据库证据。检查结果后，仅使用 `docker rm -f orbitjob-test-pg` 删除这个自有可丢弃容器。

## 隔离安装与 Console 检查

`make kind-verify TAG=dev` 运行 `deploy/kind/verify-install.sh`。它创建或复用选定验证集群，构建 Chart 渲染的镜像，安装 PostgreSQL，并检查迁移、角色、引导行为、重复升级和失败处理。它可能改变活动上下文，并在失败时保留新建集群。应使用隔离 kubeconfig 和唯一集群名称；这不是对正常运行安装的只读检查。

已安装 Console 工具仅限 `orbitjob-console-e2e`，并会验证本地集群身份。使用前，应通过单独 kubeconfig 构建并安装该专用环境：

对于新的专用环境：

```bash
export KUBECONFIG="$PWD/.runtime/console-kind/kubeconfig"
umask 077
mkdir -p "$(dirname "$KUBECONFIG")"
kind create cluster --name orbitjob-console-e2e --kubeconfig "$KUBECONFIG" --wait 5m
KIND_CLUSTER_NAME=orbitjob-console-e2e make kind-verify TAG=dev
```

如果同名集群已经存在，应先检查，并导出其私有 kubeconfig，不要再次创建。验证脚本成功时保留预先存在的集群，但会删除自己创建的集群。显式创建步骤可将安装保留供浏览器测试使用。保留集群安装完成后：

测试夹具直接写入定义 CR。准备前，应仅将此专用集群的管理员授权为显式原生写入方；这是测试夹具前提条件，不是普通应用用户配置。使用 `kubectl auth whoami` 确认身份，并在列表中使用精确用户名（标准 kind 身份为 `kubernetes-admin`）：

```bash
helm upgrade orbitjob ./charts/orbitjob -n orbitjob --reuse-values \
  --set-json 'admission.nativeWriters=["kubernetes-admin"]' \
  --wait --wait-for-jobs --timeout 8m
```

```bash
python3 scripts/console-installed.py prepare \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
python3 scripts/console-installed.py run \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
```

工具创建私有账户、API 配置和自有工作负载测试夹具。它演练已安装 API、Operator、Kubernetes Job 和数据库，不以路由 mock 或预置运行结果替代。使用 `--spec` 选择某个受支持套件；`python3 scripts/console-installed.py --help` 列出套件。监控检查可能需要五分钟 SLO 求值间隔；未知取消检查会有意等待停止确认边界。工具运行所有选定 spec 并记录每项结果。任何失败、跳过、重试或缺失测试，都会使总体回执保持不成功；部分测试通过不能证明完整套件验收。

清理前读取私有结果。如果中断时仍持有测试终结器，应检查已记录所有权，在验证恢复条件后仅释放该测试的终结器。删除专用集群前，退役自有定义并撤销测试夹具凭据：

```bash
python3 scripts/console-installed.py cleanup \
  --kubeconfig "$KUBECONFIG" --state-dir "$PWD/.runtime/console-kind/fixtures" --port 0
kind delete cluster --name orbitjob-console-e2e
```

清理保留历史执行证据，并验证测试夹具身份。已安装证据应与浏览器测试夹具和 Demo 结果分开。

选择大量已安装环境测试或重复运行会保留受保护归档，可能耗尽测试租户的逻辑日志预算。可以启动新的自有安装、审查选定工作负载容量，或使用另行获授权的日志清理 API 处理已完成测试夹具归档。`capacity` 归档表示明确证据缺失，即使工作负载成功也一样；不要将输出断言计为通过。在自有测试安装退役前保留未解决执行证据。

## 检查、监控与清理

使用实际工作负载名称读取组件日志：

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=100
kubectl logs -n orbitjob-system deployment/orbitjob-admin-api --tail=100
```

独立 Prometheus/Grafana 栈参见[监控](operations/monitoring.md)，负载档位参见[负载测试](loadtest-guide.md)，失败症状参见[故障排查](operations/troubleshooting.md)。

kind 节点在自身存储中保存数据库、归档和采集器数据。移除需要保留的环境前，应排空执行证据并创建经过验证的[协调备份](operations/backup-and-recovery.md)。对于证据已不再需要的可丢弃环境，`make kind-down` 会在删除 `orbitjob-dev` 前要求输入 `delete-kind`。不要通过删除共享 `default` 命名空间移除测试任务。不要删除 PVC 或移除证据终结器来绕过排空失败。
