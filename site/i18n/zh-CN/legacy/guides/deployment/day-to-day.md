---
title: "日常开发与运维"
---

<a id="change-test-redeploy"></a>
### 修改、测试与重新部署

以下开发镜像流程假定数据库已达到当前 schema，且所有运行中的写入进程均满足前述兼容边界。

```bash
make check              # lint, vet, race tests, openapi and tidy checks
make test-cover-check   # 60% coverage gate
make integration        # needs TEST_DATABASE_DSN, see below

make docker-build TAG=dev
make kind-load TAG=dev
helm upgrade orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait

for component in admin-api scheduler operator; do
  kubectl rollout restart -n orbitjob-system "deployment/orbitjob-$component"
  kubectl rollout status -n orbitjob-system "deployment/orbitjob-$component" --timeout=5m
done

kubectl rollout restart -n orbitjob-system statefulset/orbitjob-log-archive daemonset/orbitjob-log-collector
kubectl rollout status -n orbitjob-system statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n orbitjob-system daemonset/orbitjob-log-collector --timeout=5m
```

复用 `:dev` 不会改变 Deployment 的 Pod 模板。重启会使用刚加载到 kind 的镜像；仅执行 Helm 升级可能继续保留旧 Pod。

Tidy 检查使用 `go mod tidy -diff`，验证当前模块文件是否需要规范化，不修改文件，也不要求 Git 工作区无修改。

`make integration` 按顺序运行七个包且不使用缓存，在 Go 启动前拒绝空的 `TEST_DATABASE_DSN`。它要求专用 PostgreSQL 实例，并需要重建 schema 与管理部署角色的权限。直接调用带标签的 Go 命令时，缺少该变量仍可能跳过测试；跳过的测试不构成数据库证据。使用一次性服务，不得使用安装中的数据库 Pod：

```bash
TEST_PG_IMAGE="$(sed -n 's/^        image: //p' .github/workflows/integration.yml)"
docker run -d --name orbitjob-test-pg -p 127.0.0.1:55432:5432 \
  -e POSTGRES_DB=orbitjob_test -e POSTGRES_PASSWORD=postgres "$TEST_PG_IMAGE"
# Wait until this reports accepting connections before running the suite:
docker exec orbitjob-test-pg pg_isready -U postgres -d orbitjob_test
export TEST_DATABASE_DSN="postgres://postgres:postgres@127.0.0.1:55432/orbitjob_test?sslmode=disable"
go run ./scripts/provision-test-roles
make integration
```

迁移测试重建 `public`；其他辅助程序创建、删除临时 schema 并清空测试数据。角色配置可能改写同一 PostgreSQL 服务上跨数据库的登录角色密码。数据库名称包含 `test` 只是一项检查条件，不能隔离数据库服务。审阅结果后，通过 `docker rm -f orbitjob-test-pg` 删除此一次性容器。

<a id="restart-one-component"></a>
### 重启单个组件

```bash
kubectl rollout restart -n orbitjob-system deployment/orbitjob-scheduler
```

Deployment 名称为 `orbitjob-admin-api`、`-scheduler` 和 `-operator`。归档与采集器分别使用 `statefulset/orbitjob-log-archive` 和 `daemonset/orbitjob-log-collector`。
Scheduler 就绪检查使用现有 `/readyz` 数据库检查；数据库丢失可能使其不就绪，而进程仍然存活。其存活和启动探针使用 `/healthz`。Admin API 没有 `/readyz`；其 `/healthz` 就绪检查不能证明数据库连通。仅 rollout 成功不表示工作负载执行成功。

<a id="logs"></a>
### 日志

```bash
kubectl logs -n orbitjob-system deployment/orbitjob-admin-api -f
kubectl logs -n orbitjob-system deployment/orbitjob-operator --tail=100
```

也可通过 `-l` 使用 chart 设置的 `app.kubernetes.io/name=orbitjob-<component>` 标签。
