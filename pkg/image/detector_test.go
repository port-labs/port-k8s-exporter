package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/assert"
)

func buildGzipTar(files map[string][]byte) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, data := range files {
		_ = tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(data)),
		})
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

func buildLayerWithOsRelease(content string) (v1.Layer, error) {
	data := buildGzipTar(map[string][]byte{
		"etc/os-release": []byte(content),
	})
	return tarball.LayerFromReader(bytes.NewReader(data))
}

func buildEmptyLayer() (v1.Layer, error) {
	data := buildGzipTar(map[string][]byte{
		"tmp/hello.txt": []byte("hello"),
	})
	return tarball.LayerFromReader(bytes.NewReader(data))
}

func setupTestRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	reg := registry.New()
	server := httptest.NewServer(reg)
	return server, server.Listener.Addr().String()
}

func pushImage(t *testing.T, registryAddr string, repo string, tag string, layers ...v1.Layer) {
	t.Helper()
	img := empty.Image
	for _, layer := range layers {
		var err error
		img, err = mutate.Append(img, mutate.Addendum{Layer: layer})
		assert.NoError(t, err)
	}

	ref, err := name.ParseReference(fmt.Sprintf("%s/%s:%s", registryAddr, repo, tag))
	assert.NoError(t, err)

	err = remote.Write(ref, img, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	assert.NoError(t, err)
}

func TestDetectorWithOsRelease(t *testing.T) {
	server, addr := setupTestRegistry(t)
	defer server.Close()

	osReleaseContent := `ID=debian
VERSION_ID="12"
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
HOME_URL="https://www.debian.org/"
`
	layer, err := buildLayerWithOsRelease(osReleaseContent)
	assert.NoError(t, err)

	pushImage(t, addr, "test/myapp", "v1", layer)

	detector := NewDetector(authn.DefaultKeychain)
	info, err := detector.Detect(context.Background(), fmt.Sprintf("%s/test/myapp:v1", addr))
	assert.NoError(t, err)
	assert.Equal(t, "debian", info.ID)
	assert.Equal(t, "12", info.VersionID)
	assert.Equal(t, "Debian GNU/Linux 12 (bookworm)", info.PrettyName)
	assert.Equal(t, "https://www.debian.org/", info.HomeURL)
}

func TestDetectorCacheHit(t *testing.T) {
	server, addr := setupTestRegistry(t)
	defer server.Close()

	layer, err := buildLayerWithOsRelease("ID=alpine\nVERSION_ID=3.19\n")
	assert.NoError(t, err)

	pushImage(t, addr, "test/cached", "v1", layer)

	detector := NewDetector(authn.DefaultKeychain)
	ref := fmt.Sprintf("%s/test/cached:v1", addr)

	info1, err := detector.Detect(context.Background(), ref)
	assert.NoError(t, err)
	assert.Equal(t, "alpine", info1.ID)

	// Second call should return cached result
	info2, err := detector.Detect(context.Background(), ref)
	assert.NoError(t, err)
	assert.Equal(t, info1, info2)
}

func TestDetectorInvalidRef(t *testing.T) {
	detector := NewDetector(authn.DefaultKeychain)
	_, err := detector.Detect(context.Background(), "!!!invalid!!!")
	assert.Error(t, err)
}

func TestDetectorNoOsRelease(t *testing.T) {
	server, addr := setupTestRegistry(t)
	defer server.Close()

	layer, err := buildEmptyLayer()
	assert.NoError(t, err)

	pushImage(t, addr, "test/scratch", "v1", layer)

	detector := NewDetector(authn.DefaultKeychain)
	info, err := detector.Detect(context.Background(), fmt.Sprintf("%s/test/scratch:v1", addr))
	assert.NoError(t, err)
	assert.Equal(t, &OsInfo{}, info)
}

func TestDetectBatch(t *testing.T) {
	server, addr := setupTestRegistry(t)
	defer server.Close()

	debLayer, err := buildLayerWithOsRelease("ID=debian\nVERSION_ID=12\n")
	assert.NoError(t, err)
	alpLayer, err := buildLayerWithOsRelease("ID=alpine\nVERSION_ID=3.19\n")
	assert.NoError(t, err)

	pushImage(t, addr, "test/deb", "v1", debLayer)
	pushImage(t, addr, "test/alp", "v1", alpLayer)

	detector := NewDetector(authn.DefaultKeychain)
	refs := []string{
		fmt.Sprintf("%s/test/deb:v1", addr),
		fmt.Sprintf("%s/test/alp:v1", addr),
	}

	results := detector.DetectBatch(context.Background(), refs)
	assert.Len(t, results, 2)
	assert.Equal(t, "debian", results[refs[0]].ID)
	assert.Equal(t, "alpine", results[refs[1]].ID)
}
