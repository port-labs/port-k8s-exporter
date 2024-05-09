package k8s

import (
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

type Client struct {
	DiscoveryClient    *discovery.DiscoveryClient
	DynamicClient      dynamic.Interface
	DiscoveryMapper    *restmapper.DeferredDiscoveryRESTMapper
	ApiExtensionClient *apiextensions.ApiextensionsV1Client
}

func NewClient(config *rest.Config) (*Client, error) {

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	apiextensionsClient, err := apiextensions.NewForConfig(config)

	cacheClient := memory.NewMemCacheClient(discoveryClient)
	cacheClient.Invalidate()

	discoveryMapper := restmapper.NewDeferredDiscoveryRESTMapper(cacheClient)

	return &Client{discoveryClient, dynamicClient, discoveryMapper, apiextensionsClient}, nil
}
