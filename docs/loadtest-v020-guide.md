# OrbitJob v0.2.0 负载压测执行指南

这份指南记录我们如何把 `loadtest-v020` 跑通:从 4 小时全挂(0 成功)到修复根因、接上 Grafana、用 smoke/long 验证。它假设你已经有一个 kind 集群,`orbitjob` namespace 里 helm install 了 OrbitJob v0.2.0。

## 两种 profile

| profile | 时长 | 定义数 | 门槛 | 用途 |
|---|---|---|---|---|
| `standard` | 4h | 1200 | 10000 实例 + 干净工作区 | 发布资格 |
| `smoke` | 30m | 1200(触发一部分) | 无 | 验证修复 + 看监控曲线 |
| `long` | 8h | 1200 | 无 | 长时间 soak,验证 worker/监控稳定 |

smoke 不卡 10000 实例,允许脏工作区,rate 压到 worker 容量内。long 把并发提到 10-15, soak 8 小时看 Grafana 全曲线。开发迭代用 smoke,发版前跑 standard,重大稳定性改动后跑 long。

## 前置条件

- kind 集群 + OrbitJob v0.2.0 已 helm install 到 `orbitjob` namespace
- Docker:`smoke` >= 6 CPU / 8 GiB,`long` >= 8 CPU / 8 GiB,`standard` >= 10 CPU / 8 GiB
- 工具:`docker kind kubectl helm go curl git`
- 监控镜像(prom/prometheus、grafana/grafana)首次拉取较慢,提前 `docker pull` 或耐心等
- 重新跑之前必须先 `go run ./scripts/loadtest reset`,否则旧 tenant/api_keys/instance 会撞或污染曲线

## 五步流程

所有命令在仓库根 `orbitjob/` 执行。`smoke` 示例;`standard` 用 `test/load/config/standard.yaml`;`long` 用 `test/load/config/long.yaml`。

```bash
RUN_ID=smoke-$(date -u +%Y%m%dT%H%M%SZ)

# 0. 重置(清业务表、残留 task Job、重启 worker/prometheus)
go run ./scripts/loadtest reset

# 1. 预检(配置 + 镜像锁 + Docker 容量)
go run ./scripts/loadtest preflight --profile smoke --config test/load/config/smoke.yaml

# 2. 生成作业定义
go run ./scripts/loadtest generate --config test/load/config/smoke.yaml --run-id $RUN_ID

# 3. 部署 load fixture + 创建租户/作业
go run ./scripts/loadtest prepare --profile smoke \
  --config test/load/config/smoke.yaml --run-id $RUN_ID \
  --api-url http://localhost:18080 --bootstrap-key $ORBITJOB_API_KEY

# 4. 跑(后台,30 分钟)
go run ./scripts/loadtest run --profile smoke \
  --config test/load/config/smoke.yaml --run-id $RUN_ID \
  --api-url http://localhost:18080

# 5. 验证 + 报告
go run ./scripts/loadtest verify --run-id $RUN_ID \
  --orbitjob-dsn "$ORBITJOB_OWNER_DSN" --api-url http://localhost:18080 \
  --fixture-url http://localhost:18081
go run ./scripts/loadtest report --run-id $RUN_ID
```

`--api-url` 指向 admin-api,用 `kubectl port-forward -n orbitjob svc/orbitjob-admin-api 18080:8080` 暴露。`--orbitjob-dsn` 用 owner DSN(绕过 RLS 才能数全量 instance);本地跑时把 host 换成 `127.0.0.1:15432` 并 `kubectl port-forward -n orbitjob svc/orbitjob-postgres 15432:5432`。fixture 用 `kubectl port-forward -n orbitjob-load svc/load-fixture 18081:8080`。

## 接监控(Prometheus + Grafana)

```bash
go run ./scripts/loadtest monitor deploy
```

部署到 `monitoring` namespace,抓 4 个 OrbitJob 进程(admin-api:8080、scheduler:6060、dispatcher:6061、worker:6062)和 load-fixture 的 `/metrics`。Prometheus 用 10Gi PVC 持久化 TSDB,retention 10h,足够看完整 8h long run。然后:

```bash
kubectl port-forward -n monitoring svc/grafana 3000:3000   # 浏览器开 localhost:3000,admin/admin
kubectl port-forward -n monitoring svc/prometheus 9090:9090
```

Grafana 预置 "OrbitJob Load Test" dashboard,默认时间范围 `now-8h`,面板用 `increase(...[$__range])` 和 5m rate,适合看 8h soak。跑 loadtest 期间看曲线,跑完对比 verify 结果。

清理监控:`go run ./scripts/loadtest monitor cleanup`。如果 Prometheus PVC 卡在 Pending,检查 kind 是否有默认 StorageClass;没有就回退 `emptyDir`(但 pod 重启会丢数据)。

## 已修复的根因(2026-07-15)

4 小时 standard 跑出 0 成功、876 timeout;8 小时 long 跑出 540 success / 660 failed,Grafana "no data"。根因分三类,都已修:

**Workload 与 K8s 准入**

