# Execution Plane 契约

[返回 README](../README.md)

本文档定义 OrbitJob execution plane 的数据模型、状态语义与组件行为契约，为 scheduler、dispatcher、worker 之间的协作提供确定性规范。

> 权威来源：项目架构文档 · 更新时间 2026-05-04
>
> **注意：** 本文档描述**设计目标**（Mode B: `dispatched` 状态，Worker 自主 claim）。当前代码实现为 Mode A（`dispatching` 状态，Dispatcher 绑定 Worker）。迁移方案见 architecture.md P0 TODO。

## 当前实现状态（2026-05-04）

**已实现：**

- Job definition 中与执行路由相关的字段全链路打通
- `job_instances` 的 create 与 claim 语义已落地
- `workers` 的 heartbeat 与 lease upsert 已落地
- Scheduler MVP tick loop + misfire 策略 + 原子调度事务
- Dispatcher runtime：原子 claim + concurrency policy + priority aging + lease recovery + graceful shutdown
- **Worker executor MVP**（`cmd/worker` + HTTP callback + exec subprocess + heartbeat + graceful shutdown）——`feat/worker-executor-mvp` 分支

**未实现：**

- 状态机迁移（`dispatching` → `dispatched`，见 architecture.md ADR-0002）
- Manual trigger API
- Instance query API
- `job_instance_attempts` 完整写入链路

## 组件边界

```mermaid
flowchart LR
    Control["Control Plane<br/>job definitions"] --> Scheduler["Scheduler"]
    Scheduler --> InstanceRepo["job_instances"]
    Manual["Manual Trigger API<br/>(planned)"] -.-> InstanceRepo
    Dispatcher["Dispatcher"] --> InstanceRepo
    Dispatcher --> WorkerRepo["workers"]
    Worker["Worker"] --> WorkerRepo
    Worker --> InstanceRepo
```

## Job Definition 路由字段

| 字段 | 作用 |
| --- | --- |
| `priority` | 基础优先级，dispatcher 选取 runnable instance 时作为排序依据，值越大优先级越高 |
| `partition_key` | 逻辑分片键，用于 worker 路由、队列分区或租户隔离 |
| `handler_type` | 执行器类型标识（如 `http` / `worker`） |
| `handler_payload` | 具体 handler 配置，worker 侧按 `handler_type` 解释并执行 |

## Job Instance 状态机

```mermaid
stateDiagram-v2
    [*] --> pending
    pending --> dispatching: dispatcher claim
    retry_wait --> dispatching: retry eligible + dispatcher claim
    dispatching --> running: worker start
    dispatching --> pending: lease expired (recovery)
    running --> success: worker reports success
    running --> retry_wait: worker reports failure, retries remaining
    running --> failed: worker reports failure, no retries remaining
    running --> canceled: replace policy / control action
```

## 状态语义

| 状态 | 含义 |
| --- | --- |
| `pending` | 已创建，等待 dispatcher claim |
| `dispatching` | dispatcher 已 claim 并分配 `worker_id` 与 `lease_expires_at`，等待 worker 接手执行 |
| `running` | worker 已开始执行 |
| `retry_wait` | 上一次 attempt 已结束且仍有剩余重试次数，等待 `retry_at` 到达后重新进入 dispatch 队列 |
| `success` | 终态 -- 执行成功 |
| `failed` | 终态 -- 执行失败且无剩余重试 |
| `canceled` | 终态 -- 被 replace policy 取消或由控制动作 / 恢复动作取消 |

## Dispatcher Claim 流程

Dispatcher 每轮 tick 执行一个 bounded batch，流程分为两个阶段：

### 阶段一：Lease Expiry Recovery

在开始正常 dispatch 之前，先回收所有 `status = 'dispatching'` 且 `lease_expires_at < now()` 的孤儿 instance，将其重置为 `pending`，清除 `worker_id` 和 `lease_expires_at`。这确保了 dispatcher 崩溃后不会丢失任务。

### 阶段二：逐条 Dispatch

对每条候选 instance，在单个数据库事务内依次完成：

