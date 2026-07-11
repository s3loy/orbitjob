# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.4-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[English](./README.en.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

OrbitJob 是云原生分布式作业调度平台。PostgreSQL 作持久状态源，多进程职责分离（admin-api / scheduler / dispatcher / worker），分布式协调支持 etcd 或 K8s Lease。

用 OrbitJob 来：

- 用 cron 表达式或手动触发定时任务
- 多租户隔离运行作业，每租户独立 API key
- 自动恢复失败任务（lease 回收、重试、orphan recovery）
- 用 Prometheus 指标、结构化日志、健康检查监控执行
- 定义巡检和 SLO，追踪错误预算

## 核心机制

- PostgreSQL 存任务状态，事务保证一致性
- 四进程职责分离：admin-api（HTTP 控制面）、scheduler（触发）、dispatcher（分发）、worker（执行）
- Claim/Lease 协议保证任务至少执行一次，instance 级 lease 防重
- 协调层三模：PG-only（开发/CI）、etcd、K8s Lease（生产）

## 部署

| 方式 | 场景 | 状态 |
|---|---|---|
| Docker Compose | 本地开发 | 可用 |
| Helm + K8s | 生产（云原生） | 规划中 |

## Roadmap

- [x] S0 部署与认证
- [x] S1 任务调度闭环
- [ ] Helm K8s 部署（进行中）
- [ ] S2 Workflow 编排
- [ ] S3 Serverless 函数
- [ ] S4 高可用多副本

## Quick Start

```bash
docker compose up -d --build
```

默认创建 bootstrap tenant 和初始 API key。查看 key：

```bash
go run ./cmd/bootstrap "$DATABASE_DSN"
```

macOS Docker Desktop 需加 override 禁用 Promtail：

```bash
docker compose -f docker-compose.yml -f docker-compose.macos.yml up -d --build
```

## License

[BSD 3-Clause](./LICENSE)
