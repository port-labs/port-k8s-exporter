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

type PortMetric string
type PortMetricLabel string
type PhaseSuccessStatus float64

const (
	// Metric names
	MetricDurationName    PortMetric = "port_k8s_exporter_duration_seconds"
	MetricObjectCountName PortMetric = "port_k8s_exporter_object_count"
	MetricSuccessName     PortMetric = "port_k8s_exporter_success"

	// Metrics labels
	MetricLabelKind            PortMetricLabel = "kind"
	MetricLabelPhase           PortMetricLabel = "phase"
	MetricLabelObjectCountType PortMetricLabel = "object_count_type"

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
	MetricFailedResult                    = "failed"
	PhaseSucceeded     PhaseSuccessStatus = 1.0
	PhaseFailed        PhaseSuccessStatus = 0.0
	PhaseSkipped       PhaseSuccessStatus = -1.0
)

var (
	registerOnce    sync.Once
	durationSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: string(MetricDurationName),
			Help: "duration description",
		},
		[]string{string(MetricLabelKind), string(MetricLabelPhase)},
	)
	objectCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: string(MetricObjectCountName),
			Help: "object_count description",
		},
		[]string{string(MetricLabelKind), string(MetricLabelObjectCountType), string(MetricLabelPhase)},
	)
	success = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: string(MetricSuccessName),
			Help: "success description",
		},
		[]string{string(MetricLabelKind), string(MetricLabelPhase)},
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
	// Reset gauges before repopulating to avoid stale metrics for non existing kinds
	durationSeconds.Reset()
	objectCount.Reset()
	success.Reset()
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

func RegisterMetrics() {
	registerOnce.Do(func() {
		prometheus.MustRegister(durationSeconds)
		prometheus.MustRegister(objectCount)
		prometheus.MustRegister(success)
	})
}

func StartMetricsServer(logger *zap.SugaredLogger, port int) {
	go func() {
		logger.Infof("Starting metrics server on port %d", port)
		RegisterMetrics()
		http.Handle("/metrics", promhttp.Handler())
		_ = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	}()
}

func InitializeMetricsForController(aggregatedResources *port.AggregatedResource) {
	// Initialize object count metric for each kind
	for kindIndex := range aggregatedResources.KindConfigs {
		kindLabel := GetKindLabel(aggregatedResources.Kind, &kindIndex)
		AddObjectCount(kindLabel, MetricFailedResult, MetricPhaseExtract, 0)
		AddObjectCount(kindLabel, MetricTransformResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricFilteredOutResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricFailedResult, MetricPhaseTransform, 0)
		AddObjectCount(kindLabel, MetricLoadedResult, MetricPhaseLoad, 0)
		AddObjectCount(kindLabel, MetricFailedResult, MetricPhaseLoad, 0)
		AddObjectCount(MetricKindReconciliation, MetricDeletedResult, MetricPhaseDelete, 0)
		AddObjectCount(MetricKindReconciliation, MetricFailedResult, MetricPhaseDelete, 0)
	}

	kindLabel := GetKindLabel(aggregatedResources.Kind, nil)

	// Initialize success status metric for each phase
	addDuration(kindLabel, MetricPhaseExtract, 0)
	addDuration(kindLabel, MetricPhaseTransform, 0)
	addDuration(kindLabel, MetricPhaseLoad, 0)
	addDuration(MetricKindReconciliation, MetricPhaseDelete, 0)

	// Initialize success status metric for each phase
	SetSuccessStatus(MetricKindReconciliation, MetricPhaseDelete, PhaseSkipped)
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

func addDuration(kind string, phase string, duration float64) {
	if aggregatedMetricsInstance == nil {
		return
	}

	aggregatedMetricsInstance.mu.Lock()
	defer aggregatedMetricsInstance.mu.Unlock()
	aggregatedMetricsInstance.DurationSeconds[[2]string{kind, phase}] += duration
}

func MeasureDuration[T any](kind string, phase string, fn func(phase string) (T, error)) (T, error) {
	start := time.Now()
	result, err := fn(phase)
	addDuration(kind, phase, time.Since(start).Seconds())

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

func SetSuccessStatusConditionally(kind string, phase string, succeeded bool) {
	successVal := PhaseSucceeded
	if !succeeded {
		successVal = PhaseFailed
	}
	SetSuccessStatus(kind, phase, successVal)
}

func SetSuccessStatus(kind string, phase string, successVal PhaseSuccessStatus) {
	if aggregatedMetricsInstance == nil {
		return
	}

	aggregatedMetricsInstance.mu.Lock()
	defer aggregatedMetricsInstance.mu.Unlock()
	aggregatedMetricsInstance.Success[[2]string{kind, phase}] = float64(successVal)
}

// The following helpers are exported to allow tests in external packages
// (e.g., metrics_test) to read current gauge values without importing
// unexported variables and creating import cycles.

func GetMetricValue(metricName PortMetric, expectedLabels map[PortMetricLabel]string) (float64, error) {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, fmt.Errorf("could not gather: %v", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() != string(metricName) {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[string(lp.GetName())] = lp.GetValue()
			}

			// Check if all expected labels match
			allMatch := true
			for k, v := range expectedLabels {
				if labels[string(k)] != v {
					allMatch = false
					break
				}
			}
			if !allMatch {
				continue
			}

			// Handle different metric types
			switch {
			case m.Gauge != nil:
				return m.GetGauge().GetValue(), nil
			case m.Counter != nil:
				return m.GetCounter().GetValue(), nil
			case m.Untyped != nil:
				return m.GetUntyped().GetValue(), nil
			default:
				return 0, fmt.Errorf("unsupported metric type for %s", metricName)
			}
		}
	}
	return 0, fmt.Errorf("metric %s with labels (%v) has not been populated", metricName, expectedLabels)
}
