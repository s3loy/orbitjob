---
title: "选择部署方式"
---

<a id="1-choosing-a-deployment-method"></a>

| 方式 | 适用场景 | 数据库 | 进程拓扑 | 状态 |
|---|---|---|---|---|
| **kind + Helm** | 开发与可丢弃测试 | 辅助工具配置的 PostgreSQL | Admin API、scheduler、operator、日志归档与采集器 | 见下方开发指南 |
| **已有 Kubernetes 集群上的 Helm 安装** | 托管安装 | 已配置的 PostgreSQL | Admin API、scheduler、operator、日志归档与采集器 | 需配置 admission、凭据、存储和备份 |
| 独立二进制 | 开发与自定义进程封装 | 已迁移的 PostgreSQL | 取决于组件 | admin-api/operator 仍需集群内 Kubernetes 凭据 |

> 历史说明：旧 Docker Compose 本地环境已随 worker 执行路径一起移除；本地开发使用 kind + Helm。

**源码开发**：使用 kind + Helm 配置完整环境；参阅 [`docs/local-deployment.md`](/orbitjob/docs/deployment/prerequisites/)。
