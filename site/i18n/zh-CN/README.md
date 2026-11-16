# 中文文档维护

中文站点位于配置的基础路径下的 `/zh-cn/`，默认本地入口为 `http://127.0.0.1:18432/orbitjob/zh-cn/`。页面通过语言切换器连接到同一主题的英文页面。完整流程和检查命令见[文档维护](../../../docs/project-site.md)。

| 内容 | 维护位置 |
|---|---|
| 完整主题 | `topics/`，与 `site/scripts/guide-sources.mjs` 中的英文主题标识对应 |
| HTTP 描述 | `api/current.json`，英文键与原始契约逐字匹配 |
| 已审核输入 | `reviewed.json`，记录英文和中文输入哈希 |
| 首页、导航与 API 浏览器 | `site/src/i18n.ts` 及共享组件 |
| 生成参考的章节与提示 | `site/scripts/localize.mjs` 与参考生成器 |
| 搜索界面与标题补充 | `Search.astro` 和 `search-titles.ts` |
| 旧译文与旧测试 | `legacy/`，仅作历史参考，不参与构建 |

先更新英文，再逐段核对中文的权限、条件、行为、数值和限制。保留界面实际标签、API 字段、枚举、权限 action、命令、代码示例和链接目标。中文来源中的相对链接按对应英文来源解析；构建后，已发布主题之间的链接指向中文页面，其他仓库文件链接保留原有语言。生成器为对应标题保留相同的定位标识。

## 术语

| 英文概念 | 中文用法 |
|---|---|
| job | 作业；工作流中的 task 使用“任务” |
| execution ledger | 执行台账，后文可简称“台账” |
| resource group | 资源组 |
| service level indicator / objective | 服务等级指标（SLI）／服务等级目标（SLO） |
| Check | 保留平台资源名 Check；首次需要时说明其执行 HTTP 探测 |
| occurrence | 按上下文使用“触发项”“计划触发项”；occurrence key 使用“触发标识”，保留字段 `occurrence_key` |
| typed definition | 类型化定义 |

触发标识用于关联同一次逻辑触发，与 HTTP 请求标识、幂等键和台账运行 ID 分别说明。不要将所有 occurrence 机械替换为 Run。实际界面名称、资源种类和代码字面内容保持原样。

语义审核完成后运行 `node site/scripts/translation.mjs --record-reviewed`，再执行站点构建与浏览器测试。输入哈希变化会阻止构建，要求重新核对；更新哈希本身不代表审核完成。自动检查保护字面内容和页面行为，不能代替逐段阅读。生成 Markdown、目录数据、搜索索引和 OpenAPI 显示副本均应通过所属来源重新生成。
