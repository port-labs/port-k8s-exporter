package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	// Metric names
	MetricDurationName    = "port_k8s_exporter_duration_seconds"
	MetricObjectCountName = "port_k8s_exporter_object_count"
	MetricSuccessName     = "port_k8s_exporter_success"

	// Metric kinds
	MetricKindResync         = "__resync__"
	MetricKindReconciliation = "__reconciliation__"

	// Metric phases
	MetricPhaseExtract   = "extract"
	MetricPhaseTransform = "transform"
	MetricPhaseLoad      = "load"
	MetricPhaseResync    = "resync"
	MetricPhaseDelete    = "delete"

	// Metric extract results
	MetricRawExtractedResult = "raw_extracted"

	// Metric transform results
	MetricTransformResult   = "transformed"
	MetricFilteredOutResult = "filtered_out"

	// Metric load results
	MetricLoadedResult  = "loaded"
	MetricSkippedResult = "skipped"

	// Metric deletion results
	MetricDeletedResult = "deleted"

	// Metric generic results
	MetricFailedResult = "failed"
)

var (
	DurationSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricDurationName,
			Help: "duration description",
		},
		[]string{"kind", "phase"},
	)
	ObjectCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricObjectCountName,
			Help: "object_count description",
		},
		[]string{"kind", "object_count_type", "phase"},
	)
	Success = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricSuccessName,
			Help: "success description",
		},
		[]string{"kind", "phase"},
	)

	registerOnce sync.Once
)

func RegisterMetrics() {
	registerOnce.Do(func() {
		prometheus.MustRegister(DurationSeconds)
		prometheus.MustRegister(ObjectCount)
		prometheus.MustRegister(Success)
	})
}

func StartMetricsServer(logger *zap.SugaredLogger) {
	go func() {
		logger.Infof("Starting metrics server on port %s", "6556")
		RegisterMetrics()
		http.Handle("/metrics", promhttp.Handler())
		_ = http.ListenAndServe(":6556", nil)
	}()
}

func MeasureDuration(fn func(), onDuration func(duration float64)) {
	start := time.Now()
	fn()
	onDuration(time.Since(start).Seconds())
}
