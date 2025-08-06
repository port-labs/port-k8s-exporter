package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	DurationSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "port_k8s_exporter_duration_seconds",
			Help: "duration description",
		},
		[]string{"kind", "phase"},
	)
	ObjectCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "port_k8s_exporter_object_count",
			Help: "object_count description",
		},
		[]string{"kind", "object_count_type", "phase"},
	)
	Success = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "port_k8s_exporter_success",
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
