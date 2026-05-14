# etcd 分布式协调层基准测试报告

> 测试日期：2026-05-15  
> 测试人：s3  
> 测试目的：量化 etcd 引入前后（memory vs 单节点 etcd vs 三节点 etcd）在 election 和 discovery 两层的性能差异。

---

## 1. 测试环境

| 项目 | 配置 |
|------|------|
| OS | Windows 11 Pro |
| CPU | AMD Ryzen 7 8845H (8C16T) |
| Go | 1.26.3 |
| etcd 镜像 | quay.io/coreos/etcd:v3.5.21 |
| Docker | Docker Desktop (Windows) |

etcd 部署方式：
- **单节点**：`docker-compose.yml` 中定义的 etcd 服务，端口 `2379`
- **三节点集群**：`docker-compose-etcd-cluster.yml`，端口 `2379/22379/32379`

---

## 2. 测试方法

### 2.1 架构

采用 **build tag 隔离 + 统一 benchmark runner**：

```
benchmark_test.go              // 无 tag，定义 benchmark 函数
benchmark_memory_test.go       // !etcd tag，注入 memory 实现
benchmark_etcd_test.go         // etcd tag，注入 etcd 实现
```

- memory：基于 `sync.Mutex` + `map`，零外部依赖
- etcd：基于 `go.etcd.io/etcd/client/v3` + `concurrency` 包

### 2.2 测试项

**election 层：**
- `BenchmarkCampaign` — 首次 leader 竞选延迟
- `BenchmarkTryLock` — 无竞争时锁获取延迟
- `BenchmarkTryLockContention` — 多 goroutine 竞争锁（2/4/8 并发）

**discovery 层：**
- `BenchmarkRegister` — 单次服务注册延迟
- `BenchmarkListInstances` — 查询延迟（10/100/1000 实例规模）
- `BenchmarkWatchLatency` — 注册 → Watch 事件到达延迟

### 2.3 运行命令

```bash
# Memory 基线（无 tag）
go test -bench=. -benchmem -count=5 ./internal/platform/election/ ./internal/platform/discovery/

# 单节点 etcd
go test -tags etcd -bench=. -benchmem -count=5 ./internal/platform/election/ ./internal/platform/discovery/

# 三节点 etcd
ETCD_ENDPOINTS="localhost:2379,localhost:22379,localhost:32379" \
  go test -tags etcd -bench=. -benchmem -count=5 \
  ./internal/platform/election/ ./internal/platform/discovery/

# benchstat 对比
benchstat bench-memory.txt bench-etcd.txt bench-etcd-cluster.txt
```

---

## 3. 测试结果

### 3.1 election 层

| 测试项 | Memory | 单节点 etcd | 三节点 etcd | 单节点倍数 | 三节点倍数 |
|--------|--------|-------------|-------------|-----------|-----------|
| Campaign | 1.24 µs | 10.48 ms | **19.46 ms** | ~8480x | **~15745x** |
| TryLock | 4.19 µs | 5.52 ms | **10.11 ms** | ~1315x | **~2412x** |
| TryLockContention/2 | 180 ns | 1.31 ms | **2.87 ms** | ~7290x | **~15930x** |
| TryLockContention/4 | 135 ns | 953 µs | **1.75 ms** | ~7080x | **~13000x** |
| TryLockContention/8 | 138 ns | 923 µs | **1.04 ms** | ~6690x | **~7570x** |

### 3.2 discovery 层

| 测试项 | Memory | 单节点 etcd | 三节点 etcd | 单节点倍数 | 三节点倍数 |
|--------|--------|-------------|-------------|-----------|-----------|
| Register | 3.01 µs | 11.31 ms | **16.76 ms** | ~3760x | **~5570x** |
| ListInstances/10 | 931 ns | 1.41 ms | **5.70 ms** | ~1510x | **~6120x** |
| ListInstances/100 | 4.83 µs | 2.39 ms | **3.85 ms** | ~493x | **~796x** |
| ListInstances/1000 | 37.8 µs | 8.15 ms | **12.78 ms** | ~215x | **~337x** |
| WatchLatency | 1.23 µs | 10.87 ms | **22.23 ms** | ~8830x | **~18080x** |

### 3.3 内存分配对比（三节点 etcd vs Memory）

| 测试项 | Memory B/op | 三节点 etcd B/op | 增长倍数 |
|--------|-------------|------------------|---------|
| Campaign | 341 | 29,346 | ~85x |
| TryLock | 273 | 14,169 | ~51x |
| Register | 276 | 17,661 | ~63x |
| WatchLatency | 295 | 19,329 | ~65x |
| ListInstances/1000 | 87.7 KiB | 417.8 KiB | ~4.8x |

