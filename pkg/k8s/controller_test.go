package k8s

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	testUtils "github.com/port-labs/port-k8s-exporter/test_utils"
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
	blueprintPrefix    = "k8s-export-test"
)

func getBlueprintId(stateKey string) string {
	return testUtils.GetBlueprintIdFromPrefixAndStateKey(blueprintPrefix, stateKey)
}

type fixture struct {
	t          *testing.T
	controller *Controller
	kubeClient *k8sfake.FakeDynamicClient
	stateKey   string
}

type fixtureConfig struct {
	portClientId        string
	portClientSecret    string
	stateKey            string
	sendRawDataExamples *bool
	resource            port.Resource
	existingObjects     []runtime.Object
	blueprint           *port.Blueprint
}

func tearDownFixture(
	t *testing.T,
	f *fixture,
) {
	blueprintId := getBlueprintId(f.stateKey)
	t.Logf("deleting resources for %s", f.stateKey)
	_ = integration.DeleteIntegration(
		f.controller.portClient,
		f.stateKey,
	)
	_ = blueprint.DeleteBlueprintEntities(
		f.controller.portClient,
		blueprintId,
	)
	_ = blueprint.DeleteBlueprint(
		f.controller.portClient,
		blueprintId,
	)
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

	if newConfig.StateKey == "" {
		newConfig.StateKey = guuid.NewString()
	}

	portClient := cli.New(newConfig)
	blueprintIdentifier := getBlueprintId(newConfig.StateKey)

	kubeClient := k8sfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), newGvrToListKind(), fixtureConfig.existingObjects...)
	controller := newController(t, fixtureConfig.resource, kubeClient, interationConfig, newConfig)

	var blueprintRaw port.Blueprint
	if fixtureConfig.blueprint != nil {
		blueprintRaw = *fixtureConfig.blueprint
	} else {
		blueprintRaw = port.Blueprint{
			Identifier: blueprintIdentifier,
			Title:      blueprintIdentifier,
			Ownership: &port.Ownership{
				Type: "Direct",
			},
			Schema: port.BlueprintSchema{
				Properties: map[string]port.Property{
					"bool": {
						Type: "boolean",
					},
					"text": {
						Type: "string",
					},
					"num": {
						Type: "number",
					},
					"obj": {
						Type: "object",
					},
					"arr": {
						Type: "array",
					},
				},
			},
		}
	}

	t.Logf("creating blueprint %s", blueprintIdentifier)
	if _, err := blueprint.NewBlueprint(portClient, blueprintRaw); err != nil {
		t.Logf("Error creating Port blueprint: %s, retrying", err.Error())
		if _, secondErr := blueprint.NewBlueprint(portClient, blueprintRaw); secondErr != nil {
			t.Logf("Error when retrying to create Port blueprint: %s ", secondErr.Error())
		}
	}

	return &fixture{
		t:          t,
		controller: controller,
		kubeClient: kubeClient,
		stateKey:   newConfig.StateKey,
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

func newDeployment(stateKey string) *appsv1.Deployment {
	blueprintIdentifier := getBlueprintId(stateKey)
	labels := map[string]string{
		"app": blueprintIdentifier,
	}
	return &appsv1.Deployment{
		TypeMeta: v1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      blueprintIdentifier,
			Namespace: blueprintIdentifier,
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
							Name:  blueprintIdentifier,
							Image: fmt.Sprintf("%s:latest", blueprintIdentifier),
						},
					},
				},
			},
		},
	}
}

func newDeploymentWithCustomLabels(
	stateKey string,
	generation int64,
	generateName string,
	creationTimestamp v1.Time,
	labels map[string]string,
) *appsv1.Deployment {
	blueprintIdentifier := getBlueprintId(stateKey)
	return &appsv1.Deployment{
		TypeMeta: v1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:              blueprintIdentifier,
			Namespace:         blueprintIdentifier,
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
							Name:  blueprintIdentifier,
							Image: fmt.Sprintf("%s:latest", blueprintIdentifier),
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

func getGvr(kind string) schema.GroupVersionResource {
	s := strings.SplitN(kind, "/", 3)
	return schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
}

func getKey(deployment *appsv1.Deployment, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(deployment)
	if err != nil {
		t.Errorf("Unexpected error getting key for deployment %v: %v", deployment.Name, err)
		return ""
	}
	return key
}

func getBaseDeploymentResource(stateKey string) port.Resource {
	return newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", getBlueprintId(stateKey)),
			Icon:       "\"Microservice\"",
			// TODO: Create this explicitly and suffix with stateKey
			// Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			/* Relations: map[string]interface{}{
				// TODO: Explicitly build this :( perhaps with stateKey as well
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			}, */
		},
	})
}

