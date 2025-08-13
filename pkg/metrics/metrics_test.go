package metrics_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	metrics "github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
	testUtils "github.com/port-labs/port-k8s-exporter/test_utils"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	t          *testing.T
	k8sClient  *k8s.Client
	portClient *cli.PortClient
	stateKey   string
}

type fixtureConfig struct {
	portClientId     string
	portClientSecret string
	stateKey         string
	resources        []port.Resource
	existingObjects  []runtime.Object
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
	integrationConfig := &port.IntegrationAppConfig{
		DeleteDependents:             true,
		CreateMissingRelatedEntities: true,
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

	return &fixture{
		t:          t,
		k8sClient:  k8sClient,
		portClient: portClient,
		stateKey:   exporterConfig.StateKey,
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

func getResource(stateKey string, kind string) port.Resource {
	blueprintId := getBlueprintId(stateKey)

	res := newResource("", []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprintId),
			Icon:       "\"Microservice\"",
			Properties: map[string]string{
				"text": "\"pod\"",
				"num":  "1",
				"bool": "true",
				"obj":  ".spec.selector",
				"arr":  ".spec.template.spec.containers",
			},
		},
	}, kind)

	return res
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

func validateMetrics(
	t *testing.T,
	kind string,
	kindIndex *int,
	expectedObjectCountMetrics map[[2]string]float64,
	expectedSuccessMetrics map[[2]string]float64,
) {
	defaultObjectCountMetrics := map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:  0,
		{metrics.MetricFailedResult, metrics.MetricPhaseExtract}:        0,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:   0,
		{metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}: 0,
		{metrics.MetricFailedResult, metrics.MetricPhaseTransform}:      0,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:           0,
		{metrics.MetricFailedResult, metrics.MetricPhaseLoad}:           0,
		{metrics.MetricDeletedResult, metrics.MetricPhaseDelete}:        0,
		{metrics.MetricFailedResult, metrics.MetricPhaseDelete}:         0,
	}

	defaultDurationMetrics := map[[2]string]float64{
		{kind, metrics.MetricPhaseExtract}:                            0,
		{kind, metrics.MetricPhaseTransform}:                          0,
		{kind, metrics.MetricPhaseLoad}:                               0,
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         0,
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: 0,
	}

	defaultSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         0,
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: 0,
		{kind, metrics.MetricPhaseResync}:                             0,
	}

	kindLabel := metrics.GetKindLabel(kind, kindIndex)
	for defaultMetric, defaultMetricValue := range defaultObjectCountMetrics {
		expectedValue := defaultMetricValue
		if val, ok := expectedObjectCountMetrics[defaultMetric]; ok {
			expectedValue = val
		}
		gauge, err := metrics.GetObjectCountGauge(kindLabel, defaultMetric[0], defaultMetric[1])
		assert.NoError(t, err)
		gever := testutil.ToFloat64(gauge)
		assert.Equal(t, expectedValue, gever)
	}

	for defaultMetric, defaultMetricValue := range defaultDurationMetrics {
		durationGauge, err := metrics.GetDurationGauge(defaultMetric[0], defaultMetric[1])
		assert.NoError(t, err)
		assert.Greater(t, testutil.ToFloat64(durationGauge), defaultMetricValue)
	}

	for defaultMetric, defaultMetricValue := range defaultSuccessMetrics {
		expectedValue := defaultMetricValue
		if val, ok := expectedSuccessMetrics[defaultMetric]; ok {
			expectedValue = val
		}
		successGauge, err := metrics.GetSuccessGauge(defaultMetric[0], defaultMetric[1])
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, testutil.ToFloat64(successGauge))
	}
}

func TestMetricsPopulation_SuccessfullResync(t *testing.T) {
	stateKey := guuid.NewString()
	firstDeploymentResource := getResource(stateKey, deploymentKind)
	firstDeploymentResource.Port.Entity.Mappings[0].Properties["identifier"] = ".metadata.name + \"first\""
	secondDeploymentResource := getResource(stateKey, deploymentKind)
	resources := []port.Resource{firstDeploymentResource, secondDeploymentResource}
	d1 := newDeployment(stateKey)
	d1.Name = guuid.NewString()
	d2 := newDeployment(stateKey)
	d2.Name = guuid.NewString()

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(d1), newUnstructured(d2)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	expectedObjectCountMetrics := map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 2,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:  2,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:          2,
	}
	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         1,
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: 1,
		{deploymentKind, metrics.MetricPhaseResync}:                   1,
	}
	for i := 1; i < 2; i++ {
		validateMetrics(t, deploymentKind, &i, expectedObjectCountMetrics, expectedSuccessMetrics)
	}
}

func TestMetricsPopulation_JQError(t *testing.T) {
	stateKey := guuid.NewString()
	resources := []port.Resource{getResource(stateKey, deploymentKind)}
	d1 := newDeployment(stateKey)
	d1.Name = guuid.NewString()
	d2 := newDeployment(stateKey)
	d2.Name = guuid.NewString()

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(d1), newUnstructured(d2)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	expectedObjectCountMetrics := map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 2,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:  2,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:          2,
	}
	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         1,
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: 1,
		{deploymentKind, metrics.MetricPhaseResync}:                   1,
	}
	deploymentKindIndex := 0
	validateMetrics(t, deploymentKind, &deploymentKindIndex, expectedObjectCountMetrics, expectedSuccessMetrics)
}
