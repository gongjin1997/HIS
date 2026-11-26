package obs

import "github.com/prometheus/client_golang/prometheus"

// Metrics exposes canonical platform SLI/SLO Prometheus collectors (ARCHITECTURE O-05, D2 §1.1).
type Metrics struct {
	OperationClaimLatency prometheus.Histogram
	LeaseTakeoverTotal    prometheus.Counter
	SSELagSeconds         prometheus.Histogram
	SSHDialErrorsTotal    *prometheus.CounterVec
	TaskRunsTotal         *prometheus.CounterVec
	SQLiteWALPages        prometheus.Gauge
	Registry              *prometheus.Registry
}

// NewMetrics constructs an isolated Prometheus registry (no global init() side effects).
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		OperationClaimLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "kinstall_operation_claim_latency_seconds",
			Help:    "Latency from Operation pending creation to Worker claim.",
			Buckets: prometheus.DefBuckets,
		}),
		LeaseTakeoverTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "kinstall_lease_takeover_total",
			Help: "Total number of expired Operation leases taken over by another Worker.",
		}),
		SSELagSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "kinstall_sse_lag_seconds",
			Help:    "End-to-end event delivery latency from DB commit to SSE flush.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}),
		SSHDialErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kinstall_ssh_dial_errors_total",
			Help: "Total SSH connection/authentication errors labeled by error code.",
		}, []string{"code"}),
		TaskRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kinstall_task_runs_total",
			Help: "Total task runs labeled by task type and terminal state.",
		}, []string{"task_type", "state"}),
		SQLiteWALPages: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kinstall_sqlite_wal_pages",
			Help: "Current SQLite WAL checkpoint page count (D2 §1.1).",
		}),
	}
	reg.MustRegister(
		m.OperationClaimLatency,
		m.LeaseTakeoverTotal,
		m.SSELagSeconds,
		m.SSHDialErrorsTotal,
		m.TaskRunsTotal,
		m.SQLiteWALPages,
	)
	return m
}
