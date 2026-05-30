# OrbitJob Library 使用指南

OrbitJob 所有公共 API 通过 `pkg/` 暴露。你可以在自己的 Go 应用中组装调度器、分发器和执行器，无需启动独立进程。

## 完整示例

```go
package main

import (
    "context"
    "database/sql"
    "time"

    _ "github.com/lib/pq"

    "orbitjob/pkg/dispatch"
    "orbitjob/pkg/execute"
    "orbitjob/pkg/execute/handler"
    "orbitjob/pkg/schedule"
    "orbitjob/pkg/store/postgres"
)

func main() {
    ctx := context.Background()
    db, _ := sql.Open("postgres", "postgres://...")

    // 调度器：扫描 cron job，生成待执行实例
    schedRepo := postgres.NewSchedulerRepository(db)
    scheduler := schedule.NewTickUseCase(schedRepo, postgres.ClassifyError)

    // 分发器：将待执行实例分配给 worker
    dispatchRepo := postgres.NewDispatchRepository(db)
    dispatcher := dispatch.NewTickUseCase(dispatchRepo)

    // 执行器：拉取任务并执行 handler
    handler.Register("my_handler", myCustomHandler{})
    execRepo := postgres.NewExecutorRepository(db)
    handlers := map[string]execute.Handler{
        "exec":      &handler.Exec{},
        "http":      handler.NewHTTP(nil),
        "webhook":   handler.NewWebhook(nil),
        "pg_notify": handler.NewPGNotify(db),
        "my_handler": myCustomHandler{},
    }
    worker := execute.NewTickUseCase(execRepo, handlers)

    // 运行 tick...
    counts, _ := scheduler.RunBatch(ctx, time.Now(), 100)
}

type myCustomHandler struct{}

func (h myCustomHandler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
    return execute.Result{Success: true}
}
```

## 符号速查

| 包路径 | 用途 |
|--------|------|
| `pkg/schedule` | 调度器用例（扫描 cron、生成实例） |
| `pkg/dispatch` | 分发器用例（分配实例给 worker） |
| `pkg/execute` | 执行器用例（worker 拉取并执行） |
| `pkg/execute/handler` | 内置 handler（Exec、HTTP、Webhook、PGNotify） |
| `pkg/store/postgres` | PostgreSQL 存储实现 |
| `pkg/domain/*` | 领域类型和常量（job、instance、worker、validation、resource） |
| `pkg/admin/*` | 控制面 API（HTTP handler、查询/命令用例） |
| `pkg/platform/*` | 基础设施（config、logger、health） |
