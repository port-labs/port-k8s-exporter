package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
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
	aggregatedMetricsInstance = nil
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

func InitializeMetricsForController(aggregatedResources *port.AggregatedResource) {
	for kindIndex, _ := range aggregatedResources.KindConfigs {
		kindLabel := GetKindLabel(aggregatedResources.Kind, &kindIndex)
		AddObjectCount(kindLabel, MetricTransformResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricFilteredOutResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricFailedResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricLoadedResult, MetricPhaseLoad, 0)
		AddObjectCount(kindLabel, MetricFailedResult, MetricPhaseLoad, 0)
	}
}

func GetKindLabel(kind string, kindIndex *int) string {
	if kindIndex == nil {
		return kind
	}
	return fmt.Sprintf("%s-%d", kind, *kindIndex)
}

func MeasureResync[T any](resyncFn func() (T, error)) (T, error) {
	am := newAggregatedMetrics()
	res, err := resyncFn()
	flushMetrics(am)
	return res, err
}

func MeasureDuration[T any](kind string, phase string, fn func(kind string, phase string) (T, error)) (T, error) {
	start := time.Now()
	result, err := fn(kind, phase)
	if aggregatedMetricsInstance != nil {
		aggregatedMetricsInstance.mu.Lock()
		defer aggregatedMetricsInstance.mu.Unlock()
		aggregatedMetricsInstance.DurationSeconds[[2]string{kind, phase}] += time.Since(start).Seconds()
	}

	return result, err
}

func AddObjectCount(kind string, objectCountType string, phase string, count float64) {
	if aggregatedMetricsInstance == nil {
		return
	}

	aggregatedMetricsInstance.mu.Lock()
	defer aggregatedMetricsInstance.mu.Unlock()
	aggregatedMetricsInstance.ObjectCount[[3]string{kind, objectCountType, phase}] += count
}

func SetSuccess(kind string, phase string, successVal float64) {
	if aggregatedMetricsInstance == nil {
		return
	}

	aggregatedMetricsInstance.mu.Lock()
	defer aggregatedMetricsInstance.mu.Unlock()
	aggregatedMetricsInstance.Success[[2]string{kind, phase}] = successVal
}
