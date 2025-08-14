package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
	testUtils "github.com/port-labs/port-k8s-exporter/test_utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apiextensionsv1fake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	discoveryfake "k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/restmapper"
)

var (
	blueprintPrefix = "k8s-export-test"
	deploymentKind  = "apps/v1/deployments"
	daemonSetKind   = "apps/v1/daemonsets"
)

func getBlueprintId(stateKey string) string {
	return testUtils.GetBlueprintIdFromPrefixAndStateKey(blueprintPrefix, stateKey)
}

type fixture struct {
	t                  *testing.T
	controllersHandler *ControllersHandler
	k8sClient          *k8s.Client
	portClient         *cli.PortClient
	stateKey           string
}

type fixtureConfig struct {
	portClientId        string
	portClientSecret    string
	stateKey            string
	sendRawDataExamples *bool
	resources           []port.Resource
	existingObjects     []runtime.Object
}

func tearDownFixture(
	t *testing.T,
	f *fixture,
) {
	blueprintId := getBlueprintId(f.stateKey)
	t.Logf("deleting resources for %s", f.stateKey)
	_ = integration.DeleteIntegration(
		f.portClient,
		f.stateKey,
	)
	_ = blueprint.DeleteBlueprintEntities(
		f.portClient,
		blueprintId,
	)
	_ = blueprint.DeleteBlueprint(
		f.portClient,
		blueprintId,
	)
}

type resourceMapEntry struct {
	list *metav1.APIResourceList
	err  error
}

type fakeDiscovery struct {
	*discoveryfake.FakeDiscovery

	lock         sync.Mutex
	groupList    *metav1.APIGroupList
	groupListErr error
	resourceMap  map[string]*resourceMapEntry
}

func (c *fakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if rl, ok := c.resourceMap[groupVersion]; ok {
		return rl.list, rl.err
	}
	return nil, errors.New("doesn't exist")
}

func (c *fakeDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.groupList == nil {
		return nil, errors.New("doesn't exist")
	}
	return c.groupList, c.groupListErr
}

