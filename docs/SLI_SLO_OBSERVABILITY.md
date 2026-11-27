# kinstall 平台 SLI / SLO 指标规范与监控看板模板（`SLI_SLO_OBSERVABILITY.md` · O-05 / T-M2-05 / T-M3-28）

> 对应架构约束：`ARCHITECTURE.md §12.4 O-05`、`G1 §8.1`、`DEV_TASK_CHECKLIST.md T-M2-05 / T-M3-28`。

## 1. Prometheus 指标命名规范（`/metrics`）

| 指标名 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `kinstall_http_requests_total` | Counter | `method`, `route`, `status` | 控制面 REST/SSE HTTP 请求总数 |
| `kinstall_http_request_duration_seconds` | Histogram | `method`, `route` | API 请求延迟分布（P50/P95/P99） |
| `kinstall_operations_total` | Counter | `action`, `state` | Operation 按动作与终态计数（`succeeded`/`failed`/`canceled`） |
| `kinstall_task_runs_duration_seconds` | Histogram | `task_type`, `state` | 15 类内置 Task 单主机执行耗时 |
| `kinstall_sse_active_streams` | Gauge | `replica` | 当前在线 SSE 实时日志流连接数 |
| `kinstall_worker_lease_renew_failures_total` | Counter | `replica` | Worker 租约续期失败与 Fencing 抢占告警计数 |
| `kinstall_audit_chain_seq` | Gauge | — | 当前防分叉审计哈希链最新单调序号 |
| `kinstall_media_fetch_bytes_total` | Counter | `source_type`, `delivery_mode` | Local / OBS·S3 / Online 三元介质拉取字节数 |

## 2. 核心 SLI 与 SLO 目标红线

| 业务域 | SLI 定义 | SLO 目标 | 告警阈值（Severity） |
| --- | --- | --- | --- |
| 控制面 API 可用性 | 非 5xx HTTP 请求占比（排除 `/readyz` Draining 503） | **≥ 99.95%**（30 天） | 5m 错误率 > 1%（Critical） |
| 控制面 API P99 延迟 | 常规 CRUD / Plan 查询 P99 响应时间 | **≤ 500 ms** | 10m P99 > 1.5s（Warn） |
| SSE 实时日志丢帧率 | PG 主备切换 / 副本滚动重启期间 `Last-Event-ID` 断流丢帧 | **0 丢帧** | 任意断连重同步失败 > 0（Critical） |
| 审计链完整性 | `audit verify` 每日定时全链验真通过率 | **100%** | 验链失败立即 P0 告警 |
| Worker 租约健康 | Fencing Token 非计划抢占率 | **< 0.1%** | 5m 租约续期失败 ≥ 3 次（Warn） |

## 3. Prometheus Alerting Rules 模板

```yaml
groups:
  - name: kinstall-control-plane-slo
    rules:
      - alert: KinstallHighAPI5xxRate
        expr: |
          sum(rate(kinstall_http_requests_total{status=~"5.."}[5m]))
          / clamp_min(sum(rate(kinstall_http_requests_total[5m])), 1) > 0.01
        for: 5m
        labels: { severity: critical }
        annotations:
          summary: "kinstall 控制面 5xx 错误率超过 1%"
      - alert: KinstallWorkerLeaseRenewFailure
        expr: increase(kinstall_worker_lease_renew_failures_total[5m]) >= 3
        for: 1m
        labels: { severity: warning }
        annotations:
          summary: "kinstall Worker 租约续期异常，请排查数据库连接或副本负载"
```
