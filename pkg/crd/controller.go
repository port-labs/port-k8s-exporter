package crd

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/client-go/informers"

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

func NewCrdController(portClient *cli.PortClient, informer informers.GenericInformer, portConfig *port.IntegrationAppConfig, apiExtensionsClient apiextensions.ApiextensionsV1Interface) *Controller {
	controller := &Controller{
		portClient: portClient,
		informer:   informer.Informer(),
		workqueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	controller.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
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

	return controller
}