---

## 4. 数据分析

### 4.1 三节点 vs 单节点的 overhead：约 1.5x ~ 2x

这是 Raft 多数派写入的固有成本：

```
单节点：client → leader 落盘 → 返回
三节点：client → leader → follower 同步 → 多数派确认 → 返回
```

三节点需要等待 leader 将日志复制到至少 1 个 follower 并收到确认，延迟增加约一个网络 RTT。

### 4.2 规模效应：ListInstances 随规模扩大，"相对倍数"反而下降

| 规模 | 三节点 vs Memory |
|------|-----------------|
| 10 实例 | ~6120x |
| 100 实例 | ~796x |
| 1000 实例 | ~337x |

原因：memory 的 map 遍历成本随规模线性增长（O(n)），而 etcd 的 Range 请求相对固定（一次 RPC + 服务端遍历）。当规模足够大时，memory 的优势被稀释。

### 4.3 三节点集群在高负载下出现 request timeout

`BenchmarkListInstances/instances=1000` 的 setup 阶段需要预注册 1000 个实例，三节点 etcd 在此过程中多次返回：

```
etcdserver: request timed out
```

这说明：
- 三节点的写入吞吐量低于单节点
- etcd 集群对磁盘 I/O 敏感，需要独立 SSD，不能和业务进程抢资源
- benchmark 的高并发注册场景远超生产环境的实际负载（生产是秒级心跳，不是 µs 级压力测试）

### 4.4 内存分配差异

etcd 每次操作伴随大量分配（protobuf 编解码、gRPC 帧、lease 维护），而 memory 实现：
- `TryLockContention` 在 memory 下是 **0 B/op, 0 allocs/op**（纯 `sync.Mutex`）
- 在 etcd 下是 **~19 KiB/op, ~286 allocs/op**

这意味着 etcd 会给 GC 带来显著压力，但对于低频操作（秒级/分钟级）可以忽略。

---

## 5. 结论与建议

### 5.1 什么时候用 memory

- 开发/测试环境
- 小规模单节点部署
- 对延迟敏感（< 1ms）但不需要高可用的场景

### 5.2 什么时候用 etcd

- 生产多节点高可用部署
- 需要自动 leader 故障转移
- 需要跨节点的服务注册发现

### 5.3 部署建议

| 维度 | 建议 |
|------|------|
| 节点数 | 生产推荐 3 或 5 节点（奇数，避免脑裂）|
| 磁盘 | 独立 SSD，低延迟随机写入 |
| CPU | 至少 2 核，与业务进程隔离 |
| 网络 | 节点间低延迟（< 5ms），建议同机房 |
| 监控 | etcd 集群健康、磁盘 I/O、leader 切换频率 |

### 5.4 性能预期

以本次测试的三节点 etcd（同机 Docker）为参考：

| 操作 | 预期延迟 |
|------|---------|
| Campaign（leader 选举）| 15 ~ 25 ms |
| TryLock（无竞争）| 8 ~ 12 ms |
| Register（服务注册）| 12 ~ 20 ms |
| WatchLatency（事件通知）| 15 ~ 25 ms |
| ListInstances（1000 规模查询）| 5 ~ 15 ms |

> 注：实际生产环境中，etcd 集群独立部署、网络延迟更低时，上述数字会优于同机 Docker 测试结果。

---

## 6. 复现步骤

```bash
# 1. 启动单节点 etcd
docker compose up -d etcd
make bench-etcd-compare

# 2. 启动三节点 etcd 集群
docker compose -f docker-compose-etcd-cluster.yml up -d
ETCD_ENDPOINTS="localhost:2379,localhost:22379,localhost:32379" \
  go test -tags etcd -bench=. -benchmem -count=5 \
  ./internal/platform/election/ ./internal/platform/discovery/

# 3. benchstat 对比（需安装）
# go install golang.org/x/perf/cmd/benchstat@latest
benchstat bench-memory.txt bench-etcd.txt bench-etcd-cluster.txt
```

---

## 7. 相关文件

| 文件 | 说明 |
|------|------|
| `internal/platform/election/benchmark_test.go` | election benchmark 定义 |
| `internal/platform/discovery/benchmark_test.go` | discovery benchmark 定义 |
| `scripts/bench-etcd.sh` | 一键对比脚本（单节点）|
| `docker-compose-etcd-cluster.yml` | 三节点 etcd 集群配置 |
| `Makefile` | `bench-etcd-memory` / `bench-etcd` / `bench-etcd-compare` 目标 |
