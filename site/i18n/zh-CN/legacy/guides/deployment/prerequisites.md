---
title: "前置条件"
---

开发安装需要 4 vCPU、8 GiB 内存和 40 GiB 初始空闲 Docker/Kubernetes 后端存储，其中包含数据库与日志卷的分配。使用 Docker Desktop 时，Docker 文件系统和存放虚拟磁盘的宿主机文件系统均须满足空闲空间要求。预检会检查 macOS 标准 `Docker.raw` 位置。如果磁盘已迁移，或 Docker Desktop 运行在其他宿主平台上，将 `ORBITJOB_DOCKER_STORAGE_PATH` 设为存放该磁盘的文件系统上的现有路径。检查必须在 Docker 宿主机上运行；远程 Desktop context 无法证明后端文件系统容量。
安装前运行 `bash scripts/preflight-local.sh`，检查现有数据库 PVC 时增加 `--cluster`。低于 5 GiB 的旧 PVC 需要按[执行日志](/orbitjob/docs/execution-logs/)所述进行离线迁移。Helm 还会安装使用独立 10-GiB PVC 的归档 StatefulSet 和 Fluent Bit 节点采集器。升级后不得省略其 rollout 检查。

当前源码要求 **Go 1.27.3+**。宿主机前端开发要求 **Node.js 22.12.0+** 和 npm；Docker 构建会安装自己的 Node 运行时，因此完整镜像构建不要求宿主机安装 Node.js。

| 工具 | 版本或所需能力 | 用途 |
|---|---|---|
| Go | **1.27.3+**（[模块要求](https://github.com/s3loy/orbitjob/blob/main/go.mod)） | 构建 Go 工具并运行安装辅助程序 |
| Docker Engine | 参考环境为 **29.5.2**，包含 BuildKit | 构建镜像并运行 kind 节点 |
| kind | 参考环境为 **0.33.0** | 创建开发集群 |
| Helm | 参考环境为 **4.3.1** | 安装 chart |
| kubectl | 与集群的 Kubernetes 次版本匹配；**1.37** 集群使用 **1.37.x** | 管理资源与端口转发 |
| Node.js / npm | **22.12.0+** / npm（[前端要求](https://github.com/s3loy/orbitjob/blob/main/web/package.json)） | 在宿主机运行 Vite、前端测试和构建 |
| Bash / GNU make | 参考环境为 **3.2 / 3.81** | 运行仓库脚本与目标 |
| netcat、OpenSSL、shasum | `nc -z`、`openssl rand -hex` 和 `shasum -a 256` | 检查端口、生成凭据和比较 Secret 内容 |
| golangci-lint | **2.15.0**（[CI 固定版本](https://github.com/s3loy/orbitjob/blob/main/.github/workflows/lint.yml)） | 运行 lint 检查 |

固定参考版本描述已记录的本地环境，不表示推定的最低版本，也不保证所有后续版本均适用。可使用这些版本复现环境。托管负载工作流单独固定 kind 0.32.0 和 Helm 4.2.4；这些是工作流输入，不是兼容版本范围。kubectl 应保持在上游[支持的版本偏差](https://kubernetes.io/releases/version-skew-policy/)内。上述 shell 工具按所需能力约束，项目尚未为其分别确定最低版本。

启动 Docker 并通过 `docker info` 检查。如果通过包管理器安装工具，运行设置流程前先检查版本。负载验证还需要 `psql` 和 `curl`；负载工具回归检查使用 Python 3。
