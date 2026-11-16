---
title: "前端开发"
---

从源码构建的 Admin 镜像通过现有 Admin API Service 提供 `/console/`；集群中不安装前端 Service 或 Node 运行时。完成前述端口转发后，打开 `http://localhost:18080/console/`。Console 引入之前发布的镜像需要从当前源码重新构建。仅构建 Go API 时，API 仍可用，但缺少 Console 资源会返回 503；前端开发应使用 Vite，不应向 `/console` 手动复制文件。

使用 **Node.js 22.12.0+** 和 npm。将 API 端口转发保留在独立终端中。在另一个终端从仓库根目录运行：

```bash
npm --prefix web ci
npm --prefix web run dev -- --port 4173 --strictPort
```

打开 `http://127.0.0.1:4173/console/`。该配置使用真实安装，不含 Demo 入口或打包的模拟客户端。保留 `127.0.0.1:18080` 上的 API 端口转发；开发代理目标由 `web/vite.config.ts` 定义。使用个人账户登录。代理保留浏览器的 Host 和 Origin。此本地流程中，将 `adminapi.console.publicOrigin` 留空，或设为 `http://127.0.0.1:4173`；必须显式启用 HTTP 回环模式。将 origin 固定为 18080 端口会拒绝来自 4173 的浏览器写请求。参阅[本地会话设置](/orbitjob/docs/accounts/#https-and-local-development)。每个受保护请求均独立验证访问权限。

运行浏览器测试前，通过 Ctrl+C 停止 Vite 开发服务器，使测试运行器能够使用 4173 和 4174 端口。测试会自行启动 fixture 与模拟安装服务器；4173 上已有的真实服务器不属于 fixtures 配置。在测试终端从仓库根目录运行：

```bash
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web exec -- playwright install chromium
CI=1 npm --prefix web run test:browser
```

`check` 包含生成类型漂移、TypeScript、lint、单元测试和生产构建检查。执行 `make openapi-gen` 后，`npm run generate` 从 `api/openapi.yaml` 重新生成声明；应修改 schema 源码，不应编辑生成的声明。浏览器检查在 4173 使用显式 fixtures 配置，在 4174 使用模拟 HTTP 边界的安装配置；这些检查不能证明 Kubernetes 执行成功。安装后的触发、重试和取消验证仍需一次性 kind/Helm 安装。
构建 Admin 镜像后，从 `web/` 运行 `npm run image:check -- <admin-image>`，检查实际镜像的入口、本地资源和延迟加载路由包。缺失或为空的资源包会使检查失败。该命令不启动数据库或集群；此打包检查不能证明真实工作负载执行。
`web/go.mod` 边界将 `node_modules` 中的第三方文件排除在 Go 递归检查之外，不引入前端 Go 运行时。
