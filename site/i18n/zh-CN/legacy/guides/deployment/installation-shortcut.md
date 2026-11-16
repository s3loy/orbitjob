---
title: "安装快捷方式"
---

此命令简化前述源码开发安装步骤。它要求已检出本仓库并满足全部前置条件；从仓库根目录运行：

```bash
bash scripts/quickstart.sh
```

脚本检查前置条件，确保集群和数据库存在，构建并加载开发镜像，安装 chart，重启应用 Deployment、归档 StatefulSet 和采集器 DaemonSet，并将凭据导出命令写入 `.kind/env.sh`。脚本仅打印端口转发与 API 验证命令，不执行这些命令。加载环境文件后，完成前述第 5 至第 7 步。

已有 `orbitjob-dev` 时，辅助脚本会复用该集群。它不会在 pre-upgrade 迁移 hook 前停止旧写入进程，因此不能用于需要遵循后述兼容边界的现有安装迁移。
