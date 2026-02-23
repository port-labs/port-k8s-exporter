package image

import (
	"context"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
)

type Enricher struct {
	detector *Detector
	enabled  bool
}

func NewEnricher(detector *Detector, enabled bool) *Enricher {
	return &Enricher{
		detector: detector,
		enabled:  enabled,
	}
}

func (e *Enricher) Enrich(ctx context.Context, obj map[string]interface{}) {
	if e == nil || !e.enabled || e.detector == nil {
		return
	}

	refs := extractImageRefs(obj)
	if len(refs) == 0 {
		return
	}

	results := e.detector.DetectBatch(ctx, refs)
	if len(results) == 0 {
		return
	}

	osInfoMap := make(map[string]interface{}, len(results))
	for ref, info := range results {
		if info == nil {
			continue
		}
		osInfoMap[ref] = map[string]interface{}{
			"id":         info.ID,
			"versionId":  info.VersionID,
			"prettyName": info.PrettyName,
			"homeUrl":    info.HomeURL,
		}
	}

	if len(osInfoMap) > 0 {
		obj["imageOsInfo"] = osInfoMap
	}
}

func extractImageRefs(obj map[string]interface{}) []string {
	seen := make(map[string]bool)
	var refs []string

	addRef := func(ref string) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	extractFromContainers := func(containers interface{}) {
		containerList, ok := containers.([]interface{})
		if !ok {
			return
		}
		for _, c := range containerList {
			container, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if image, ok := container["image"].(string); ok {
				addRef(image)
			}
		}
	}

	extractFromPodSpec := func(podSpec interface{}) {
		spec, ok := podSpec.(map[string]interface{})
		if !ok {
			return
		}
		extractFromContainers(spec["containers"])
		extractFromContainers(spec["initContainers"])
		extractFromContainers(spec["ephemeralContainers"])
	}

	// Pod: .spec.containers
	if spec, ok := obj["spec"].(map[string]interface{}); ok {
		extractFromContainers(spec["containers"])
		extractFromContainers(spec["initContainers"])
		extractFromContainers(spec["ephemeralContainers"])

		// Deployment/StatefulSet/DaemonSet/ReplicaSet/Job: .spec.template.spec.containers
		if template, ok := spec["template"].(map[string]interface{}); ok {
			extractFromPodSpec(template["spec"])
		}

		// CronJob: .spec.jobTemplate.spec.template.spec.containers
		if jobTemplate, ok := spec["jobTemplate"].(map[string]interface{}); ok {
			if jtSpec, ok := jobTemplate["spec"].(map[string]interface{}); ok {
				if template, ok := jtSpec["template"].(map[string]interface{}); ok {
					extractFromPodSpec(template["spec"])
				}
			}
		}
	}

	if len(refs) == 0 {
		logger.Debug("No image refs found in object for OS detection")
	}

	return refs
}
