package k8s

import (
	"context"
	"fmt"
	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	k8sfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	noResyncPeriodFunc = func() time.Duration { return 0 }
	blueprint          = "k8s-export-test-bp"
)

type fixture struct {
	t          *testing.T
	controller *Controller
	kubeClient *k8sfake.FakeDynamicClient
}

type fixtureConfig struct {
	portClientId        string
	portClientSecret    string
	stateKey            string
	sendRawDataExamples *bool
	resource            port.Resource
	existingObjects     []runtime.Object
}

func newFixture(t *testing.T, fixtureConfig *fixtureConfig) *fixture {
	defaultTrue := true
	sendRawDataExamples := &defaultTrue
	if fixtureConfig.sendRawDataExamples != nil {
		sendRawDataExamples = fixtureConfig.sendRawDataExamples
	}

	interationConfig := &port.IntegrationAppConfig{
		DeleteDependents:             true,
		CreateMissingRelatedEntities: true,
		SendRawDataExamples:          sendRawDataExamples,
		Resources:                    []port.Resource{fixtureConfig.resource},
	}

	newConfig := &config.ApplicationConfiguration{
		ConfigFilePath:                  config.ApplicationConfig.ConfigFilePath,
		ResyncInterval:                  config.ApplicationConfig.ResyncInterval,
		PortBaseURL:                     config.ApplicationConfig.PortBaseURL,
		EventListenerType:               config.ApplicationConfig.EventListenerType,
		CreateDefaultResources:          config.ApplicationConfig.CreateDefaultResources,
		OverwriteConfigurationOnRestart: config.ApplicationConfig.OverwriteConfigurationOnRestart,
		Resources:                       config.ApplicationConfig.Resources,
		DeleteDependents:                config.ApplicationConfig.DeleteDependents,
		CreateMissingRelatedEntities:    config.ApplicationConfig.CreateMissingRelatedEntities,
		UpdateEntityOnlyOnDiff:          config.ApplicationConfig.UpdateEntityOnlyOnDiff,
		PortClientId:                    config.ApplicationConfig.PortClientId,
		PortClientSecret:                config.ApplicationConfig.PortClientSecret,
		StateKey:                        config.ApplicationConfig.StateKey,
	}

	if fixtureConfig.portClientId != "" {
		newConfig.PortClientId = fixtureConfig.portClientId
	}
	if fixtureConfig.portClientSecret != "" {
		newConfig.PortClientSecret = fixtureConfig.portClientSecret
	}
	if fixtureConfig.stateKey != "" {
		newConfig.StateKey = fixtureConfig.stateKey
	}

	kubeClient := k8sfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), newGvrToListKind(), fixtureConfig.existingObjects...)
	controller := newController(t, fixtureConfig.resource, kubeClient, interationConfig, newConfig)
	_, err := controller.portClient.Authenticate(context.Background(), newConfig.PortClientId, newConfig.PortClientSecret)
	if err != nil {
		t.Errorf("Failed to authenticate with port %v", err)
	}

	return &fixture{
		t:          t,
		controller: controller,
		kubeClient: kubeClient,
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
		TypeMeta: v1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "port-k8s-exporter",
			Namespace: "port-k8s-exporter",
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &v1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
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

func newDeploymentWithCustomLabels(generation int64,
	generateName string,
	creationTimestamp v1.Time,
	labels map[string]string,
) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: v1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:              "port-k8s-exporter",
			Namespace:         "port-k8s-exporter",
			GenerateName:      generateName,
			Generation:        generation,
			CreationTimestamp: creationTimestamp,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &v1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
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

func newGvrToListKind() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}
}

func getGvr(kind string) schema.GroupVersionResource {
	s := strings.SplitN(kind, "/", 3)
	return schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
}

func newController(t *testing.T, resource port.Resource, kubeClient *k8sfake.FakeDynamicClient, integrationConfig *port.IntegrationAppConfig, applicationConfig *config.ApplicationConfiguration) *Controller {
	informerFactory := dynamicinformer.NewDynamicSharedInformerFactory(kubeClient, noResyncPeriodFunc())
	gvr := getGvr(resource.Kind)
	informer := informerFactory.ForResource(gvr)
	kindConfig := port.KindConfig{Selector: resource.Selector, Port: resource.Port}
	controller := NewController(port.AggregatedResource{Kind: resource.Kind, KindConfigs: []port.KindConfig{kindConfig}}, informer, integrationConfig, applicationConfig)
	ctx := context.Background()

	informerFactory.Start(ctx.Done())
	if synced := informerFactory.WaitForCacheSync(ctx.Done()); !synced[gvr] {
		t.Errorf("informer for %s hasn't synced", gvr)
	}

	return controller
}

func (f *fixture) createObjects(t *testing.T, objects []*unstructured.Unstructured) {
	gvr := getGvr(f.controller.Resource.Kind)
	currentNumEventsInQueue := f.controller.eventsWorkqueue.Len()
	if objects != nil {
		for _, d := range objects {
			_, err := f.kubeClient.Resource(gvr).Namespace(d.GetNamespace()).Create(context.TODO(), d, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("error creating object %s: %v", d.GetName(), err)
			}
		}

		for f.controller.eventsWorkqueue.Len() != currentNumEventsInQueue+len(objects) {
		}
	}
}

