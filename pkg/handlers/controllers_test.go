package handlers

import (
	"context"
	"errors"
	"fmt"
	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
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
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	blueprint      = "k8s-export-test-bp"
	deploymentKind = "apps/v1/deployments"
	daemonSetKind  = "apps/v1/daemonsets"
)

type fixture struct {
	t                  *testing.T
	controllersHandler *ControllersHandler
	k8sClient          *k8s.Client
	portClient         *cli.PortClient
}

type fixtureConfig struct {
	portClientId        string
	portClientSecret    string
	stateKey            string
	sendRawDataExamples *bool
	resources           []port.Resource
	existingObjects     []runtime.Object
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
	err := defaults.InitIntegration(portClient, exporterConfig)
	if err != nil {
		t.Errorf("error initializing integration: %v", err)
	}

	controllersHandler := NewControllersHandler(exporterConfig, integrationConfig, k8sClient, portClient)

	return &fixture{
		t:                  t,
		controllersHandler: controllersHandler,
		k8sClient:          k8sClient,
		portClient:         portClient,
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

func newDaemonSet() *appsv1.DaemonSet {
	labels := map[string]string{
		"app": "port-k8s-exporter",
	}
	return &appsv1.DaemonSet{
		TypeMeta: v1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "port-k8s-exporter-ds",
			Namespace: "port-k8s-exporter",
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
		{Group: "apps", Version: "v1", Resource: "daemonsets"}:  "DaemonSetList",
	}
}

func getGvr(kind string) schema.GroupVersionResource {
	s := strings.SplitN(kind, "/", 3)
	return schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
}

func getBaseResource(kind string) port.Resource {
	return newResource("", []port.EntityMapping{
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
	}, time.Second*10, time.Millisecond*500)

	assert.Eventually(f.t, func() bool {
		entities, err := f.portClient.SearchEntities(context.Background(), port.SearchBody{
			Rules: []port.Rule{
				{
					Property: "$datasource",
					Operator: "contains",
					Value:    "port-k8s-exporter",
				},
				{
					Property: "$datasource",
					Operator: "contains",
					Value:    fmt.Sprintf("statekey/%s", f.controllersHandler.stateKey),
				},
			},
			Combinator: "and",
		})

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
	}, time.Second*10, time.Millisecond*500)
}

func (f *fixture) runControllersHandle() {
	f.controllersHandler.Handle()
}

func TestSuccessfulControllersHandle(t *testing.T) {
	de := newDeployment()
	de.Name = guuid.NewString()
	da := newDaemonSet()
	da.Name = guuid.NewString()
	resources := []port.Resource{getBaseResource(deploymentKind), getBaseResource(daemonSetKind)}
	f := newFixture(t, &fixtureConfig{resources: resources, existingObjects: []runtime.Object{newUnstructured(de), newUnstructured(da)}, stateKey: guuid.NewString()})
	defer f.portClient.DeleteIntegration(f.controllersHandler.stateKey)

	// To test later that the delete stale entities is working
	f.portClient.CreateEntity(context.Background(), &port.EntityRequest{Blueprint: blueprint, Identifier: guuid.NewString()}, "", false)

	f.runControllersHandle()

	f.assertObjectsHandled([]struct{ kind, name string }{{kind: deploymentKind, name: de.Name}, {kind: daemonSetKind, name: da.Name}})

	nde := newDeployment()
	nde.Name = guuid.NewString()
	f.createObjects([]*unstructured.Unstructured{newUnstructured(nde)}, deploymentKind)

	nda := newDaemonSet()
	nda.Name = guuid.NewString()
	f.createObjects([]*unstructured.Unstructured{newUnstructured(nda)}, daemonSetKind)

	assert.Eventually(t, func() bool {
		for _, eid := range []string{nde.Name, nda.Name} {
			_, err := f.portClient.ReadEntity(context.Background(), eid, blueprint)
			if err != nil {
				return false
			}
		}
		return true
	}, time.Second*10, time.Millisecond*500)

	nde.Spec.Selector.MatchLabels["app"] = "new-label"
	f.updateObjects([]*unstructured.Unstructured{newUnstructured(nde)}, deploymentKind)
	da.Spec.Selector.MatchLabels["app"] = "new-label"
	f.updateObjects([]*unstructured.Unstructured{newUnstructured(da)}, daemonSetKind)

	assert.Eventually(t, func() bool {
		entity, err := f.portClient.ReadEntity(context.Background(), nde.Name, blueprint)
		if err != nil || entity.Properties["obj"].(map[string]interface{})["matchLabels"].(map[string]interface{})["app"] != nde.Spec.Selector.MatchLabels["app"] {
			return false
		}
		entity, err = f.portClient.ReadEntity(context.Background(), da.Name, blueprint)
		return err == nil && entity.Properties["obj"].(map[string]interface{})["matchLabels"].(map[string]interface{})["app"] == nde.Spec.Selector.MatchLabels["app"]
	}, time.Second*10, time.Millisecond*500)

	f.deleteObjects([]struct{ kind, namespace, name string }{
		{kind: deploymentKind, namespace: de.Namespace, name: de.Name}, {kind: daemonSetKind, namespace: da.Namespace, name: da.Name},
		{kind: deploymentKind, namespace: nde.Namespace, name: nde.Name}, {kind: daemonSetKind, namespace: nda.Namespace, name: nda.Name}})

	assert.Eventually(t, func() bool {
		for _, eid := range []string{de.Name, da.Name, nde.Name, nda.Name} {
			_, err := f.portClient.ReadEntity(context.Background(), eid, blueprint)
			if err == nil || !strings.Contains(err.Error(), "was not found") {
				return false
			}
		}
		return true
	}, time.Second*10, time.Millisecond*500)
}

func TestControllersHandleTolerateFailure(t *testing.T) {
	resources := []port.Resource{getBaseResource(deploymentKind)}
	f := newFixture(t, &fixtureConfig{resources: resources, existingObjects: []runtime.Object{}})

	f.runControllersHandle()

	invalidId := fmt.Sprintf("%s!@#", guuid.NewString())
	d := newDeployment()
	d.Name = invalidId
	f.createObjects([]*unstructured.Unstructured{newUnstructured(d)}, deploymentKind)

	id := guuid.NewString()
	d.Name = id
	f.createObjects([]*unstructured.Unstructured{newUnstructured(d)}, deploymentKind)

	assert.Eventually(t, func() bool {
		_, err := f.portClient.ReadEntity(context.Background(), id, blueprint)
		return err == nil
	}, time.Second*5, time.Millisecond*500)

	f.deleteObjects([]struct{ kind, namespace, name string }{{kind: deploymentKind, namespace: d.Namespace, name: d.Name}})

	assert.Eventually(t, func() bool {
		_, err := f.portClient.ReadEntity(context.Background(), id, blueprint)
		return err != nil && strings.Contains(err.Error(), "was not found")
	}, time.Second*5, time.Millisecond*500)
}
