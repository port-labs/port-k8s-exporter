package k8s

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"
	"strings"
)

func GetGVRFromResource(discoveryMapper *restmapper.DeferredDiscoveryRESTMapper, resource string) (schema.GroupVersionResource, error) {
	var gvr schema.GroupVersionResource

	if strings.Count(resource, "/") >= 2 {
		s := strings.SplitN(resource, "/", 3)
		gvr = schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
	} else if strings.Count(resource, "/") == 1 {
		s := strings.SplitN(resource, "/", 2)
		gvr = schema.GroupVersionResource{Group: "", Version: s[0], Resource: s[1]}
	}

	gvrs, err := discoveryMapper.ResourcesFor(gvr)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	if len(gvrs) == 0 {
		return gvr, nil
	}

	return gvrs[0], nil
}
