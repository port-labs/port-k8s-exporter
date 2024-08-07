package crdsyncer

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"

	crdSyncerV1alpha1 "github.com/port-labs/port-k8s-exporter/pkg/api/v1alpha1"
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

	// crdController.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
	// If a CRD is added we want to see wheter he matches any of the CRD Syncers and if so to add it
	// AddFunc: func(obj interface{}) {
	// var err error
	// var item EventItem
	// item.ActionType = CreateAction
	// item.Key, err = cache.MetaNamespaceKeyFunc(obj)
	// if err == nil {
	// AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
	// }
	// },
	// If a CRD changed we want to update the relevant blueprint in Port (if there's a mapping that matches the CRD)
	// UpdateFunc: func(old interface{}, new interface{}) {
	// var err error
	// var item EventItem
	// item.ActionType = UpdateAction
	// item.Key, err = cache.MetaNamespaceKeyFunc(new)
	// if err == nil {
	// AutoDiscoverSingleCRDToAction(portConfig, apiExtensionsClient, portClient, item.Key)
	// }
	// },
	// })

	syncerConfigurationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// If there's a new file of CRDSyncer configuration we want to see if there's a CRD that matches it and if so to add it
		AddFunc: func(obj interface{}) {
			if crdSyncerConfiguration, ok := obj.(*crdSyncerV1alpha1.CrdSyncer); ok {
				var err error
				var item EventItem
				item.ActionType = CreateAction
				item.Key, err = cache.MetaNamespaceKeyFunc(obj)
				if err == nil {
					Sync([]crdSyncerV1alpha1.CrdSyncer{*crdSyncerConfiguration}, apiExtensionsClient, portClient, item.Key)
				}
			}

		},
		// If there's an update to the CRDSyncer configuration we want to update the relevant blueprints/automations that affected from it in Port
		UpdateFunc: func(old interface{}, new interface{}) {
			if crdSyncerConfiguration, ok := new.(*crdSyncerV1alpha1.CrdSyncer); ok {
				var err error
				var item EventItem

				item.ActionType = UpdateAction
				item.Key, err = cache.MetaNamespaceKeyFunc(new)
				if err == nil {
					Sync([]crdSyncerV1alpha1.CrdSyncer{*crdSyncerConfiguration}, apiExtensionsClient, portClient, item.Key)
				}
			}
		},
	})
}