func (f *fixture) createObjects(objects []*unstructured.Unstructured) {
	gvr := getGvr(f.controller.Resource.Kind)
	currentNumEventsInQueue := f.controller.eventsWorkqueue.Len()
	if objects != nil {
		for _, d := range objects {
			_, err := f.kubeClient.Resource(gvr).Namespace(d.GetNamespace()).Create(context.TODO(), d, metav1.CreateOptions{})
			if err != nil {
				f.t.Errorf("error creating object %s: %v", d.GetName(), err)
			}
		}

		assert.Eventually(f.t, func() bool {
			return f.controller.eventsWorkqueue.Len() == currentNumEventsInQueue+len(objects)
		}, time.Second*2, time.Millisecond*100)
	}
}

func (f *fixture) updateObjects(objects []*unstructured.Unstructured) {
	gvr := getGvr(f.controller.Resource.Kind)
	currentNumEventsInQueue := f.controller.eventsWorkqueue.Len()
	if objects != nil {
		for _, d := range objects {
			_, err := f.kubeClient.Resource(gvr).Namespace(d.GetNamespace()).Update(context.TODO(), d, metav1.UpdateOptions{})
			if err != nil {
				f.t.Errorf("error updating object %s: %v", d.GetName(), err)
			}
		}

		assert.Eventually(f.t, func() bool {
			return f.controller.eventsWorkqueue.Len() == currentNumEventsInQueue+len(objects)
		}, time.Second*2, time.Millisecond*100)
	}
}

func (f *fixture) deleteObjects(objects []struct{ namespace, name string }) {
	gvr := getGvr(f.controller.Resource.Kind)
	if objects != nil {
		for _, d := range objects {
			err := f.kubeClient.Resource(gvr).Namespace(d.namespace).Delete(context.TODO(), d.name, metav1.DeleteOptions{})
			if err != nil {
				f.t.Errorf("error deleting object %s: %v", d.name, err)
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

func (f *fixture) runControllerEventsSync() {
	f.controller.RunEventsSync(1, signal.SetupSignalHandler())
}

func TestSuccessfulRunInitialSync(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintIdentifier := getBlueprintId(stateKey)
	ud1 := newUnstructured(newDeployment(stateKey))
	ud1.SetName("deployment1")
	ud2 := newUnstructured(newDeployment(stateKey))
	ud2.SetName("deployment2")
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: getBaseDeploymentResource(stateKey), existingObjects: []runtime.Object{ud1, ud2}})
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{
		EntitiesSet:     map[string]interface{}{fmt.Sprintf("%s;%s", blueprintIdentifier, ud1.GetName()): nil, fmt.Sprintf("%s;%s", blueprintIdentifier, ud2.GetName()): nil},
		RawDataExamples: []interface{}{ud1.Object, ud2.Object}, ShouldDeleteStaleEntities: true,
	})
}

func TestRunInitialSyncWithSelectorQuery(t *testing.T) {
	stateKey := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	notSelectedResource := getBaseDeploymentResource(stateKey)
	notSelectedResource.Selector.Query = fmt.Sprintf(".metadata.name != \"%s\"", getBlueprintId(stateKey))
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: notSelectedResource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: true})
}

func TestRunInitialSyncWithBadPropMapping(t *testing.T) {
	stateKey := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	badPropMappingResource := getBaseDeploymentResource(stateKey)
	badPropMappingResource.Port.Entity.Mappings[0].Properties["text"] = "bad-jq"
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: badPropMappingResource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: false})
}

func TestRunInitialSyncWithBadEntity(t *testing.T) {
	stateKey := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	badEntityResource := getBaseDeploymentResource(stateKey)
	badEntityResource.Port.Entity.Mappings[0].Identifier = "\"!@#\""
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: badEntityResource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: false})
}

