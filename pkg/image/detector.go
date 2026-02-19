package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Detector struct {
	keychain authn.Keychain
	cache    sync.Map
}

func NewDetector(keychain authn.Keychain) *Detector {
	return &Detector{
		keychain: keychain,
	}
}

func (d *Detector) Detect(ctx context.Context, imageRef string) (*OsInfo, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("error parsing image reference %q: %v", imageRef, err)
	}

	desc, err := remote.Get(ref, remote.WithAuthFromKeychain(d.keychain), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error fetching image descriptor for %q: %v", imageRef, err)
	}

	digest := desc.Digest.String()

	if cached, ok := d.cache.Load(digest); ok {
		return cached.(*OsInfo), nil
	}

	img, err := desc.Image()
	if err != nil {
		return nil, fmt.Errorf("error getting image for %q: %v", imageRef, err)
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("error getting layers for %q: %v", imageRef, err)
	}

	// Search layers top-down (last layer first) for os-release
	for i := len(layers) - 1; i >= 0; i-- {
		rc, err := layers[i].Compressed()
		if err != nil {
			continue
		}

		info, err := findOsReleaseInLayer(rc)
		rc.Close()
		if err != nil {
			continue
		}
		if info != nil {
			d.cache.Store(digest, info)
			return info, nil
		}
	}

	// Cache empty result for scratch images to avoid re-fetch
	empty := &OsInfo{}
	d.cache.Store(digest, empty)
	return empty, nil
}

const maxConcurrentDetections = 10

func (d *Detector) DetectBatch(ctx context.Context, imageRefs []string) map[string]*OsInfo {
	results := make(map[string]*OsInfo)
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, maxConcurrentDetections)

	for _, ref := range imageRefs {
		wg.Add(1)
		go func(imageRef string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := d.Detect(ctx, imageRef)
			if err != nil {
				return
			}
			mu.Lock()
			results[imageRef] = info
			mu.Unlock()
		}(ref)
	}

	wg.Wait()
	return results
}

func findOsReleaseInLayer(compressed io.Reader) (*OsInfo, error) {
	gr, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Skip non-regular files (symlinks, directories, etc.)
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		cleanName := strings.TrimPrefix(hdr.Name, "./")
		cleanName = strings.TrimPrefix(cleanName, "/")

		if cleanName == "etc/os-release" || cleanName == "usr/lib/os-release" {
			content, err := io.ReadAll(io.LimitReader(tr, 1<<20)) // 1MB limit
			if err != nil {
				return nil, err
			}
			if len(content) == 0 {
				continue
			}
			return ParseOsRelease(bytes.NewReader(content))
		}
	}

	return nil, nil
}
