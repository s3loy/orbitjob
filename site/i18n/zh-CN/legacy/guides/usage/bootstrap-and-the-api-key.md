---
title: "初始管理员访问"
---

<a id="6-bootstrap-and-the-api-key"></a>

Helm 安装后，通过[一次性 setup 流程](/orbitjob/docs/accounts/#create-the-first-administrator)创建首个 Console 管理员。普通用户接受工作区邀请并使用个人账户登录，无需读取 Kubernetes Secret。安装管理权限丢失时，使用[运维恢复流程](/orbitjob/docs/accounts/#recover-installation-administration)。

<a id="bootstrap-integration-key"></a>

### Bootstrap 集成 key

Bootstrap key 继续用于旧集成和初始脚本化 API 配置，不能登录 Console，也不能证明个人账户所有权。确认 Kubernetes context，并使用 Helm release 的安装 namespace。具有读取其 Secret 的权限时，运行：

```bash
INSTALLATION_NAMESPACE='orbitjob-system'
kubectl -n "$INSTALLATION_NAMESPACE" get secret bootstrap-api-key \
  -o jsonpath='{.data.api-key}' | base64 --decode
```

保密保存输出，这是 bootstrap 交付的集成 key。它属于 Default，具有初始管理员预设权限；它不是平台凭据，也不会切换其业务数据租户。本地 quickstart 将该 key 和 API 地址写入 `.kind/env.sh`，供 shell 使用。

普通脚本应在 **Administration → API keys** 中签发命名专用 key，授予所需 action 和资源范围。将仅显示一次的明文存入获准的密钥存储。Jobs/Runs 使用未绑定资源组的 key，需要日志时另行授予日志权限。人员通过独立于这些 key 的[成员关系](/orbitjob/docs/accounts/)访问 Console。

`bootstrap-api-key` 输出 Secret 中的数据键为 `api-key`。安装输入 Secret 通常为 `orbitjob-database`，通过其 `bootstrap-api-key` 数据键提供 bootstrap 配置明文。这是两个不同的 Secret 位置，不应通过修改任一位置临时实现凭据轮换。

Bootstrap 具有幂等性。如果 key 已提交但交付失败，使用相同的配置明文重新运行。它会在重试交付前验证已存储的 bcrypt 哈希，并拒绝冲突的文件或 Secret。它不能从 PostgreSQL 恢复未知明文，也不能静默替换已有凭据。普通凭据丢失时，应替换并撤销 key。

> **已弃用**：`bootstrap.secretName` 保留为被忽略的 chart value。Bootstrap 在安装 namespace 中写入固定的 `bootstrap-api-key` 输出 Secret；应从安装 values 中移除过时覆盖项。
