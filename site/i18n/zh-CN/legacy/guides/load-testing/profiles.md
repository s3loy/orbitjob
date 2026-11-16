---
title: "负载配置"
---

| 配置 | 配置文件 | 计划时长 | 是否请求资格验证 |
|---|---|---|---|
| `smoke` | `test/load/config/smoke.yaml` | 30m | 否 |
| `standard` | `test/load/config/standard.yaml` | 4h | 是 |
| `long` | `test/load/config/long.yaml` | 8h | 否 |

配置内部 `profile` 必须与命令的 profile 标志一致：`smoke.yaml` 声明 `profile: smoke`，与 `--profile standard` 一同使用会被拒绝。文件名不是配置标识：`smoke-faults.yaml` 也声明 `smoke`，`standard-dynamic.yaml` 声明 `standard`。

`smoke.yaml` 的三十分钟计划内不包含故障；`smoke-faults.yaml` 增加时间范围内的自定义故障计划。动态节奏可能使墙钟耗时超过配置的虚拟计划时长。带故障的配置要求后文所述恢复 canary 和独立证据。

目前只有 Kubernetes 控制平面这一条执行路径，因此配置不再声明执行模式。用于比较旧路径与 Kubernetes 路径的 `*-dual.yaml` 和 `smoke-k8s.yaml` 已删除，因为两条路径不再同时存在。配置声明 `tenant_count: 4`。生成器使用项目 ULID 生成器，将映射保存到每次运行的 `tenant-ids.json`；prepare、run 与 verify 读取同一映射。不同运行使用不同租户。`default` 等 slug 是名称，不是租户标识。仍支持显式指定租户 ID 的自定义配置，但生成声明仍包含完整运行身份摘要，使不同运行的修订版本与证据相互独立。

`standard` 被称为资格验证配置，是因为它采用完整四小时工作负载和最小实例数规则。这描述请求的工作负载，不表示已通过资格验证。缺少恢复 canary、来源观察或连续 journal 证据时，即使 Deployment 就绪，结果仍为 `INCONCLUSIVE`。参阅后文证据规则与[验证记录](/orbitjob/docs/verification/)。证据应来自特定运行的产物，不能仅以配置名称或工作流状态为依据。
