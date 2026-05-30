# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.26.3-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/s3loy/orbitjob)](https://goreportcard.com/report/github.com/s3loy/orbitjob)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)
[![Stars](https://img.shields.io/github/stars/s3loy/orbitjob)](https://github.com/s3loy/orbitjob/stargazers)

[English](./README.en.md)

![Stone Badge](https://stone.professorlee.work/api/stone/s3loy/orbitjob)

像 `database/sql` 定义 Go 如何访问数据库一样，OrbitJob 定义 Go 如何调度任务。

纯 Go 实现，PostgreSQL 为唯一必需外部依赖。内置可观测性，支持 cron 与手动触发，可嵌入为 library 也可独立部署。

- **单一依赖** — PostgreSQL 是唯一必需外部依赖，无需 Redis、Kafka 等额外基础设施
- **两种模式** — 嵌入为 library 接入现有应用，或独立部署为多进程服务
- **内置可观测性** — Prometheus 指标、健康检查、分布式追踪开箱即用
- **灵活触发** — 支持 cron 表达式调度与手动触发
- **可扩展 handler** — 内置 exec、HTTP、webhook、PGNotify，支持自定义 handler

## 快速开始

```bash
docker compose up -d
```

使用文档见 [docs](./docs)。

## License

[BSD 3-Clause](./LICENSE)
