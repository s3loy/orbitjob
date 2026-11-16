---
title: "监控"
---

监控栈是独立的 kube-prometheus-stack release，不属于负载工具：

```bash
make monitoring-up
kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80 &
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090 &
```

Grafana 地址为 http://localhost:3000，用户名和密码为 `admin` / `admin`。

`make observability-status` 要求指定的 OrbitJob dashboard ConfigMap、其 `grafana_dashboard=1` sidecar 标签，以及 UID 为 `orbitjob-run-ledger` 的 JSON 内容。监控栈中无关的 dashboard 不能满足此检查。可选的 Grafana 端口转发不可用时返回 WARN；Grafana API 已响应但返回 HTTP 404/500、无效 JSON 或错误 UID 时，检查失败。Prometheus 就绪、采集目标和规则检查仍为必需项。