func (f *fixture) updateObjects(t *testing.T, objects []*unstructured.Unstructured) {
	gvr := getGvr(f.controller.Resource.Kind)
	currentNumEventsInQueue := f.controller.eventsWorkqueue.Len()
	if objects != nil {
		for _, d := range objects {
			_, err := f.kubeClient.Resource(gvr).Namespace(d.GetNamespace()).Update(context.TODO(), d, metav1.UpdateOptions{})
			if err != nil {
				t.Errorf("error updating object %s: %v", d.GetName(), err)
			}
		}

		for f.controller.eventsWorkqueue.Len() != currentNumEventsInQueue+len(objects) {
		}
	}
}

func (f *fixture) deleteObjects(t *testing.T, objects []struct{ namespace, name string }) {
	gvr := getGvr(f.controller.Resource.Kind)
	if objects != nil {
		for _, d := range objects {
			err := f.kubeClient.Resource(gvr).Namespace(d.namespace).Delete(context.TODO(), d.name, metav1.DeleteOptions{})
			if err != nil {
				t.Errorf("error deleting object %s: %v", d.name, err)
			}
		}
	}
}

func (f *fixture) assertSyncResult(result *SyncResult, expectedResult *SyncResult) {
	assert.True(f.t, reflect.DeepEqual(result.EntitiesSet, expectedResult.EntitiesSet), fmt.Sprintf("expected entities set: %v, got: %v", expectedResult.EntitiesSet, result.EntitiesSet))
	assert.True(f.t, reflect.DeepEqual(result.RawDataExamples, expectedResult.RawDataExamples), fmt.Sprintf("expected raw data examples: %v, got: %v", expectedResult.RawDataExamples, result.RawDataExamples))
	assert.True(f.t, result.ShouldDeleteStaleEntities == expectedResult.ShouldDeleteStaleEntities, fmt.Sprintf("expected should delete stale entities: %v, got: %v", expectedResult.ShouldDeleteStaleEntities, result.ShouldDeleteStaleEntities))
}

func (f *fixture) runControllerSyncHandler(item EventItem, expectedResult *SyncResult, expectError bool) {
	syncResult, err := f.controller.syncHandler(item)
	if !expectError && err != nil {
		f.t.Errorf("error syncing item: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing item, got nil")
	}

	f.assertSyncResult(syncResult, expectedResult)
}

func (f *fixture) runControllerInitialSync(expectedResult *SyncResult) {
	syncResult := f.controller.RunInitialSync()

	f.assertSyncResult(syncResult, expectedResult)
}

func (f *fixture) runControllerEventsSync() func() {
	f.controller.RunEventsSync(1, signal.SetupSignalHandler())
	return func() {
		for f.controller.eventsWorkqueue.Len() > 0 {
		}
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

func TestSuccessfulRunInitialSync(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true})
}

func TestRunInitialSyncWithBadMapping(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "bad-jq",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: false})
}

func TestRunEventsSyncWithCreateEvent(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	id := guuid.NewString()
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: fmt.Sprintf("\"%s\"", id),
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
		},
	})

	f := newFixture(t, &fixtureConfig{stateKey: config.ApplicationConfig.StateKey, resource: resource, existingObjects: []runtime.Object{}})
	f.createObjects(t, []*unstructured.Unstructured{ud})
	defer f.controller.portClient.DeleteEntity(context.Background(), id, blueprint, true)
	waitForSync := f.runControllerEventsSync()
	waitForSync()

	assert.Eventually(t, func() bool {
		_, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprint)
		return err == nil
	}, time.Second*5, time.Millisecond*500)
}

func TestRunEventsSyncWithDeleteEvent(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityToBeDeleted\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
		},
	})
	f := newFixture(t, &fixtureConfig{stateKey: config.ApplicationConfig.StateKey, resource: resource, existingObjects: []runtime.Object{ud}})

	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, "entityToBeDeleted"): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true})
	f.deleteObjects(t, []struct{ namespace, name string }{{namespace: d.Namespace, name: d.Name}})

	assert.Eventually(t, func() bool {
		_, err := f.controller.portClient.ReadEntity(context.Background(), "entityToBeDeleted", blueprint)
		return err != nil && strings.Contains(err.Error(), "was not found")
	}, time.Second*5, time.Millisecond*500)

}

func TestCreateDeployment(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestCreateDeploymentWithSearchRelation(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": map[string]interface{}{
					"combinator": "\"or\"",
					"rules": []interface{}{
						map[string]interface{}{
							"property": "\"$identifier\"",
							"operator": "\"=\"",
							"value":    "\"e_AgPMYvq1tAs8TuqM\"",
						},
						map[string]interface{}{
							"property": "\"$identifier\"",
							"operator": "\"=\"",
							"value":    ".metadata.name",
						},
					},
				},
			},
		},
	})
	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestUpdateDeployment(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "[\"Test\", \"Test2\"]",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: UpdateAction}

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestDeleteDeploymentSameOwner(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithSameOwner\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, &fixtureConfig{stateKey: config.ApplicationConfig.StateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(createItem, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;entityWithSameOwner", blueprint): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithSameOwner", blueprint)
	if err != nil && !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to be deleted")
	}
}

