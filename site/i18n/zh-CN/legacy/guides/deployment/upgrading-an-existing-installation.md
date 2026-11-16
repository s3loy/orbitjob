---
title: "升级现有安装"
---

现有本地 kind 安装应按[维护流程](/orbitjob/docs/local-maintenance/)准备来源控制、协调备份、网络验证、有界采集器存储与恢复。`preflight-local.sh --cluster` 还会检查 kubectl 是否与服务端次版本匹配。准备操作不会改变运行中的安装。

迁移 `0012_storage_integrity.up.sql` 和完整摘要的 Run 地址格式要求在应用迁移或启动修复版本前停止旧 Operator 与 Run 发布者，包括旧 Admin API 进程。先准备备份和维护窗口，停止并确认这些写入进程已终止，再单独应用经过审阅的 CRD 变更，运行新 chart 的 hook，最后只启动兼容二进制。切换与回滚限制见[数据库设置](/orbitjob/docs/database/#audit-permissions-and-outcome-recovery)，已测试升级场景见[验证记录](/orbitjob/docs/verification/)。迁移 `0013_execution_observation.up.sql` 为 Unknown 执行增加持久恢复元数据。部署 Lease 与来源准入变更期间，旧写入进程也必须保持停止。
已使用迁移 0001-0011 的早期 Console 安装保留原有校验和；后续变更通过正向迁移增加，不修改已记录的文件。

迁移 `0027_personal_accounts.up.sql` 增加个人身份、成员资格、能力令牌与会话。账户设置前，先安装包含 `/orbitjob-identity` 的匹配 Admin 镜像和 chart 迁移。已有 API 密钥保留其 API 行为，不会变为 Console 账户。查阅[账户迁移与回滚](/orbitjob/docs/database/#personal-account-migration)，并在签发设置链接前配置预期的 Console origin。熟悉的本地地址上运行的服务可能仍使用旧镜像；修改源码本身不会升级服务。

Helm 只在首次安装时安装 `crds/` 下的文件，常规 chart 升级不会更新现有 CRD。单独审阅并应用当前 schema：

```bash
kubectl apply -f charts/orbitjob/crds/
```

Helm 回滚不是数据库回滚。已有完整摘要 Run 地址和持久去重事实必须与恢复所用版本保持兼容。

新的执行 Pod 带有 `orbitjob.io/termination-evidence`。Operator 在持久化已验证的退出信息后，需要命名空间内的 Pod `patch` 权限才能解除该 finalizer。匹配的 Operator 与 RBAC 必须一起升级。已有 Pod 不会追补此机制，升级也无法重建缺失的旧证据。
卸载或降级前，停止新提交，并让兼容 Operator 在所有映射命名空间中完成证据持久化和解除。遵循[排空检查](/orbitjob/docs/usage/helm-and-kubernetes/#74-upgrade-and-uninstall)。恢复未完成时，保留受保护 Pod 和数据库；旧 Operator 可能无法解除这些保留状态。
