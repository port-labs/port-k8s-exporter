package k8s

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	DiscoveryClient *discovery.DiscoveryClient
	DynamicClient   dynamic.Interface
	DiscoveryMapper *restmapper.DeferredDiscoveryRESTMapper
}

func getKubeConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return config, nil
}

func NewClient() (*Client, error) {
	kubeConfig, err := getKubeConfig()
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	cacheClient := memory.NewMemCacheClient(discoveryClient)
	cacheClient.Invalidate()

	discoveryMapper := restmapper.NewDeferredDiscoveryRESTMapper(cacheClient)

	return &Client{discoveryClient, dynamicClient, discoveryMapper}, nil
}
