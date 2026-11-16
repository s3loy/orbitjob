---
title: "开始之前"
---

- **Console 用户：**获取安装地址和工作区邀请，然后使用个人账户登录。成员关系和策略授权必须允许目标操作。任务运行前，管理员必须配置执行 namespace 和 RBAC。
- **HTTP 客户端与脚本：**获取具有所需授权的专用租户 API key。Jobs/Runs 需要未绑定资源组的 key。API key 不能登录 Console。
- **安装管理员：**完成[安装](/orbitjob/docs/usage/helm-and-kubernetes/)和[首个账户设置](/orbitjob/docs/accounts/#create-the-first-administrator)，然后按需邀请成员并签发独立的集成 key。仅创建工作区记录不会创建其执行环境。
- **源码贡献者：**从[开发环境](/orbitjob/docs/deployment/prerequisites/)开始。本地 kind 脚本构建 `:dev` 镜像，与发布版本安装分开。

首次使用 Console 执行任务，参阅[快速开始](/orbitjob/docs/getting-started/)。客户端集成从[认证与请求约定](/orbitjob/docs/usage/authentication-and-request-conventions/)开始。[项目状态](/orbitjob/docs/status/)和[验证记录](/orbitjob/docs/verification/)分别记录已实现范围和已完成检查。
