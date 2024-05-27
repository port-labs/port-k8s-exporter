package crdsyncer

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type EventActionType string

const (
	CreateAction   EventActionType = "create"
	UpdateAction   EventActionType = "update"
	DeleteAction   EventActionType = "delete"
	MaxNumRequeues int             = 4
)

type EventItem struct {
	Key        string
	ActionType EventActionType
}

type Controller struct {
	portClient *cli.PortClient
	informer   cache.SharedIndexInformer
	workqueue  workqueue.RateLimitingInterface
}

func InitalizeCRDSyncerControllers(portClient *cli.PortClient, portConfig *port.IntegrationAppConfig, apiExtensionsClient apiextensions.ApiextensionsV1Interface, informersFactory dynamicinformer.DynamicSharedInformerFactory) {
	crdInformer := informersFactory.ForResource(schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"})
	syncerConfigurationInformer := informersFactory.ForResource(schema.GroupVersionResource{Group: "getport.io", Version: "v1", Resource: "crdsyncers"})

	crdController := &Controller{
		portClient: portClient,
		informer:   crdInformer.Informer(),
		workqueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	crdController.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.ActionType = CreateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			var err error
			var item EventItem
			item.ActionType = UpdateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
			}
		},
	})

	syncerConfigurationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.ActionType = CreateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			var err error
			var item EventItem
			item.ActionType = UpdateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
			}
		},
	})
}