func newFixture(t *testing.T, fixtureConfig *fixtureConfig) *fixture {
	defaultTrue := true
	sendRawDataExamples := &defaultTrue
	if fixtureConfig.sendRawDataExamples != nil {
		sendRawDataExamples = fixtureConfig.sendRawDataExamples
	}

	integrationConfig := &port.IntegrationAppConfig{
		DeleteDependents:             true,
		CreateMissingRelatedEntities: true,
		SendRawDataExamples:          sendRawDataExamples,
		Resources:                    fixtureConfig.resources,
	}

	if fixtureConfig.stateKey != "" {
		config.ApplicationConfig.StateKey = fixtureConfig.stateKey
	} else {
		config.ApplicationConfig.StateKey = "my-k8s-exporter"
	}

	applicationConfig := &config.ApplicationConfiguration{
		ConfigFilePath:                  config.ApplicationConfig.ConfigFilePath,
		ResyncInterval:                  config.ApplicationConfig.ResyncInterval,
		PortBaseURL:                     config.ApplicationConfig.PortBaseURL,
		EventListenerType:               config.ApplicationConfig.EventListenerType,
		CreateDefaultResources:          config.ApplicationConfig.CreateDefaultResources,
		OverwriteConfigurationOnRestart: config.ApplicationConfig.OverwriteConfigurationOnRestart,
		Resources:                       integrationConfig.Resources,
		DeleteDependents:                integrationConfig.DeleteDependents,
		CreateMissingRelatedEntities:    integrationConfig.CreateMissingRelatedEntities,
		UpdateEntityOnlyOnDiff:          config.ApplicationConfig.UpdateEntityOnlyOnDiff,
		PortClientId:                    config.ApplicationConfig.PortClientId,
		PortClientSecret:                config.ApplicationConfig.PortClientSecret,
		StateKey:                        config.ApplicationConfig.StateKey,
	}

	if fixtureConfig.portClientId != "" {
		applicationConfig.PortClientId = fixtureConfig.portClientId
	}
	if fixtureConfig.portClientSecret != "" {
		applicationConfig.PortClientSecret = fixtureConfig.portClientSecret
	}

	exporterConfig := &port.Config{
		StateKey:                        applicationConfig.StateKey,
		EventListenerType:               applicationConfig.EventListenerType,
		CreateDefaultResources:          applicationConfig.CreateDefaultResources,
		ResyncInterval:                  applicationConfig.ResyncInterval,
		OverwriteConfigurationOnRestart: applicationConfig.OverwriteConfigurationOnRestart,
		Resources:                       applicationConfig.Resources,
		DeleteDependents:                applicationConfig.DeleteDependents,
		CreateMissingRelatedEntities:    applicationConfig.CreateMissingRelatedEntities,
	}

	groups := make([]metav1.APIGroup, 0)
	resourceMap := make(map[string]*resourceMapEntry)

	for _, resource := range integrationConfig.Resources {
		gvr := getGvr(resource.Kind)
		version := metav1.GroupVersionForDiscovery{Version: gvr.Version, GroupVersion: fmt.Sprintf("%s/%s", gvr.Group, gvr.Version)}
		groupFound := false
		for i, group := range groups {
			if group.Name == gvr.Group {
				groupFound = true
				versionFound := false
				for _, v := range group.Versions {
					if v.Version == gvr.Version {
						versionFound = true
						break
					}
				}
				if !versionFound {
					groups[i].Versions = append(groups[i].Versions, version)
				}
			}
		}
		if !groupFound {
			groups = append(groups, metav1.APIGroup{Name: gvr.Group, Versions: []metav1.GroupVersionForDiscovery{version}})
		}
		resourceMapKey := fmt.Sprintf("%s/%s", gvr.Group, gvr.Version)
		apiResource := metav1.APIResource{
			Name:       gvr.Resource,
			Namespaced: true,
			Group:      gvr.Group,
			Version:    gvr.Version,
		}
		if _, ok := resourceMap[resourceMapKey]; ok {
			resourceMap[resourceMapKey].list.APIResources = append(resourceMap[resourceMapKey].list.APIResources, apiResource)
		} else {
			resourceMap[resourceMapKey] = &resourceMapEntry{
				list: &metav1.APIResourceList{
					GroupVersion: resourceMapKey,
					APIResources: []metav1.APIResource{apiResource},
				},
			}
		}
	}

	fakeD := &fakeDiscovery{
		groupList: &metav1.APIGroupList{
			Groups: groups,
		},
		resourceMap: resourceMap,
	}

	kClient := fakeclientset.NewSimpleClientset()
	discoveryClient := discovery.NewDiscoveryClient(fakeD.RESTClient())
	dynamicClient := k8sfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), newGvrToListKind(), fixtureConfig.existingObjects...)
	fae := apiextensionsv1fake.FakeApiextensionsV1{Fake: &kClient.Fake}
	apiExtensionsClient := apiextensionsv1.New(fae.RESTClient())
	cacheClient := memory.NewMemCacheClient(fakeD)
	discoveryMapper := restmapper.NewDeferredDiscoveryRESTMapper(cacheClient)
	k8sClient := &k8s.Client{DiscoveryClient: discoveryClient, DynamicClient: dynamicClient, DiscoveryMapper: discoveryMapper, ApiExtensionClient: apiExtensionsClient}
	portClient := cli.New(applicationConfig)
	blueprintIdentifier := getBlueprintId(exporterConfig.StateKey)

	blueprintRaw := port.Blueprint{
		Identifier: blueprintIdentifier,
		Title:      blueprintIdentifier,
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
	t.Logf("creating blueprint %s", blueprintIdentifier)
	if _, err := blueprint.NewBlueprint(portClient, blueprintRaw); err != nil {
		t.Logf("Error creating Port blueprint: %s, retrying once", err.Error())
		if _, secondErr := blueprint.NewBlueprint(portClient, blueprintRaw); secondErr != nil {
			t.Errorf("Error when retrying to create Port blueprint: %s ", secondErr.Error())
			t.FailNow()
		}
	}

	err := defaults.InitIntegration(portClient, exporterConfig, "unknown", true)
	if err != nil {
		t.Errorf("error initializing integration: %v", err)
	}

	controllersHandler := NewControllersHandler(exporterConfig, integrationConfig, k8sClient, portClient)

	return &fixture{
		t:                  t,
		controllersHandler: controllersHandler,
		k8sClient:          k8sClient,
		portClient:         portClient,
		stateKey:           exporterConfig.StateKey,
	}
}

func newResource(selectorQuery string, mappings []port.EntityMapping, kind string) port.Resource {
	return port.Resource{
		Kind: kind,
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
	blueprintId := getBlueprintId(stateKey)
	labels := map[string]string{
		"app": blueprintId,
	}
	return &appsv1.Deployment{
		TypeMeta: v1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      blueprintId,
			Namespace: blueprintId,
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
							Name:  blueprintId,
							Image: fmt.Sprintf("%s:latest", blueprintId),
						},
					},
				},
			},
		},
	}
}