1. **锁定候选**：从 `job_instances` 中按优先级排序选取一条，使用 `FOR UPDATE SKIP LOCKED` 防止并发 claim
2. **锁定 job 行**：对对应 `jobs` 行加 `FOR UPDATE` 锁，读取 `concurrency_policy`
3. **统计运行数**：查询该 job 当前 `dispatching` + `running` 状态的 instance 数量
4. **策略决策**：调用纯函数 `DecideDispatch(input)` 获得决策结果
5. **执行决策**：根据结果执行 dispatch / skip / replace

### 候选排序与 Priority Aging

候选 instance 的选取排序为：

```
effective_priority DESC, scheduled_at ASC, id ASC
```

其中 effective priority 的计算方式：

```
effective_priority = min(base_priority + floor(minutes_since_scheduled), base_priority + 60)
```

即 pending instance 每等待一分钟，有效优先级自动 +1，上限为 base priority + 60。这一机制防止低优先级任务长时间饥饿。

### 候选状态

| 候选条件 | 规则 |
| --- | --- |
| `pending` | 直接符合候选条件 |
| `retry_wait` | 需满足 `retry_at <= now()` 且 `attempt < max_attempt` |

### Claim 写入

| 操作 | 说明 |
| --- | --- |
| 正常 claim | 设置 `status = 'dispatching'`、写入 `worker_id` 和 `lease_expires_at` |
| 从 `retry_wait` claim | 在上述基础上 `attempt + 1`，清除 `retry_at`、`started_at`、`finished_at`、`result_code`、`error_msg` |

## Concurrency Policy 决策

dispatcher 在 claim 候选 instance 后、写入 dispatching 状态前，根据 job 的 `concurrency_policy` 字段执行策略决策：

| 策略 | 条件 | 决策 |
| --- | --- | --- |
| `allow` | 任何情况 | dispatch -- 允许多实例并发运行 |
| `forbid` | `running_count = 0` | dispatch |
| `forbid` | `running_count > 0` | skip -- 候选 instance 保持 pending，等待下一轮 tick |
| `replace` | `running_count = 0` | dispatch |
| `replace` | `running_count > 0` | replace -- 先取消现有 dispatching/running instance，再 dispatch 新实例 |
| 未知策略 | 任何情况 | 降级为 allow 行为 |

决策逻辑实现为纯函数 `DecideDispatch`，无副作用，便于独立测试。

## Worker Heartbeat / Lease 规则

Worker 通过单次 upsert 操作同时完成注册与心跳刷新：

| 字段 | 规则 |
| --- | --- |
| `worker_id` | worker 的稳定标识，在 tenant 内唯一 |
| `status` | `online` / `offline` / `draining` |
| `capacity` | 并发处理能力，必须 `>= 1` |
| `labels` | JSON object，供路由与调度过滤使用 |
| `lease_expires_at` | heartbeat 时由 worker 显式提供的新租约截止时间 |

约束：

- heartbeat 刷新 `last_heartbeat_at`
- 同一 `(tenant_id, worker_id)` 采用 upsert 语义
- `draining` 状态的 worker 仍可维持心跳，dispatcher 是否继续分配由调度策略决定

## Retry 边界

- `job_instance_attempts` 表保留为每次执行 attempt 的不可变审计记录，当前阶段尚未实现完整写入链路
- 从 `retry_wait` 重新 claim 时，attempt 计数器在 SQL 层原子递增

## 代码位置

| 路径 | 作用 |
| --- | --- |
| `cmd/dispatcher/main.go` | Dispatcher 进程入口、配置加载与 tick loop |
| `internal/core/app/dispatch/tick.go` | Dispatcher tick 用例：lease recovery + bounded batch |
| `internal/core/domain/instance/dispatch.go` | `DecideDispatch` 纯函数与 concurrency policy 决策 |
| `internal/core/store/postgres/dispatch_repository.go` | Dispatch 事务：候选选取、policy 查询、决策执行、lease 回收 |
| `internal/core/domain/instance/claim.go` | `ClaimSpec` 与 claim 输入校验 |
| `cmd/scheduler/main.go` | Scheduler 进程入口 |
| `internal/core/app/schedule/` | Scheduler tick 用例与 misfire 策略 |

## 后续工作

- Worker executor runtime：接收 dispatching instance、执行、结果回写闭环
- Manual trigger API：手动触发 job instance 的创建
- Instance query API：instance 查询与列表接口
- `job_instance_attempts` 完整 attempt trail 写入
- 生产环境观测指标收敛与告警
