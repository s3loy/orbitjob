---
title: "快速开始"
---

本节说明如何在已包含 Console 的安装中运行第一个任务。如果尚无可用安装，请先参阅[部署](/orbitjob/docs/deployment/prerequisites/)或[源码开发环境](https://github.com/s3loy/orbitjob/blob/main/README.md#development)。

<a id="before-you-begin"></a>
### 开始之前

向管理员获取安装地址和工作区邀请。创建个人账户，或登录已有账户后接受邀请。成员权限必须允许创建定义、执行和读取历史，见 [Console 权限](/orbitjob/docs/usage/web-console/)。日志访问需要单独授权。
管理员必须已配置工作区的执行命名空间与 RBAC。使用集群能够拉取的容器镜像。

安装者通过[一次性设置链接](/orbitjob/docs/accounts/#create-the-first-administrator)创建首位管理员，再通过工作区邀请授予访问权限。脚本和 HTTP 客户端使用单独签发的 API 密钥。

<a id="web-console"></a>
<a id="connect-and-run-a-task"></a>
### 连接并运行任务

1. 打开安装地址；`/` 会重定向至 `/console/`。输入用户名和密码，选择 **Sign in**，然后选择工作区。在共用设备上登录前，检查 **Keep me signed in for 7 days** 选项。
2. 打开 **Jobs → Create a job**。输入名称，选择已配置的执行命名空间和 **Manual only**，再填写镜像、命令与参数。依次选择 **Review and save → Check against cluster → Save definition → Confirm save**。
3. 等待已保存版本就绪，再打开工作负载。选择 **Trigger → Confirm trigger**。通过回执打开对应执行并检查最终阶段。**Execution logs** 需要日志读取权限。

保存定义不会执行手动任务。触发回执表示请求已接受；执行阶段记录其结果。冲突处理、观察和取消见 [Console 指南](/orbitjob/docs/usage/web-console/)。服务端会在页面重新加载后保留登录会话。过期、邀请与恢复行为见[账户与会话](/orbitjob/docs/accounts/)。

<a id="helm"></a>