func newDaemonSet(stateKey string) *appsv1.DaemonSet {
	blueprintId := getBlueprintId(stateKey)
	labels := map[string]string{
		"app": blueprintId,
	}
	return &appsv1.DaemonSet{
		TypeMeta: v1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ds", blueprintId),
			Namespace: blueprintId,
		},
		Spec: appsv1.DaemonSetSpec{
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
							Name:  blueprintId,
							Image: fmt.Sprintf("%s:latest", blueprintId),
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
		{Group: "apps", Version: "v1", Resource: "daemonsets"}:  "DaemonSetList",
	}
}

func getGvr(kind string) schema.GroupVersionResource {
	s := strings.SplitN(kind, "/", 3)
	return schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
}

func getBaseResource(stateKey string, kind string) port.Resource {
	blueprintId := getBlueprintId(stateKey)

	return newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
			Icon:       "\"Microservice\"",
			// Team:       "\"Test\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
			/* Relations: map[string]interface{}{
				"k8s-relation": "\"e_AgPMYvq1tAs8TuqM\"",
			}, */
		},
	}, kind)
}

func (f *fixture) createObjects(objects []*unstructured.Unstructured, kind string) {
	if objects != nil {
		for _, d := range objects {
			gvr := getGvr(kind)
			_, err := f.k8sClient.DynamicClient.Resource(gvr).Namespace(d.GetNamespace()).Create(context.TODO(), d, metav1.CreateOptions{})
			if err != nil {
				f.t.Errorf("error creating object %s: %v", d.GetName(), err)
			}
		}
	}
}

func (f *fixture) updateObjects(objects []*unstructured.Unstructured, kind string) {
	if objects != nil {
		for _, d := range objects {
			gvr := getGvr(kind)
			_, err := f.k8sClient.DynamicClient.Resource(gvr).Namespace(d.GetNamespace()).Update(context.TODO(), d, metav1.UpdateOptions{})
			if err != nil {
				f.t.Errorf("error updating object %s: %v", d.GetName(), err)
			}
		}
	}
}

func (f *fixture) deleteObjects(objects []struct{ kind, namespace, name string }) {
	if objects != nil {
		for _, d := range objects {
			gvr := getGvr(d.kind)
			err := f.k8sClient.DynamicClient.Resource(gvr).Namespace(d.namespace).Delete(context.TODO(), d.name, metav1.DeleteOptions{})
			if err != nil {
				f.t.Errorf("error deleting object %s: %v", d.name, err)
			}
		}
	}
}

func (f *fixture) assertObjectsHandled(objects []struct{ kind, name string }) {
	assert.Eventually(f.t, func() bool {
		integrationKinds, err := f.portClient.GetIntegrationKinds(f.controllersHandler.stateKey)
		if err != nil {
			return false
		}

		for _, obj := range objects {
			examples := integrationKinds[obj.kind].Examples
			found := false
			for _, example := range examples {
				if example.Data["metadata"].(map[string]interface{})["name"] == obj.name {
					found = true
					continue
				}
			}
			if !found {
				return false
			}
		}

		return true
	}, time.Second*15, time.Millisecond*500)

	assert.Eventually(f.t, func() bool {
		processedStateKey := fmt.Sprintf("(statekey/%s)", f.controllersHandler.stateKey)
		entities, err := f.portClient.SearchEntitiesByDatasource(context.Background(), "port-k8s-exporter", processedStateKey)

		for _, obj := range objects {
			found := false
			for _, entity := range entities {
				if entity.Identifier == obj.name {
					found = true
					continue
				}
			}
			if !found {
				return false
			}
		}

		return err == nil && len(entities) == len(objects)
	}, time.Second*15, time.Millisecond*500)
}

func (f *fixture) runControllersHandle() {
	f.controllersHandler.Handle(INITIAL_RESYNC)
}