func TestRunInitialSyncWithBadSelector(t *testing.T) {
	stateKey := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	badSelectorResource := getBaseDeploymentResource(stateKey)
	badSelectorResource.Selector.Query = "bad-jq"
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: badSelectorResource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: false})
}

func TestRunEventsSyncWithCreateEvent(t *testing.T) {
	stateKey := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	id := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: fmt.Sprintf("\"%s\"", id),
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
		},
	})
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{}})

	defer tearDownFixture(t, f)

	f.createObjects([]*unstructured.Unstructured{ud})
	defer f.controller.portClient.DeleteEntity(context.Background(), id, blueprintId, true)
	f.runControllerEventsSync()

	assert.Eventually(t, func() bool {
		_, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
		return err == nil
	}, time.Second*5, time.Millisecond*500)
}

func TestRunEventsSyncWithUpdateEvent(t *testing.T) {
	id := guuid.NewString()
	stateKey := guuid.NewString()
	blueprintIdentifier := getBlueprintId(stateKey)
	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Identifier = fmt.Sprintf("\"%s\"", id)
	resource.Port.Entity.Mappings[0].Properties["bool"] = ".spec.selector.matchLabels.app == \"new-label\""
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})

	defer f.controller.portClient.DeleteEntity(context.Background(), id, getBlueprintId(stateKey), true)
	defer tearDownFixture(t, f)
	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintIdentifier, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true})
	assert.Eventually(t, func() bool {
		entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintIdentifier)
		return err == nil && entity.Properties["bool"] == false
	}, time.Second*5, time.Millisecond*500)

	d.Spec.Selector.MatchLabels["app"] = "new-label"
	f.updateObjects([]*unstructured.Unstructured{newUnstructured(d)})
	f.runControllerEventsSync()

	assert.Eventually(t, func() bool {
		entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintIdentifier)
		return err == nil && entity.Properties["bool"] == true
	}, time.Second*5, time.Millisecond*500)
}

func TestCreateDeployment(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := getBaseDeploymentResource(stateKey)
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestUpdateDeployment(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := getBaseDeploymentResource(stateKey)
	item := EventItem{Key: getKey(d, t), ActionType: UpdateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestDeleteDeploymentSameOwner(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithSameOwner\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)

	f.runControllerSyncHandler(createItem, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;entityWithSameOwner", blueprintId): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithSameOwner", blueprintId)
	if err != nil && !strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to be deleted")
	}
}

func TestSelectorQueryFilterDeployment(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource(fmt.Sprintf(".metadata.name != \"%s\"", blueprintId), []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"wrong-%s\"", blueprintId),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{}, ShouldDeleteStaleEntities: true}, false)
}

func TestFailPortAuth(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, portClientId: "wrongclientid", portClientSecret: "wrongclientsecret", resource: resource, existingObjects: []runtime.Object{ud}})
	// defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: nil, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: false}, true)
}

func TestFailDeletePortEntity(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"wrong-%s\"", blueprintId),
		},
	})
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
}

