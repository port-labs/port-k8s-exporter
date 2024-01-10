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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
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
	resource   port.AggregatedResource
	portClient *cli.PortClient
	informer   cache.SharedIndexInformer
	lister     cache.GenericLister
	workqueue  workqueue.RateLimitingInterface
}

func NewController(resource port.AggregatedResource, portClient *cli.PortClient, informer informers.GenericInformer) *Controller {
	controller := &Controller{
		resource:   resource,
		portClient: portClient,
		informer:   informer.Informer(),
		lister:     informer.Lister(),
		workqueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	controller.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.ActionType = CreateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(item)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			var err error
			var item EventItem
			item.ActionType = UpdateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				controller.workqueue.Add(item)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.ActionType = DeleteAction
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

func (c *Controller) Shutdown() {
	klog.Infof("Shutting down controller for resource '%s'", c.resource.Kind)
	c.workqueue.ShutDown()
	klog.Infof("Closed controller for resource '%s'", c.resource.Kind)
}

func (c *Controller) WaitForCacheSync(stopCh <-chan struct{}) error {
	klog.Infof("Waiting for informer cache to sync for resource '%s'", c.resource.Kind)
	if ok := cache.WaitForCacheSync(stopCh, c.informer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}

func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting workers for resource '%s'", c.resource.Kind)
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	klog.Infof("Started workers for resource '%s'", c.resource.Kind)
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
	_, err := c.portClient.Authenticate(context.Background(), c.portClient.ClientID, c.portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	errors := make([]error, 0)
	for _, kindConfig := range c.resource.KindConfigs {
		portEntities, err := c.getObjectEntities(obj, kindConfig.Selector, kindConfig.Port.Entity.Mappings)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("error getting entities for object key '%s': %v", item.Key, err))
			continue
		}

		for _, portEntity := range portEntities {
			err = c.entityHandler(portEntity, item.ActionType)
			if err != nil {
				errors = append(errors, err)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("error handling entity for object key '%s': %v", item.Key, errors)
	}

	return nil
}

func (c *Controller) getObjectEntities(obj interface{}, selector port.Selector, mappings []port.EntityMapping) ([]port.Entity, error) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("error casting to unstructured")
	}
	var structuredObj interface{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &structuredObj)
	if err != nil {
		return nil, fmt.Errorf("error converting from unstructured: %v", err)
	}

	var selectorResult = true
	if selector.Query != "" {
		selectorResult, err = jq.ParseBool(selector.Query, structuredObj)
		if err != nil {
			return nil, fmt.Errorf("invalid selector query '%s': %v", selector.Query, err)
		}
	}
	if !selectorResult {
		return nil, nil
	}

	entities := make([]port.Entity, 0, len(mappings))
	for _, entityMapping := range mappings {
		var portEntity *port.Entity
		portEntity, err = mapping.NewEntity(structuredObj, entityMapping)
		if err != nil {
			return nil, fmt.Errorf("invalid entity mapping '%#v': %v", entityMapping, err)
		}
		entities = append(entities, *portEntity)
	}

	return entities, nil
}

func checkIfOwnEntity(entity port.Entity, portClient *cli.PortClient) (*bool, error) {
	portEntities, err := portClient.SearchEntities(context.Background(), port.SearchBody{
		Rules: []port.Rule{
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    "port-k8s-exporter",
			},
			{
				Property: "$identifier",
				Operator: "=",
				Value:    entity.Identifier,
			},
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    fmt.Sprintf("statekey/%s", config.ApplicationConfig.StateKey),
			},
		},
		Combinator: "and",
	})
	if err != nil {
		return nil, err
	}

	if len(portEntities) > 0 {
		result := true
		return &result, nil
	}
	result := false
	return &result, nil
}

func (c *Controller) entityHandler(portEntity port.Entity, action EventActionType) error {
	switch action {
	case CreateAction, UpdateAction:
		_, err := c.portClient.CreateEntity(context.Background(), &portEntity, "", c.portClient.CreateMissingRelatedEntities)
		if err != nil {
			return fmt.Errorf("error upserting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
		}
		klog.V(0).Infof("Successfully upserted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
	case DeleteAction:
		result, err := checkIfOwnEntity(portEntity, c.portClient)
		if err != nil {
			return fmt.Errorf("error checking if entity '%s' of blueprint '%s' is owned by this exporter: %v", portEntity.Identifier, portEntity.Blueprint, err)
		}

		if *result {
			err := c.portClient.DeleteEntity(context.Background(), portEntity.Identifier, portEntity.Blueprint, c.portClient.DeleteDependents)
			if err != nil {
				return fmt.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
			}
			klog.V(0).Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		} else {
			klog.Warningf("trying to delete entity but didn't find it in port with k8s ownership", portEntity.Identifier, portEntity.Blueprint)
		}

	}

	return nil
}

func (c *Controller) GetEntitiesSet() (map[string]interface{}, error) {
	k8sEntitiesSet := map[string]interface{}{}
	objects, err := c.lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("error listing K8s objects of resource '%s': %v", c.resource.Kind, err)
	}

	for _, obj := range objects {
		for _, kindConfig := range c.resource.KindConfigs {
			mappings := make([]port.EntityMapping, 0, len(kindConfig.Port.Entity.Mappings))
			for _, m := range kindConfig.Port.Entity.Mappings {
				mappings = append(mappings, port.EntityMapping{
					Identifier: m.Identifier,
					Blueprint:  m.Blueprint,
				})
			}
			entities, err := c.getObjectEntities(obj, kindConfig.Selector, mappings)
			if err != nil {
				return nil, fmt.Errorf("error getting entities of object: %v", err)
			}
			for _, entity := range entities {
				k8sEntitiesSet[c.portClient.GetEntityIdentifierKey(&entity)] = nil
			}
		}
	}

	return k8sEntitiesSet, nil
}
