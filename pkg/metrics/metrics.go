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

func getAggregatedMetrics() *AggregatedMetrics {
	if aggregatedMetricsInstance == nil {
		aggregatedMetricsInstance = &AggregatedMetrics{
			DurationSeconds: make(map[[2]string]float64),
			ObjectCount:     make(map[[3]string]float64),
			Success:         make(map[[2]string]float64),
		}
	}
	return aggregatedMetricsInstance
}

func StartMeasuring() {
	aggregatedMetricsInstance = &AggregatedMetrics{
		DurationSeconds: make(map[[2]string]float64),
		ObjectCount:     make(map[[3]string]float64),
		Success:         make(map[[2]string]float64),
	}
}

func AddDuration(kind, phase string, duration float64) {
	getAggregatedMetrics().AddDuration(kind, phase, duration)
}

func AddObjectCount(kind, objectCountType, phase string, count float64) {
	getAggregatedMetrics().AddObjectCount(kind, objectCountType, phase, count)
}

func SetSuccess(kind, phase string, successVal float64) {
	getAggregatedMetrics().SetSuccess(kind, phase, successVal)
}

func FlushMetrics() {
	am := getAggregatedMetrics()
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

func RegisterMetrics() {
	registerOnce.Do(func() {
		prometheus.MustRegister(durationSeconds)
		prometheus.MustRegister(objectCount)
		prometheus.MustRegister(success)
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

func (am *AggregatedMetrics) AddDuration(kind, phase string, duration float64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.DurationSeconds[[2]string{kind, phase}] += duration
}

func MeasureDuration(kind string, phase string, fn func(kind string, phase string)) {
	start := time.Now()
	fn(kind, phase)
	durationSeconds.WithLabelValues(kind, phase).Set(time.Since(start).Seconds())
}

func MeasureOperation[T any](kind string, phase string, fn func(kind string, phase string) T) T {
	start := time.Now()
	result := fn(kind, phase)
	durationSeconds.WithLabelValues(kind, phase).Set(time.Since(start).Seconds())
	return result
}

func (am *AggregatedMetrics) AddObjectCount(kind, objectCountType, phase string, count float64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.ObjectCount[[3]string{kind, objectCountType, phase}] += count
}

func (am *AggregatedMetrics) SetSuccess(kind, phase string, success float64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.Success[[2]string{kind, phase}] = success
}
