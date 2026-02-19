package image

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/assert"
)

func setupEnricherTestRegistry(t *testing.T) (*httptest.Server, string, *Detector) {
	t.Helper()
	reg := registry.New()
	server := httptest.NewServer(reg)
	addr := server.Listener.Addr().String()

	layer, err := buildLayerWithOsRelease("ID=debian\nVERSION_ID=12\nPRETTY_NAME=\"Debian GNU/Linux 12\"\nHOME_URL=\"https://www.debian.org/\"\n")
	assert.NoError(t, err)
	pushImage(t, addr, "test/nginx", "1.25", layer)

	detector := NewDetector(authn.DefaultKeychain)
	return server, addr, detector
}

func TestExtractImageRefsDeployment(t *testing.T) {
	obj := map[string]interface{}{
		"kind": "Deployment",
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{"name": "app", "image": "nginx:1.25"},
						map[string]interface{}{"name": "sidecar", "image": "envoy:1.28"},
					},
					"initContainers": []interface{}{
						map[string]interface{}{"name": "init", "image": "busybox:latest"},
					},
				},
			},
		},
	}

	refs := extractImageRefs(obj)
	assert.ElementsMatch(t, []string{"nginx:1.25", "envoy:1.28", "busybox:latest"}, refs)
}

func TestExtractImageRefsPod(t *testing.T) {
	obj := map[string]interface{}{
		"kind": "Pod",
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{"name": "app", "image": "nginx:latest"},
			},
			"ephemeralContainers": []interface{}{
				map[string]interface{}{"name": "debug", "image": "busybox:latest"},
			},
		},
	}

	refs := extractImageRefs(obj)
	assert.ElementsMatch(t, []string{"nginx:latest", "busybox:latest"}, refs)
}

func TestExtractImageRefsCronJob(t *testing.T) {
	obj := map[string]interface{}{
		"kind": "CronJob",
		"spec": map[string]interface{}{
			"jobTemplate": map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{"name": "job", "image": "batch:v1"},
							},
						},
					},
				},
			},
		},
	}

	refs := extractImageRefs(obj)
	assert.Equal(t, []string{"batch:v1"}, refs)
}

func TestExtractImageRefsDeduplication(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{"name": "a", "image": "nginx:1.25"},
						map[string]interface{}{"name": "b", "image": "nginx:1.25"},
					},
					"initContainers": []interface{}{
						map[string]interface{}{"name": "init", "image": "nginx:1.25"},
					},
				},
			},
		},
	}

	refs := extractImageRefs(obj)
	assert.Equal(t, []string{"nginx:1.25"}, refs)
}

func TestExtractImageRefsEmptyObject(t *testing.T) {
	obj := map[string]interface{}{
		"kind": "ConfigMap",
	}
	refs := extractImageRefs(obj)
	assert.Empty(t, refs)
}

func TestEnricherDisabled(t *testing.T) {
	enricher := NewEnricher(nil, false)
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{"name": "app", "image": "nginx:latest"},
			},
		},
	}

	enricher.Enrich(context.Background(), obj)
	_, exists := obj["imageOsInfo"]
	assert.False(t, exists)
}

func TestEnricherNil(t *testing.T) {
	var enricher *Enricher
	obj := map[string]interface{}{}
	enricher.Enrich(context.Background(), obj)
	_, exists := obj["imageOsInfo"]
	assert.False(t, exists)
}

func TestEnricherEnrich(t *testing.T) {
	server, addr, detector := setupEnricherTestRegistry(t)
	defer server.Close()

	enricher := NewEnricher(detector, true)
	imageRef := fmt.Sprintf("%s/test/nginx:1.25", addr)

	obj := map[string]interface{}{
		"kind": "Deployment",
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{"name": "app", "image": imageRef},
					},
				},
			},
		},
	}

	enricher.Enrich(context.Background(), obj)

	osInfo, exists := obj["imageOsInfo"]
	assert.True(t, exists)

	osInfoMap, ok := osInfo.(map[string]interface{})
	assert.True(t, ok)

	imageInfo, ok := osInfoMap[imageRef].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "debian", imageInfo["id"])
	assert.Equal(t, "12", imageInfo["versionId"])
}

func TestEnricherNoImageRefs(t *testing.T) {
	server, _, detector := setupEnricherTestRegistry(t)
	defer server.Close()

	enricher := NewEnricher(detector, true)
	obj := map[string]interface{}{
		"kind": "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "my-config",
		},
	}

	enricher.Enrich(context.Background(), obj)
	_, exists := obj["imageOsInfo"]
	assert.False(t, exists)
}
