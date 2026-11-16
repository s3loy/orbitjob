---
title: "冒烟测试与示例数据"
---

<a id="15-smoke-test-and-sample-data"></a>

覆盖主要接口的冒烟测试包含租户、API key、触发和执行查询流程，并填写 `smoke-test-results.md`：

```bash
ADMIN_BOOTSTRAP_API_KEY="$ORBITJOB_API_KEY" \
  ./scripts/smoke-test.sh http://localhost:8080
```

脚本会创建数据并触发执行，实际修改数据库。切勿对生产环境运行。取消场景是必测项：找不到目标、执行过早终止或未观察到 Running 都会导致失败。HTTP 传输错误同样失败，即使不完整响应已返回状态 200。可选探针跳过时标记为未验证，不计入全部通过。

> 历史说明：`scripts/import-jobs.sh` 与 `scripts/job-dataset.json` 面向已移除的 `POST /api/v1/jobs` 接口（handler payload 数据集），在本分支不可用。
