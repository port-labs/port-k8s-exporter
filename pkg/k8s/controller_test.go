package k8s

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/stretchr/testify/assert"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/dynamic/fake"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type fixtureConfig struct {
	portClientId        string
	portClientSecret    string
	stateKey            string
	sendRawDataExamples *bool
	resource            port.Resource
	objects             []runtime.Object
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
	kubeclient := k8sfake.NewSimpleDynamicClient(runtime.NewScheme())

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

	return &fixture{
		t:          t,
		controller: newController(fixtureConfig.resource, fixtureConfig.objects, kubeclient, interationConfig, newConfig),
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

func newController(resource port.Resource, objects []runtime.Object, kubeclient *k8sfake.FakeDynamicClient, integrationConfig *port.IntegrationAppConfig, applicationConfig *config.ApplicationConfiguration) *Controller {
	k8sI := dynamicinformer.NewDynamicSharedInformerFactory(kubeclient, noResyncPeriodFunc())
	s := strings.SplitN(resource.Kind, "/", 3)
	gvr := schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
	informer := k8sI.ForResource(gvr)
	kindConfig := port.KindConfig{Selector: resource.Selector, Port: resource.Port}
	c := NewController(port.AggregatedResource{Kind: resource.Kind, KindConfigs: []port.KindConfig{kindConfig}}, informer, integrationConfig, applicationConfig)

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

func (f *fixture) runControllerGetEntitiesSet(expectedEntitiesSet map[string]interface{}, expectedExamples []interface{}, expectError bool) {
	entitiesSet, examples, err := f.controller.GetEntitiesSet()
	if !expectError && err != nil {
		f.t.Errorf("error syncing item: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing item, got nil")
	}

	eq := reflect.DeepEqual(entitiesSet, expectedEntitiesSet)
	if !eq {
		f.t.Errorf("expected entities set: %v, got: %v", expectedEntitiesSet, entitiesSet)
	}

	eq = reflect.DeepEqual(examples, expectedExamples)
	if !eq {
		f.t.Errorf("expected raw data examples: %v, got: %v", expectedExamples, examples)
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
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}

	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
	f.runControllerSyncHandler(item, false)
}

func TestJqSearchRelation(t *testing.T) {

	mapping := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{},
			Relations: map[string]interface{}{
				"k8s-relation": map[string]interface{}{
					"combinator": "\"or\"",
					"rules": []interface{}{
						map[string]interface{}{
							"property": "\"$identifier\"",
							"operator": "\"=\"",
							"value":    "\"e_AgPMYvq1tAs8TuqM\"",
						},
					},
				},
			},
		},
	}
	res, _ := jq.ParseRelations(mapping[0].Relations, nil)
	assert.Equal(t, res, map[string]interface{}{
		"k8s-relation": map[string]interface{}{
			"combinator": "or",
			"rules": []interface{}{
				map[string]interface{}{
					"property": "$identifier",
					"operator": "=",
					"value":    "e_AgPMYvq1tAs8TuqM",
				},
			},
		},
	})

}

func TestCreateDeploymentWithSearchRelation(t *testing.T) {
	d := newDeployment()
	objects := []runtime.Object{newUnstructured(d)}
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
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
	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
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
			Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			},
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: UpdateAction}

	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
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

	f := newFixture(t, &fixtureConfig{stateKey: config.ApplicationConfig.StateKey, resource: resource, objects: objects})
	f.runControllerSyncHandler(createItem, false)

	f.runControllerSyncHandler(item, false)
	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithSameOwner", "k8s-export-test-bp")
	if err != nil && !strings.Contains(err.Error(), "was not found") {
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

	f := newFixture(t, &fixtureConfig{stateKey: "non_exist_statekey", resource: resource, objects: objects})
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

	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
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

	f := newFixture(t, &fixtureConfig{portClientId: "wrongclientid", portClientSecret: "wrongclientsecret", resource: resource, objects: objects})
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

	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
	f.runControllerSyncHandler(item, false)
}

func TestGetEntitiesSet(t *testing.T) {
	d := newUnstructured(newDeployment())
	var structuredObj interface{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(d.Object, &structuredObj)
	if err != nil {
		t.Errorf("Error from unstructured: %s", err.Error())
	}

	objects := []runtime.Object{d}
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	})
	expectedEntitiesSet := map[string]interface{}{
		"k8s-export-test-bp;port-k8s-exporter": nil,
	}

	f := newFixture(t, &fixtureConfig{resource: resource, objects: objects})
	f.runControllerGetEntitiesSet(expectedEntitiesSet, []interface{}{structuredObj}, false)

	sendRawDataExamples := false
	f = newFixture(t, &fixtureConfig{sendRawDataExamples: &sendRawDataExamples, resource: resource, objects: objects})
	f.runControllerGetEntitiesSet(expectedEntitiesSet, []interface{}{}, false)
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
				Blueprint:  "\"k8s-export-test-bp\"",
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
				Blueprint:  "\"k8s-export-test-bp\"",
				Icon:       "\"Microservice\"",
				Team:       "\"Test\"",
				Properties: map[string]string{},
				Relations:  map[string]interface{}{},
			},
			{
				Identifier: ".metadata.name",
				Blueprint:  "\"k8s-export-test-bp\"",
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

		controllerWithFullMapping := newFixture(t, &fixtureConfig{resource: mapping, objects: []runtime.Object{}}).controller

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
