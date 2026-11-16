---
title: "开始之前"
---

负载测试会修改安装：创建租户和命名空间、扩展 Operator 映射、重启组件，并可能注入数据库或组件故障。使用隔离开发安装，每次只运行一种配置；不同负载运行共用 fixture 状态和重置范围。

前置条件、源码安装和单独的停止旧写入进程升级边界见[本地部署](/orbitjob/docs/deployment/prerequisites/)。新开发安装可使用快捷命令：

```bash
bash scripts/quickstart.sh
```

然后准备 fixture 和三个端口转发：

```bash
make loadtest-prepare     # deploys shared fixture/task infrastructure
kubectl rollout status -n orbitjob-load deployment/load-fixture --timeout=5m
kubectl rollout status -n orbitjob-load deployment/load-postgres --timeout=5m

kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
kubectl port-forward -n orbitjob-load  svc/load-fixture        18081:8080 &
kubectl port-forward -n monitoring     svc/kube-prometheus-stack-prometheus 9090:9090 &
```

只有动态配置（`standard-dynamic.yaml`）需要 Prometheus 端口转发，因为反馈控制器从 `prometheus_url`（默认为 `localhost:9090`）读取实时压力指标。Prometheus 不可达时，控制器保持节奏倍率不变并记录警告，不会使运行失败。因此缺少端口转发会保留最后倍率，不会导致设置失败。静态配置不访问 Prometheus，忽略该转发。

`make loadtest-prepare` 向 `orbitjob-load`、`orbitjob-tasks` 和默认控制平面命名空间部署 manifest，不预置租户。租户、命名空间、ScheduledJob 定义和 API 密钥由工具稍后的 `prepare` 命令创建；该命令也会应用 fixture 基础设施。此 Go 命令与 `scripts/loadtest-ci.sh` 为 `ORBITJOB_NAMESPACE` 渲染运维 RBAC；Makefile 的 fixture 目标直接应用默认 manifest。覆盖命名空间时，应使用这些经过渲染的路径。

重新应用 ConfigMap/TLS Secret 不会触发 fixture 代码 rollout。Fixture 修改后复用安装时，先重启 `deployment/load-fixture` 并等待就绪，再开始运行。运行中重启会丢失内存 journal。版本化 `/journal` 响应包含启动身份和完整序列。即使重启后的 fixture 已为本次运行产生新条目，验证仍拒绝启动身份变化或序列缺口。CI 脚本会自行执行此重启。宿主端 settling 与验证通过负载工具的 Go 驱动读取 PostgreSQL；租户设置与重置在数据库 Pod 内使用 `psql`。

继续前检查 fixture 可用：

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18080/healthz   # 200
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:18081/ledger    # 200
```

<a id="the-host-has-to-stay-awake"></a>
### 宿主机必须保持唤醒

引擎根据单调时钟控制计划节奏，机器挂起时该时钟停止。三十分钟配置需要三十分钟唤醒时间。挂起会延长墙钟计划并使延迟测量失效。

macOS 上，`run` 在整个运行期间通过 `caffeinate` 持有防休眠声明。保持宿主机接通交流电；macOS 资格验证预检拒绝电池供电。其他宿主机也需要始终唤醒且可用的环境。如果仍发生挂起（macOS 上该声明需要交流电），运行退出时会报告挂起，使延长的墙钟时长能够与系统延迟区分：

```
loadtest: the host was suspended for about 1h35m0s; the schedule stretched to
1h46m12s of wall clock and the latency numbers are not usable
```