func TestSuccessfulControllersHandle(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	de := newDeployment(stateKey)
	de.Name = guuid.NewString()
	da := newDaemonSet(stateKey)
	da.Name = guuid.NewString()
	resources := []port.Resource{getBaseResource(stateKey, deploymentKind), getBaseResource(stateKey, daemonSetKind)}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(de), newUnstructured(da)}})

	// To test later that the delete stale entities is working
	f.portClient.CreateEntity(context.Background(), &port.EntityRequest{Blueprint: blueprintId, Identifier: guuid.NewString()}, "", false)

	f.runControllersHandle()

	f.assertObjectsHandled([]struct{ kind, name string }{{kind: deploymentKind, name: de.Name}, {kind: daemonSetKind, name: da.Name}})

	nde := newDeployment(stateKey)
	nde.Name = guuid.NewString()
	f.createObjects([]*unstructured.Unstructured{newUnstructured(nde)}, deploymentKind)

	nda := newDaemonSet(stateKey)
	nda.Name = guuid.NewString()
	f.createObjects([]*unstructured.Unstructured{newUnstructured(nda)}, daemonSetKind)

	assert.Eventually(t, func() bool {
		for _, eid := range []string{nde.Name, nda.Name} {
			_, err := f.portClient.ReadEntity(context.Background(), eid, blueprintId)
			if err != nil {
				return false
			}
		}
		return true
	}, time.Second*15, time.Millisecond*500)

	nde.Spec.Selector.MatchLabels["app"] = "new-label"
	f.updateObjects([]*unstructured.Unstructured{newUnstructured(nde)}, deploymentKind)
	da.Spec.Selector.MatchLabels["app"] = "new-label"
	f.updateObjects([]*unstructured.Unstructured{newUnstructured(da)}, daemonSetKind)

	assert.Eventually(t, func() bool {
		entity, err := f.portClient.ReadEntity(context.Background(), nde.Name, blueprintId)
		if err != nil || entity.Properties["obj"].(map[string]interface{})["matchLabels"].(map[string]interface{})["app"] != nde.Spec.Selector.MatchLabels["app"] {
			return false
		}
		entity, err = f.portClient.ReadEntity(context.Background(), da.Name, blueprintId)
		return err == nil && entity.Properties["obj"].(map[string]interface{})["matchLabels"].(map[string]interface{})["app"] == nde.Spec.Selector.MatchLabels["app"]
	}, time.Second*15, time.Millisecond*500)

	f.deleteObjects([]struct{ kind, namespace, name string }{
		{kind: deploymentKind, namespace: de.Namespace, name: de.Name}, {kind: daemonSetKind, namespace: da.Namespace, name: da.Name},
		{kind: deploymentKind, namespace: nde.Namespace, name: nde.Name}, {kind: daemonSetKind, namespace: nda.Namespace, name: nda.Name}})

	assert.Eventually(t, func() bool {
		for _, eid := range []string{de.Name, da.Name, nde.Name, nda.Name} {
			_, err := f.portClient.ReadEntity(context.Background(), eid, blueprintId)
			if err == nil || !strings.Contains(err.Error(), "was not found") {
				return false
			}
		}
		return true
	}, time.Second*15, time.Millisecond*500)
	defer tearDownFixture(t, f)
}

func TestControllersHandleTolerateFailure(t *testing.T) {
	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	resources := []port.Resource{getBaseResource(stateKey, deploymentKind)}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{}})

	f.runControllersHandle()

	invalidId := fmt.Sprintf("%s!@#", guuid.NewString())
	d := newDeployment(stateKey)
	d.Name = invalidId
	f.createObjects([]*unstructured.Unstructured{newUnstructured(d)}, deploymentKind)

	id := guuid.NewString()
	d.Name = id
	t.Logf("before creating %s", id)
	f.createObjects([]*unstructured.Unstructured{newUnstructured(d)}, deploymentKind)
	t.Logf("after creating %s", id)

	assert.Eventually(t, func() bool {
		_, err := f.portClient.ReadEntity(context.Background(), id, blueprintId)
		return err == nil
	}, time.Second*15, time.Millisecond*500)

	f.deleteObjects([]struct{ kind, namespace, name string }{{kind: deploymentKind, namespace: d.Namespace, name: d.Name}})

	assert.Eventually(t, func() bool {
		_, err := f.portClient.ReadEntity(context.Background(), id, blueprintId)
		return err != nil && strings.Contains(err.Error(), "was not found")
	}, time.Second*15, time.Millisecond*500)
	defer tearDownFixture(t, f)
}

func TestControllersHandler_Stop(t *testing.T) {
	stateKey := guuid.NewString()

	resources := []port.Resource{getBaseResource(stateKey, deploymentKind)}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{}})
	defer tearDownFixture(t, f)

	f.controllersHandler.Stop()
	assert.True(t, f.controllersHandler.isStopped)
	f.controllersHandler.Stop()
	assert.True(t, f.controllersHandler.isStopped)
	assert.Panics(t, func() { close(f.controllersHandler.stopCh) })
}

func TestControllersHandler_RunResyncNotOverlaps(t *testing.T) {
	stateKey := guuid.NewString()
	resources := []port.Resource{getBaseResource(stateKey, deploymentKind)}
	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{}})
	defer tearDownFixture(t, f)

	RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, SCHEDULED_RESYNC)
	firstControllersHandler := controllerHandler

	RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, SCHEDULED_RESYNC)
	secondControllersHandler := controllerHandler

	assert.NotNil(t, firstControllersHandler)
	assert.NotNil(t, secondControllersHandler)
	assert.NotEqual(t, firstControllersHandler, secondControllersHandler)
	assert.True(t, firstControllersHandler.isStopped)
	assert.False(t, secondControllersHandler.isStopped)
}
