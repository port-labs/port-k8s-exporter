package metrics

import (
	"fmt"
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
	MetricLoadedResult = "loaded"

	// Metric deletion results
	MetricDeletedResult = "deleted"

	// Metric generic results
	MetricFailedResult = "failed"
)

var (
	registerOnce    sync.Once
	durationSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricDurationName,
			Help: "duration description",
		},
		[]string{"kind", "phase"},
	)
	objectCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricObjectCountName,
			Help: "object_count description",
		},
		[]string{"kind", "object_count_type", "phase"},
	)
	success = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricSuccessName,
			Help: "success description",
		},
		[]string{"kind", "phase"},
	)

	aggregatedMetricsInstance *AggregatedMetrics
)

type AggregatedMetrics struct {
	DurationSeconds map[[2]string]float64 // [kind, phase] -> duration
	ObjectCount     map[[3]string]float64 // [kind, object_count_type, phase] -> count
	Success         map[[2]string]float64 // [kind, phase] -> success (0 or 1)
	mu              sync.Mutex
}

func newAggregatedMetrics() *AggregatedMetrics {
	aggregatedMetricsInstance = &AggregatedMetrics{
		DurationSeconds: make(map[[2]string]float64),
		ObjectCount:     make(map[[3]string]float64),
		Success:         make(map[[2]string]float64),
	}
	return aggregatedMetricsInstance
}

func getAggregatedMetrics() *AggregatedMetrics {
	if aggregatedMetricsInstance == nil {
		panic("Cannot instrument metrics outside of MeasureResync context")
	}
	return aggregatedMetricsInstance
}

func flushMetrics(am *AggregatedMetrics) {
	am.mu.Lock()
	defer am.mu.Unlock()
	for key, value := range am.DurationSeconds {
		durationSeconds.WithLabelValues(key[0], key[1]).Set(value)
	}
	for key, value := range am.ObjectCount {
		objectCount.WithLabelValues(key[0], key[1], key[2]).Set(value)
	}
	for key, value := range am.Success {
		success.WithLabelValues(key[0], key[1]).Set(value)
	}
}

func MeasureResync(resyncFn func()) {
	am := newAggregatedMetrics()
	resyncFn()
	flushMetrics(am)
}

func AddObjectCount(kind, objectCountType, phase string, count float64) {
	am := getAggregatedMetrics()

	am.mu.Lock()
	defer am.mu.Unlock()
	am.ObjectCount[[3]string{kind, objectCountType, phase}] += count
}

func SetSuccess(kind, phase string, successVal float64) {
	am := getAggregatedMetrics()

	am.mu.Lock()
	defer am.mu.Unlock()
	am.Success[[2]string{kind, phase}] = successVal
}

func StartMetricsServer(logger *zap.SugaredLogger, port int) {
	go func() {
		logger.Infof("Starting metrics server on port %d", port)
		registerOnce.Do(func() {
			prometheus.MustRegister(durationSeconds)
			prometheus.MustRegister(objectCount)
			prometheus.MustRegister(success)
		})
		http.Handle("/metrics", promhttp.Handler())
		_ = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	}()
}

func MeasureDuration(kind string, phase string, fn func(kind string, phase string)) {
	start := time.Now()
	am := getAggregatedMetrics()

	fn(kind, phase)
	am.mu.Lock()
	defer am.mu.Unlock()
	am.DurationSeconds[[2]string{kind, phase}] += time.Since(start).Seconds()
}

func MeasureOperation[T any](kind string, phase string, fn func(kind string, phase string) T) T {
	start := time.Now()
	am := getAggregatedMetrics()

	result := fn(kind, phase)
	am.mu.Lock()
	defer am.mu.Unlock()
	am.DurationSeconds[[2]string{kind, phase}] += time.Since(start).Seconds()

	return result
}