func TestCreateDeploymentWithSearchIdentifier(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	id := guuid.NewString()
	randTxt := guuid.NewString()
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Identifier = fmt.Sprintf("\"%s\"", id)
	resource.Port.Entity.Mappings[0].Properties["text"] = fmt.Sprintf("\"%s\"", randTxt)
	resource.Port.Entity.Mappings[0].Properties["bool"] = "true"
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	assert.True(t, entity.Properties["bool"] == true, fmt.Sprintf("expected bool to be true, got: %v", entity.Properties["bool"]))

	item = EventItem{Key: getKey(d, t), ActionType: UpdateAction}
	resource.Port.Entity.Mappings[0].Identifier = map[string]interface{}{
		"combinator": "\"and\"",
		"rules": []interface{}{
			map[string]interface{}{
				"property": "\"text\"",
				"operator": "\"=\"",
				"value":    fmt.Sprintf("\"%s\"", randTxt),
			},
		}}
	resource.Port.Entity.Mappings[0].Properties["bool"] = "false"
	f = newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})

	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	entity, err = f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	assert.True(t, entity.Properties["bool"] == false, fmt.Sprintf("expected bool to be false, got: %v", entity.Properties["bool"]))

	deleteItem := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f.runControllerSyncHandler(deleteItem, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
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
				Blueprint:  "\"to-be-replaced\"",
				Icon:       "\"Microservice\"",
				// Team:       "\"Test\"",
				Properties: map[string]string{
					"labels":            ".spec.selector",
					"generation":        ".metadata.generation",
					"generateName":      ".metadata.generateName",
					"creationTimestamp": ".metadata.creationTimestamp",
				},
				/* Relations: map[string]interface{}{
					"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
				}, */
			},
		}),
		newResource("", []port.EntityMapping{

			{
				Identifier: ".metadata.name",
				Blueprint:  "\"to-be-replaced\"",
				Icon:       "\"Microservice\"",
				// Team:       "\"Test\"",
				Properties: map[string]string{},
				Relations:  map[string]interface{}{},
			},
			{
				Identifier: ".metadata.name",
				Blueprint:  "\"to-be-replaced\"",
				Icon:       "\"Microservice\"",
				Team:       "\"Test\"",
				Properties: map[string]string{
					"labels":            ".spec.selector",
					"generation":        ".metadata.generation",
					"generateName":      ".metadata.generateName",
					"creationTimestamp": ".metadata.creationTimestamp",
				},
				/* Relations: map[string]interface{}{
					"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
				}, */
			},
		}),
	}

	for _, mapping := range fullMapping {
		stateKey := guuid.NewString()
		blueprintId := getBlueprintId(stateKey)

		f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: mapping, existingObjects: []runtime.Object{}})
		controllerWithFullMapping := f.controller
		// Test changes in each individual property
		properties := map[string]Property{
			"metadata.name":              {Value: blueprintId, ShouldSendEvent: false},
			"something_without_mapping":  {Value: blueprintId, ShouldSendEvent: false},
			"metadata.generation":        {Value: int64(3), ShouldSendEvent: true},
			"metadata.generateName":      {Value: "new-port-k8s-exporter2", ShouldSendEvent: true},
			"metadata.creationTimestamp": {Value: v1.Now().Add(1 * time.Hour).Format(time.RFC3339), ShouldSendEvent: true},
		}

		for property, value := range properties {
			newDep := newUnstructured(newDeploymentWithCustomLabels(stateKey, 2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": blueprintId}))
			oldDep := newUnstructured(newDeploymentWithCustomLabels(stateKey, 2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": blueprintId}))

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
		newDep := newUnstructured(newDeploymentWithCustomLabels(stateKey, 2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": stateKey}))
		oldDep := newUnstructured(newDeploymentWithCustomLabels(stateKey, 2, "new-port-k8s-exporter", v1.Now(), map[string]string{"app": "new-port-k8s-exporter"}))

		result := controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, true)
		assert.True(t, result, fmt.Sprintf("Expected true when labels changes and feature flag is on"))
		result = controllerWithFullMapping.shouldSendUpdateEvent(oldDep, newDep, false)
		assert.True(t, result, fmt.Sprintf("Expected true when labels changes and feature flag is off"))
		defer tearDownFixture(t, f)
	}
}

func TestDeleteDeploymentDifferentOwner(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId("non_exist")
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityWithDifferentOwner\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
		},
	})
	createItem := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	item := EventItem{Key: getKey(d, t), ActionType: DeleteAction}
	f := newFixture(t, &fixtureConfig{stateKey: "non_exist", resource: resource, existingObjects: []runtime.Object{ud}})

	f.runControllerSyncHandler(createItem, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;entityWithDifferentOwner", blueprintId): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	_, err := f.controller.portClient.ReadEntity(context.Background(), "entityWithDifferentOwner", blueprintId)
	defer tearDownFixture(t, f)
	if err != nil && strings.Contains(err.Error(), "was not found") {
		t.Errorf("expected entity to exist")
	}
}

/*

func TestRunEventsSyncWithDeleteEvent(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := newResource("", []port.EntityMapping{
		{
			Identifier: "\"entityToBeDeleted\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
		},
	})
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})

	f.runControllerInitialSync(&SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, "entityToBeDeleted"): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true})
	f.deleteObjects([]struct{ namespace, name string }{{namespace: d.Namespace, name: d.Name}})

	assert.Eventually(t, func() bool {
		_, err := f.controller.portClient.ReadEntity(context.Background(), "entityToBeDeleted", blueprintId)
		return err != nil && strings.Contains(err.Error(), "was not found")
	}, time.Second*15, time.Millisecond*1500)
	defer tearDownFixture(t, f)
}

*/

/* // TODO: Verify relations here
func TestCreateDeploymentWithSearchRelation(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Relations = map[string]interface{}{
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
	}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, d.Name): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)
} */

func newBlueprintWithContainerName(blueprintId string) port.Blueprint {
	return port.Blueprint{
		Identifier: blueprintId,
		Title:      blueprintId,
		Ownership: &port.Ownership{
			Type: "Direct",
		},
		Schema: port.BlueprintSchema{
			Properties: map[string]port.Property{
				"bool": {
					Type: "boolean",
				},
				"text": {
					Type: "string",
				},
				"num": {
					Type: "number",
				},
				"obj": {
					Type: "object",
				},
				"arr": {
					Type: "array",
				},
				"containerName": {
					Type: "string",
				},
			},
		},
	}
}

func TestItemsToParseName(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)

	resource := getBaseDeploymentResource(stateKey)
	resource.Port.ItemsToParse = ".spec.template.spec.containers"
	resource.Port.Entity.Mappings[0].Properties["containerName"] = ".item.name"

	blueprint := newBlueprintWithContainerName(blueprintId)
	f := newFixture(t, &fixtureConfig{
		stateKey:        stateKey,
		resource:        resource,
		existingObjects: []runtime.Object{ud},
		blueprint:       &blueprint,
	})
	defer tearDownFixture(t, f)

	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	_, err := f.controller.syncHandler(item)
	if err != nil {
		t.Errorf("error syncing item: %v", err)
	}

	// Verify the container name was correctly extracted using default "item"
	entity, err := f.controller.portClient.ReadEntity(context.Background(), d.Name, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	assert.Equal(t, d.Name, entity.Properties["containerName"])
}

func TestItemsToParseNameCustom(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	d := newDeployment(stateKey)
	ud := newUnstructured(d)

	resource := getBaseDeploymentResource(stateKey)
	resource.Port.ItemsToParse = ".spec.template.spec.containers"
	resource.Port.ItemsToParseName = "container"
	resource.Port.Entity.Mappings[0].Properties["containerName"] = ".container.name"

	blueprint := newBlueprintWithContainerName(blueprintId)
	f := newFixture(t, &fixtureConfig{
		stateKey:        stateKey,
		resource:        resource,
		existingObjects: []runtime.Object{ud},
		blueprint:       &blueprint,
	})
	defer tearDownFixture(t, f)

	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	_, err := f.controller.syncHandler(item)
	if err != nil {
		t.Errorf("error syncing item: %v", err)
	}

	// Verify the container name was correctly extracted using custom "container"
	entity, err := f.controller.portClient.ReadEntity(context.Background(), d.Name, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	assert.Equal(t, d.Name, entity.Properties["containerName"])
}

func TestCreateDeploymentWithTeamString(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	id := guuid.NewString()
	exampleTeamName := "example_team"
	d := newDeployment(stateKey)
	ud := newUnstructured(d)
	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Identifier = fmt.Sprintf("\"%s\"", id)
	resource.Port.Entity.Mappings[0].Team = fmt.Sprintf("\"%s\"", exampleTeamName)
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	teamArray := entity.Team.([]interface{})
	teamValue := teamArray[0].(string)
	assert.Equal(t, exampleTeamName, teamValue)
}

func TestCreateDeploymentWithTeamSearch(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	id := guuid.NewString()
	searchTeamName := "search_team"
	d := newDeployment(stateKey)
	ud := newUnstructured(d)

	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Identifier = fmt.Sprintf("\"%s\"", id)
	resource.Port.Entity.Mappings[0].Team = map[string]interface{}{
		"combinator": "\"and\"",
		"rules": []interface{}{
			map[string]interface{}{
				"property": "\"$identifier\"",
				"operator": "\"=\"",
				"value":    fmt.Sprintf("\"%s\"", searchTeamName),
			},
		},
	}
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	defer tearDownFixture(t, f)
	// Create test team
	teamEntityBody := &port.Entity{
		Blueprint:  "_team",
		Identifier: searchTeamName,
		Title:      searchTeamName,
		Properties: map[string]any{},
		Relations:  map[string]any{},
	}
	pb := &port.ResponseBody{}
	resp, err := f.controller.portClient.Client.R().
		SetBody(teamEntityBody).
		SetHeader("Accept", "application/json").
		SetResult(&pb).
		SetPathParam("blueprint_id", "_team").
		SetQueryParam("upsert", "true").
		Post("v1/blueprints/{blueprint_id}/entities")
	if err != nil {
		t.Errorf("error creating team: %v", err)
	}
	if !pb.OK {
		t.Errorf("failed to create team, got: %s", resp.Body())
	}

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	searchTeamArray := entity.Team.([]interface{})
	searchTeamValue := searchTeamArray[0].(string)
	assert.Equal(t, searchTeamName, searchTeamValue)

	// Cleanup test team
	pc := &port.ResponseBody{}
	res, err := f.controller.portClient.Client.R().
		SetHeader("Accept", "application/json").
		SetResult(&pc).
		SetPathParam("name", searchTeamName).
		Delete("v1/teams/{name}")
	if err != nil {
		t.Errorf("error deleting team: %v", err)
	}
	if !pc.OK {
		t.Errorf("failed to delete team, got: %s", res.Body())
	}
}

func TestCreateDeploymentWithMultiTeamSearch(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	id := guuid.NewString()
	exampleTeamName := "example_team"
	searchTeamName := "search_team"
	d := newDeployment(stateKey)
	ud := newUnstructured(d)

	resource := getBaseDeploymentResource(stateKey)
	resource.Port.Entity.Mappings[0].Identifier = fmt.Sprintf("\"%s\"", id)
	resource.Port.Entity.Mappings[0].Team = map[string]interface{}{
		"combinator": "\"and\"",
		"rules": []interface{}{
			map[string]interface{}{
				"property": "\"$identifier\"",
				"operator": "\"in\"",
				"value":    fmt.Sprintf("[\"%s\", \"%s\"]", exampleTeamName, searchTeamName),
			},
		},
	}
	item := EventItem{Key: getKey(d, t), ActionType: CreateAction}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resource: resource, existingObjects: []runtime.Object{ud}})
	// Create test team
	teamEntityBody := &port.Entity{
		Blueprint:  "_team",
		Identifier: searchTeamName,
		Title:      searchTeamName,
		Properties: map[string]any{},
		Relations:  map[string]any{},
	}
	pb := &port.ResponseBody{}
	resp, err := f.controller.portClient.Client.R().
		SetBody(teamEntityBody).
		SetHeader("Accept", "application/json").
		SetResult(&pb).
		SetPathParam("blueprint_id", "_team").
		SetQueryParam("upsert", "true").
		Post("v1/blueprints/{blueprint_id}/entities")
	if err != nil {
		t.Errorf("error creating team: %v", err)
	}
	if !pb.OK {
		t.Errorf("failed to create team, got: %s", resp.Body())
	}
	defer tearDownFixture(t, f)

	f.runControllerSyncHandler(item, &SyncResult{EntitiesSet: map[string]interface{}{fmt.Sprintf("%s;%s", blueprintId, id): nil}, RawDataExamples: []interface{}{ud.Object}, ShouldDeleteStaleEntities: true}, false)

	entity, err := f.controller.portClient.ReadEntity(context.Background(), id, blueprintId)
	if err != nil {
		t.Errorf("error reading entity: %v", err)
	}
	expectedTeams := []string{exampleTeamName, searchTeamName}
	teamsArray := entity.Team.([]interface{})
	assert.ElementsMatch(t, expectedTeams, teamsArray)

	// Cleanup test team
	pc := &port.ResponseBody{}
	res, err := f.controller.portClient.Client.R().
		SetHeader("Accept", "application/json").
		SetResult(&pc).
		SetPathParam("name", searchTeamName).
		Delete("v1/teams/{name}")
	if err != nil {
		t.Errorf("error deleting team: %v", err)
	}
	if !pc.OK {
		t.Errorf("failed to delete team, got: %s", res.Body())
	}
}
