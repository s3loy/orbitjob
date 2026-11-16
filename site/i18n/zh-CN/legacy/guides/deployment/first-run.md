---
title: "首次运行"
---

先获取仓库并进入根目录：

```bash
git clone --branch dev https://github.com/s3loy/orbitjob.git
cd orbitjob
```

后续仓库命令均在同一目录运行。安装 chart 前先准备集群和数据库。

<a id="1-cluster"></a>
### 1. 集群

```bash
make kind-up
```

如果 `orbitjob-dev` 集群不存在则创建，并将 kubectl 切换至该集群。这一步只创建集群。

<a id="2-database"></a>
### 2. 数据库

```bash
make kind-db
```

Chart 还需要第一步未提供的 PostgreSQL 服务和已配置的数据库 Secret。各组件读取各自角色对应的键，完整映射见 `charts/orbitjob/values.yaml`。此操作必须在 `helm install` 之前执行。缺少 Secret 时，Helm hook 会出现 `CreateContainerConfigError`，release 进入 `failed` 状态。

该步骤执行以下操作：

1. 数据库 Deployment 不存在时，使用 `scripts/render-local-manifest.sh`，按所选数据库命名空间（默认为 `orbitjob`）渲染并应用 `deploy/kind/postgres-17.yaml`。
2. 将该 Pod 的端口转发到 `127.0.0.1:15432`。
3. 确保本地 bootstrap 角色存在，再运行 `cmd/configure setup --mode external`；该命令创建或复用 `.runtime/kind/database.json` 中的状态，并写入 `.runtime/kind/database.env`。
4. 在 `orbitjob-system` 中创建 `orbitjob-database` Secret，将 DSN 的主机地址从 `127.0.0.1:15432` 改写为集群内 Service。

该操作幂等。重复运行会保留现有 bootstrap API 密钥，因此此前导出的 `ORBITJOB_API_KEY` 继续有效。通过 `ORBITJOB_NAMESPACE` 和 `ORBITJOB_DB_NAMESPACE` 覆盖命名空间，通过 `ORBITJOB_DB_PORT` 覆盖本地端口。数据库 manifest 与 Service DSN 使用相同的解析后命名空间。

<a id="3-images"></a>
### 3. 镜像

```bash
make docker-build TAG=dev
make kind-load TAG=dev
```

将 Makefile 中 `DOCKER_COMPONENTS` 定义的镜像构建为 `ghcr.io/s3loy/orbitjob-<component>:dev`，包括日志归档镜像，并加载到集群。加载目标还会准备 Fluent Bit 采集器。`TAG=dev` 用于区分开发镜像与发布版本。

Docker 上下文只允许 Go 源码和 Console 构建输入。Go 阶段通过独立源码阶段复制 `api/`、`cmd/` 和 `internal/`，前端修改不会使这些 Go 构建输入失效。Admin 镜像单独构建 Console，并包含编译后的 `/console` 资源。`.dockerignore` 排除公共站点、测试、部署文件、本地依赖和运行证据。

源码开发流程使用这些新构建的镜像。安装已发布版本时，需要匹配的 chart 与组件镜像 tag，以及兼容的升级路径。选择 tag 前先检查对应发布版本。

<a id="4-install"></a>
### 4. 安装

```bash
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --wait --timeout=10m
```

Values 文件设置 OrbitJob 组件的共用镜像 tag 和拉取策略，必须与第三步加载的 tag 一致。本地开发 values 显式启用 HTTP 回环会话。现有安装应先遵循后文的升级边界。

`operator.namespaceTenants` 为必填项。Operator 根据配置将命名空间映射到租户，不读取 CR 注解中的租户声明，因此写入 Custom Resource 不能用于冒用其他租户。此映射没有默认值；必须显式分配命名空间。上述 values 将 `default` 命名空间映射到 bootstrap 租户。

缺少此值时，chart 渲染失败并返回以下信息：

```
operator.namespaceTenants is required when operator.enabled is true. Set it to
"namespace=tenant[,namespace=tenant]", or set operator.enabled=false to install
without the operator.
```

设置 `operator.enabled=false` 可以在无映射、无 Operator 的情况下安装控制平面；此配置不会协调定义或执行 Run。

<a id="5-reach-the-api"></a>
### 5. 访问 API

```bash
kubectl port-forward -n orbitjob-system svc/orbitjob-admin-api 18080:8080 &
curl http://localhost:18080/healthz
# {"status":"ok"}
```

健康端点为 `/healthz`，没有 `/health`。该端点反映进程存活状态；返回 200 不表示工作负载已验证，也不证明依赖数据库的操作成功。

<a id="6-credentials"></a>
### 6. 凭据

```bash
source <(make kind-env)
```

`kind-env` 从 `bootstrap-api-key` Secret 读取 bootstrap 集成密钥，并向当前 shell 导出 `ORBITJOB_API_KEY` 和 `ORBITJOB_API` 以调用 API。它不会登录 Console。妥善保护该密钥，并为日常脚本签发权限更窄的专用密钥。

Console 使用[首位管理员设置](/orbitjob/docs/accounts/#create-the-first-administrator)流程，指定 `-origin http://localhost:18080`，再打开一次性链接并创建个人用户名和密码账户。通过工作区成员邀请添加其他用户。已有账户使用密码登录；已完成首次设置的安装不会重新开放设置流程。

<a id="7-check-it"></a>
### 7. 检查

```bash
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" \
  http://localhost:18080/api/v1/tenants
```

预期返回 bootstrap 租户：

```json
{"items":[{"id":"00000000000000000000000001","slug":"default","name":"Default","status":"active"}]}
```

不带请求头的请求返回 401。这只验证一次已认证的 API 查询。使用 Console 时，登录个人账户，选择已授权的工作区，再按[首个任务流程](/orbitjob/docs/getting-started/)操作。任务的最终执行阶段与日志必须独立于 API 连通性进行检查。
