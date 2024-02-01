package k8s

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/dynamic/fake"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var (
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t          *testing.T
	controller *Controller
}

func newFixture(t *testing.T, portClientId string, portClientSecret string, userAgent string, resource port.Resource, objects []runtime.Object) *fixture {
	kubeclient := k8sfake.NewSimpleDynamicClient(runtime.NewScheme())

	if portClientId == "" {
		portClientId = config.ApplicationConfig.PortClientId
	}
	if portClientSecret == "" {
		portClientSecret = config.ApplicationConfig.PortClientSecret
	}
	if userAgent == "" {
		userAgent = "port-k8s-exporter/0.1"
	}

	portClient, err := cli.New(config.ApplicationConfig.PortBaseURL, cli.WithHeader("User-Agent", userAgent),
		cli.WithClientID(portClientId), cli.WithClientSecret(portClientSecret))
	if err != nil {
		t.Errorf("Error building Port client: %s", err.Error())
	}

	return &fixture{
		t:          t,
		controller: newController(resource, objects, portClient, kubeclient),
	}
}

func newResource(selectorQuery string, mappings []port.EntityMapping) port.Resource {
	return port.Resource{
		Kind: "apps/v1/deployments",
		Selector: port.Selector{
			Query: selectorQuery,
		},
		Port: port.Port{
			Entity: port.EntityMappings{
				Mappings: mappings,
			},
		},
	}
}

func newDeployment() *appsv1.Deployment {
	labels := map[string]string{
		"app": "port-k8s-exporter",
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "port-k8s-exporter",
			Namespace: "port-k8s-exporter",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "port-k8s-exporter",
							Image: "port-k8s-exporter:latest",
						},
					},
				},
			},
		},
	}
}

func newUnstructured(obj interface{}) *unstructured.Unstructured {
	res, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: res}
}

func newController(resource port.Resource, objects []runtime.Object, portClient *cli.PortClient, kubeclient *k8sfake.FakeDynamicClient) *Controller {
	k8sI := dynamicinformer.NewDynamicSharedInformerFactory(kubeclient, noResyncPeriodFunc())
	s := strings.SplitN(resource.Kind, "/", 3)
	gvr := schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
	informer := k8sI.ForResource(gvr)
	kindConfig := port.KindConfig{Selector: resource.Selector, Port: resource.Port}
	c := NewController(port.AggregatedResource{Kind: resource.Kind, KindConfigs: []port.KindConfig{kindConfig}}, portClient, informer)

	for _, d := range objects {
		informer.Informer().GetIndexer().Add(d)
	}

	return c
}

func (f *fixture) runControllerSyncHandler(item EventItem, expectError bool) {
	err := f.controller.syncHandler(item)
	if !expectError && err != nil {
		f.t.Errorf("error syncing item: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing item, got nil")
	}

}

func (f *fixture) runControllerGetEntitiesSet(expectedEntitiesSet map[string]interface{}, expectError bool) {
	entitiesSet, err := f.controller.GetEntitiesSet()
	if !expectError && err != nil {
		f.t.Errorf("error syncing item: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing item, got nil")
	}

	eq := reflect.DeepEqual(entitiesSet, expectedEntitiesSet)
	if !eq {
		f.t.Errorf("expected entities set: %v, got: %v", expectedEntitiesSet, entitiesSet)
	}
}

func getKey(deployment *appsv1.Deployment, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(deployment)
	if err != nil {
		t.Errorf("Unexpected error getting key for deployment %v: %v", deployment.Name, err)
		return ""
	}
	return key
}

func TestCreateDeployment(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]string{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}

	f := newFixture(t, "", "", "", resource, objects)
	f.runControllerSyncHandler(item, false)
}

func TestUpdateDeployment(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
			Icon:       "\"Microservice\"",
			Team:       "[\"Test\", \"Test2\"]",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]string{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: UpdateAction}

	f := newFixture(t, "", "", "", resource, objects)
	f.runControllerSyncHandler(item, false)
}

func TestDeleteDeploymentSameOwner(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithSameOwner\"",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, "", "", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", config.ApplicationConfig.StateKey), resource, objects)
	f.runControllerSyncHandler(createItem, false)

	f.runControllerSyncHandler(item, false)
	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithSameOwner", "k8s-export-test-bp")
	if !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to be deleted")
	}
}

func TestDeleteDeploymentDifferentOwner(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithDifferentOwner\"",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, "", "", fmt.Sprintf("statekey/%s", "non_exist_statekey")+"port-k8s-exporter", resource, objects)
	f.runControllerSyncHandler(createItem, false)

	f.runControllerSyncHandler(item, false)
	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithDifferentOwner", "k8s-export-test-bp")
	if err != nil && strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to exist")
	}
}

func TestSelectorQueryFilterDeployment(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource(".metadata.name != \"port-k8s-exporter\"", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"wrong-k8s-export-test-bp\"",
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, "", "", "", resource, objects)
	f.runControllerSyncHandler(item, false)
}

func TestFailPortAuth(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}

	f := newFixture(t, "wrongclientid", "wrongclientsecret", "", resource, objects)
	f.runControllerSyncHandler(item, true)
}

func TestFailDeletePortEntity(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"wrong-k8s-export-test-bp\"",
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, "", "", "", resource, objects)
	f.runControllerSyncHandler(item, false)
}

func TestGetEntitiesSet(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	})
	expectedEntitiesSet := map[string]interface{}{
		"k8s-export-test-bp;port-k8s-exporter": nil,
	}

	f := newFixture(t, "", "", "", resource, objects)
	f.runControllerGetEntitiesSet(expectedEntitiesSet, false)
}
