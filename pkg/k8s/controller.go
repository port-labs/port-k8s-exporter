package k8s

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/mapping"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type EventItemType string

const (
	CreateEventItem EventItemType = "create"
	UpdateEventItem EventItemType = "update"
	DeleteEventItem EventItemType = "delete"
	MaxNumRequeues  int           = 4
)

type EventItem struct {
	Key  string
	Type EventItemType
}

type Controller struct {
	resource   config.Resource
	portClient *cli.PortClient
	informer   cache.SharedIndexInformer
	workqueue  workqueue.RateLimitingInterface
}

func NewController(resource config.Resource, portClient *cli.PortClient, informer cache.SharedIndexInformer) *Controller {
	controller := &Controller{
		resource:   resource,
		portClient: portClient,
		informer:   informer,
		workqueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.Type = CreateEventItem
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(item)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			var err error
			var item EventItem
			item.Type = UpdateEventItem
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				controller.workqueue.Add(item)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.Type = DeleteEventItem
			item.Key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				err = controller.objectHandler(obj, item)
				if err != nil {
					klog.Errorf("Error deleting item '%s' of resource '%s': %s", item.Key, resource.Kind, err.Error())
				}
			}
		},
	})

	return controller
}

func (c *Controller) Run(workers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	klog.Infof("Waiting for informer cache to sync for resource '%s'", c.resource.Kind)
	if ok := cache.WaitForCacheSync(stopCh, c.informer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Infof("Starting workers for resource '%s'", c.resource.Kind)
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	klog.Infof("Started workers for resource '%s'", c.resource.Kind)

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)

		item, ok := obj.(EventItem)

		if !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected event item of resource '%s' in workqueue but got %#v", c.resource.Kind, obj))
			return nil
		}

		if err := c.syncHandler(item); err != nil {
			if c.workqueue.NumRequeues(obj) >= MaxNumRequeues {
				utilruntime.HandleError(fmt.Errorf("error syncing '%s' of resource '%s': %s, give up after %d requeues", item.Key, c.resource.Kind, err.Error(), MaxNumRequeues))
				return nil
			}

			c.workqueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s' of resource '%s': %s, requeuing", item.Key, c.resource.Kind, err.Error())
		}

		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) syncHandler(item EventItem) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
	if err != nil {
		return fmt.Errorf("error fetching object with key '%s' from informer cache: %v", item.Key, err)
	}
	if !exists {
		utilruntime.HandleError(fmt.Errorf("'%s' in work queue no longer exists", item.Key))
		return nil
	}

	err = c.objectHandler(obj, item)
	if err != nil {
		return fmt.Errorf("error handling object with key '%s': %v", item.Key, err)
	}

	return nil
}

func (c *Controller) objectHandler(obj interface{}, item EventItem) error {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("error casting to unstructured for key: %s", item.Key))
		return nil
	}
	var structuredObj interface{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &structuredObj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error converting from unstructured for key: %s", item.Key))
		return nil
	}

	var selectorResult = true
	if c.resource.Selector.Query != "" {
		selectorResult, err = jq.ParseBool(c.resource.Selector.Query, structuredObj)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("invalid selector query '%s': %v", c.resource.Selector.Query, err))
			return nil
		}
	}
	if !selectorResult {
		klog.Infof("Selector query evaluated to false, skip syncing '%s' of resource '%s'", item.Key, c.resource.Kind)
		return nil
	}

	_, err = c.portClient.Authenticate(context.Background(), c.portClient.ClientID, c.portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	for _, entityMapping := range c.resource.Port.Entity.Mappings {
		var portEntity *port.Entity
		portEntity, err = mapping.NewEntity(structuredObj, entityMapping)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("invalid entity mapping '%#v': %v", entityMapping, err))
			return nil
		}

		switch item.Type {
		case CreateEventItem, UpdateEventItem:
			_, err = c.portClient.CreateEntity(context.Background(), portEntity, "")
			if err != nil {
				return fmt.Errorf("error upserting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
			}
			klog.Infof("Successfully upserted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		case DeleteEventItem:
			err = c.portClient.DeleteEntity(context.Background(), portEntity.Identifier, portEntity.Blueprint)
			if err != nil {
				return fmt.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
			}
			klog.Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		}
	}

	return nil
}

func (c *Controller) Shutdown() {
	klog.Infof("Shutting down controller for resource '%s'", c.resource.Kind)
	c.workqueue.ShutDown()
	klog.Infof("Closed controller for resource '%s'", c.resource.Kind)
}