- **LimitRange 冲突**。`deploy/load/namespace.yaml` 给 `orbitjob-tasks` 设了 LimitRange(max CPU 500m / max Memory 256Mi),但 container handler 默认给作业 Pod 的 limit 是 1 CPU / 512Mi。作业定义没显式 resources,用默认值超了 LimitRange,K8s admission 拒绝创建 Pod,Job 卡 Running,handler 轮询 60 秒超时。修复:`generate.go` 给每个作业的 handler_payload 加 `resources.limits: {cpu: 500m, memory: 256Mi}`,匹配 LimitRange。
- **image 与 command 错配**。`generate.go` 的 `workloadCommand` 按 family ID 字符串决定命令,和 scenario 指定的 image 解耦。alpine family 走 default 分支返回 `python -c`,但 alpine 镜像没有 python。修复:`workloadCommand` 改为按 image 决定——python 用 `python -c`,alpine/busybox 用 `/bin/sh -c`,postgres 用 `psql`,curl 用 `curl`,kubectl 用 `kubectl`。

**Workload 可靠性(440 非预期失败)**

- **busybox DNS 目标错误**。external-probes 的 busybox 家族 `nslookup orbitjob-postgres.orbitjob.svc.cluster.local`,但 fixture Postgres 服务是 `load-postgres.orbitjob-load.svc.cluster.local`,~30 个实例必失败。修复:目标改成 `load-postgres.orbitjob-load.svc.cluster.local`。
- **curl 无重试/超时**。curl 用 `--retry 2` 但没有 delay、connect-timeout、max-time,fixture 瞬抖就超时。修复:加 `--retry 3 --retry-delay 2 --retry-max-time 30 --connect-timeout 5 --max-time 30`。
- **psql 无连接超时**。`psql` 用 `ON_ERROR_STOP=1`,load-postgres 重启时直接失败。修复:加 `PGCONNECT_TIMEOUT=10` 和 `PGSSLMODE=disable`。
- **kubectl RBAC 错配**。operations 家族用默认 `orbitjob-task` SA 且 `automountServiceAccountToken=false`,但 RBAC 绑给 `orbitjob-load-operations`。修复:生成 kubectl 定义时显式指定 `service_account_name: orbitjob-load-operations` 和 `automount_service_account_token: true`。
- **超时一刀切**。所有容器作业默认 60s,数据库/HTTP/operations 在负载下容易超时。修复:按 category 设 `timeout_sec`:data-processing 60s,database/http-webhook/external-probes 120s,operations 180s,failure 90s。

**监控与 worker 稳定性**

- **Prometheus retention 太短 + 无持久化**。`deploy/monitoring/prometheus.yaml` 原设 `--storage.tsdb.retention.time=2h` 且 `emptyDir`,8h run 数据被清、pod 重启丢 TSDB。修复:retention 改 10h,`emptyDir` 改 10Gi PVC,Grafana 默认 `now-8h`、面板用 `increase(...[$__range])`。
- **worker 运行期被替换**。单 worker Deployment 无探针、无 PDB、默认 RollingUpdate,运行期间 pod 被替换(restartCount=0)导致前半段计数器丢失。修复:worker 加 health/metrics 端口、liveness/readiness/startup 探针、`terminationGracePeriodSeconds: 60`、`maxUnavailable: 0` 滚动策略、单副本 PDB、资源提到 2 CPU/1Gi、容量提到 10/25。

附带修:`verify.go` 的 ledger 证据源原来查 PostgreSQL `ledger` 表,但 fixture 的 ledger 是内存字典,通过 HTTP `/ledger` 暴露。改成查 fixture HTTP 端点,verify 不再因 "missing evidence sources: ledger" 误判 INCONCLUSIVE。

## 陷阱

1. **重跑前必须 reset**。`go run ./scripts/loadtest reset` 会 truncate 业务表、**删除非 `default` 的 load-test tenant 与 api_key**、清残留 task Job、重启 worker/prometheus。否则旧 instance/tenant 会污染新 run 的曲线和成功率。
2. **LimitRange max 500m/256Mi**。作业 resources 不能超这个上限,否则 Pod forbidden。generate 已内置匹配值,别手动改作业定义超限。
3. **worker pool capacity 10**。单 worker 并发提到 10(long peak 15),rate 超了会 pool rejected(背压,正常)。smoke 的 rate 已压到容量内,standard/long 的 peak 会故意超(测背压)。
4. **smoke 允许脏工作区,standard 要求干净**。standard preflight 检查 git status,有未提交改动会 INCONCLUSIVE。smoke/long 跳过这检查。
5. **verify 的 fixture-url 与 owner DSN**。verify 本地跑,fixture 在集群内。`kubectl port-forward -n orbitjob-load svc/load-fixture 18081:8080`,然后 `--fixture-url http://localhost:18081`。owner DSN 从 `orbitjob-database` secret 的 `bootstrap-owner-dsn` key 取,本地用 sed 把 host 换成 `127.0.0.1:15432` 并 port-forward postgres。
6. **监控镜像首次拉取慢**。kind 拉 prometheus/grafana 镜像可能要几分钟,monitor deploy 的 wait 超时设 3 分钟,不够就调大。
7. **owner DSN 数 instance**。admin/runtime 角色受 RLS 限制,数不到全量 instance。verify 要用 owner DSN(绕过 RLS)。
8. **image 更新后要重启 pod**。helm upgrade 如果 tag 不变,kubelet 可能继续用旧 image。删除 pod 强制重建,或改 tag 后重新 load 进 kind。

## 清理

```bash
go run ./scripts/loadtest clean --run-id $RUN_ID --confirm   # 删 load fixture + run 目录
kubectl delete jobs -n orbitjob-tasks --all                   # 清残留作业 Job
go run ./scripts/loadtest monitor cleanup                     # 删监控
```