func TestDeleteDeploymentDifferentOwner(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithDifferentOwner\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, &fixtureConfig{stateKey: "non_exist_statekey", resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(createItem, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;entityWithDifferentOwner", blueprint): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithDifferentOwner", blueprint)
	if err != nil && strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to exist")
	}
}

func TestSelectorQueryFilterDeployment(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource(".metadata.name != \"port-k8s-exporter\"", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"wrong-%s\"", blueprint),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: true}, false)
}

func TestFailPortAuth(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}

	f := newFixture(t, &fixtureConfig{portClientId: "wrongclientid", portClientSecret: "wrongclientsecret", resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: nil, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: false}, true)
}

func TestFailDeletePortEntity(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"wrong-%s\"", blueprint),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}

	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestUpdateHandlerWithIndividualPropertyChanges(t *testing.T) {

	type Property struct {
		Value           interface{}
		ShouldSendEvent bool
	}

	fullMapping := []port.Resource{
		newResource("", []port.EntityMapping{
			{
				Identifier: ".metadata.name",
				Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
				Icon:       "\"Microservice\"",
				Team:       "\"Test\"",
				Properties: map[string]string{
					"labels":            ".spec.selector",
					"generation":        ".metadata.generation",
					"generateName":      ".metadata.generateName",
					"creationTimestamp": ".metadata.creationTimestamp",
				},
				Relations: map[string]interface{}{
					"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
				},
			},
		}),
		newResource("", []port.EntityMapping{

			{
				Identifier: ".metadata.name",
				Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
				Icon:       "\"Microservice\"",
				Team:       "\"Test\"",
				Properties: map[string]string{},
				Relations:  map[string]interface{}{},
			},
			{
				Identifier: ".metadata.name",
				Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
				Icon:       "\"Microservice\"",
				Team:       "\"Test\"",
				Properties: map[string]string{
					"labels":            ".spec.selector",
					"generation":        ".metadata.generation",
					"generateName":      ".metadata.generateName",
					"creationTimestamp": ".metadata.creationTimestamp",
				},
				Relations: map[string]interface{}{
					"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
				},
			},
		}),
	}

	for _, mapping := range fullMapping {

		controllerWithFullMapping := newFixture(t, &fixtureConfig{resource: mapping, existingObjects: []runtime.Object{}}).controller

		// Test changes in each individual property
		properties := map[string]Property{
			"metadata.name":              {Value: "port-k8s-exporter", ShouldSendEvent: false},
			"something_without_mapping":  {Value: "port-k8s-exporter", ShouldSendEvent: false},
			"metadata.generation":        {Value: int64(3), ShouldSendEvent: true},
			"metadata.generateName":      {Value: "new-port-k8s-exporter2", ShouldSendEvent: true},
			"metadata.creationTimestamp": {Value: v1.Now().Add(1 * time.Hour).Format(time.RFC3339), ShouldSendEvent: true},
		}

		for property, value := range properties {
			newDep := newUnstructured(newDeploymentWithCustomLabels(2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": "port-k8s-exporter"}))
			oldDep := newUnstructured(newDeploymentWithCustomLabels(2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": "port-k8s-exporter"}))

			// Update the property in the new deployment
			unstructured.SetNestedField(newDep.Object, value.Value, strings.Split(property, ".")...)

			result := controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, true)
			if value.ShouldSendEvent {
				assert.True(t, result, fmt.Sprintf("Expected true when %s changes and feature flag is on", property))
			} else {
				assert.False(t, result, fmt.Sprintf("Expected false when %s changes and feature flag is on", property))
			}
			result = controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, false)
			assert.True(t, result, fmt.Sprintf("Expected true when %s changes and feature flag is off", property))
		}

		// Add a case for json update because you can't edit the json directly
		newDep := newUnstructured(newDeploymentWithCustomLabels(2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": "port-k8s-exporter"}))
		oldDep := newUnstructured(newDeploymentWithCustomLabels(2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": "new-port-k8s-exporter"}))

		result := controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, true)
		assert.True(t, result, fmt.Sprintf("Expected true when labels changes and feature flag is on"))
		result = controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, false)
		assert.True(t, result, fmt.Sprintf("Expected true when labels changes and feature flag is off"))
	}
}

func TestCreateDeploymentWithSearchIdentifier(t *testing.T) {
	d := newDeployment()
	ud := newUnstructured(d)
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: map[string]interface{}{
				"combinator": "\"and\"",
				"rules": []interface{}{
					map[string]interface{}{
						"property": "\"text\"",
						"operator": "\"=\"",
						"value":    "\"pod\"",
					},
				}},
			Blueprint: fmt.Sprintf("\"%s\"", blueprint),
			Icon:      "\"Microservice\"",
			Team:      "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	f := newFixture(t, &fixtureConfig{resource: resource, existingObjects: []runtime.Object{ud}})
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprint, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	deleteItem := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f.runControllerSyncHandler(deleteItem, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}
