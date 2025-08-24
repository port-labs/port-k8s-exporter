package metrics_test

import (
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

type OverrideableFields struct {
	Identifier   string
	ItemsToParse string
	Blueprint    string
	Selector     string
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
		CreateDefaultResources:          false,
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

	metrics.RegisterMetrics()
	return &fixture{
		t:          t,
		k8sClient:  k8sClient,
		portClient: portClient,
		stateKey:   exporterConfig.StateKey,
	}
}

func newDeployment(stateKey string, name string) *appsv1.Deployment {
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
			Name:      name,
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
						{
							Name:  "second-container",
							Image: "second-image:latest",
						},
						{
							Name:  "third-container",
							Image: "third-image:latest",
						},
					},
				},
			},
		},
	}
}

func newDaemonSet(stateKey string, name string) *appsv1.DaemonSet {
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
			Name:      name,
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

func buildMappings(stateKey string, overrideFields map[string][]OverrideableFields) []port.Resource {
	blueprintId := getBlueprintId(stateKey)
	resources := make([]port.Resource, 0)
	for kind, overrideFields := range overrideFields {
		for _, overrideField := range overrideFields {
			selectorQuery := "true"
			if overrideField.Selector != "" {
				selectorQuery = overrideField.Selector
			}
			blueprint := fmt.Sprintf("\"%s\"", blueprintId)
			if overrideField.Blueprint != "" {
				blueprint = overrideField.Blueprint
			}
			identifier := overrideField.Identifier
			if identifier == "" {
				identifier = ".metadata.name"
			}
			newResource := port.Resource{
				Kind: kind,
				Selector: port.Selector{
					Query: selectorQuery,
				},
				Port: port.Port{
					Entity: port.EntityMappings{
						Mappings: []port.EntityMapping{
							{
								Identifier: identifier,
								Blueprint:  blueprint,
							},
						},
					},
				},
			}
			if overrideField.ItemsToParse != "" {
				newResource.Port.ItemsToParse = overrideField.ItemsToParse
			}
			resources = append(resources, newResource)
		}
	}
	return resources
}

func validateMetrics(
	t *testing.T,
	kind string,
	kindIndex *int,
	expectedObjectCountMetrics map[[2]string]float64,
	expectedSuccessMetrics map[[2]string]float64,
) {
	kindLabel := metrics.GetKindLabel(kind, kindIndex)
	defaultObjectCountMetrics := map[[3]string]float64{
		{kindLabel, metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:                  0,
		{kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseExtract}:                        0,
		{kindLabel, metrics.MetricTransformResult, metrics.MetricPhaseTransform}:                   0,
		{kindLabel, metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}:                 0,
		{kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseTransform}:                      0,
		{kindLabel, metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:                           0,
		{kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseLoad}:                           0,
		{metrics.MetricKindReconciliation, metrics.MetricDeletedResult, metrics.MetricPhaseDelete}: 0,
		{metrics.MetricKindReconciliation, metrics.MetricFailedResult, metrics.MetricPhaseDelete}:  0,
	}

	defaultSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         float64(metrics.PhaseFailed),
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseFailed),
		{kind, metrics.MetricPhaseResync}:                             float64(metrics.PhaseFailed),
	}

	for defaultMetric, defaultMetricValue := range defaultObjectCountMetrics {
		expectedValue := defaultMetricValue
		if val, ok := expectedObjectCountMetrics[[2]string{defaultMetric[1], defaultMetric[2]}]; ok {
			expectedValue = val
		}
		val, err := metrics.GetMetricValue(metrics.MetricObjectCountName, map[metrics.PortMetricLabel]string{
			metrics.MetricLabelKind:            defaultMetric[0],
			metrics.MetricLabelObjectCountType: defaultMetric[1],
			metrics.MetricLabelPhase:           defaultMetric[2],
		})
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, val, fmt.Sprintf("metric: %s, kind: %s, object_count_type: %s, phase: %s", metrics.MetricObjectCountName, defaultMetric[0], defaultMetric[1], defaultMetric[2]))
	}

	for defaultMetric, defaultMetricValue := range defaultSuccessMetrics {
		expectedValue := defaultMetricValue
		if val, ok := expectedSuccessMetrics[defaultMetric]; ok {
			expectedValue = val
		}
		val, err := metrics.GetMetricValue(metrics.MetricSuccessName, map[metrics.PortMetricLabel]string{
			metrics.MetricLabelKind:  defaultMetric[0],
			metrics.MetricLabelPhase: defaultMetric[1],
		})
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, val, fmt.Sprintf("metric: %s, kind: %s, phase: %s", metrics.MetricSuccessName, defaultMetric[0], defaultMetric[1]))
	}
}

func TestMetricsPopulation_SuccessfullResync(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		deploymentKind: {
			{
				Identifier: ".metadata.name + \"first\"",
			},
			{
				Identifier: ".metadata.name + \"second\"",
			},
		},
	})
	d1 := newDeployment(stateKey, guuid.NewString())
	d2 := newDeployment(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(d1), newUnstructured(d2)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	expectedObjectCountMetrics := map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 2,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:  2,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:          2,
	}
	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         float64(metrics.PhaseSucceeded),
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSucceeded),
		{deploymentKind, metrics.MetricPhaseResync}:                   float64(metrics.PhaseSucceeded),
	}
	for i := 0; i < 2; i++ {
		validateMetrics(t, deploymentKind, &i, expectedObjectCountMetrics, expectedSuccessMetrics)
	}
}

func TestMetricsPopulation_Selector(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Selector: ".metadata.name | contains(\"system\") | not",
			},
			{
				Selector: ".metadata.name | contains(\"system\")",
			},
		},
	})
	ds1 := newDaemonSet(stateKey, "system-ds")
	ds2 := newDaemonSet(stateKey, "system-ds2")
	ds3 := newDaemonSet(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(ds1), newUnstructured(ds2), newUnstructured(ds3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         float64(metrics.PhaseSucceeded),
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSucceeded),
		{daemonSetKind, metrics.MetricPhaseResync}:                    float64(metrics.PhaseSucceeded),
	}
	firstDaemonSetKindIndex := 0
	validateMetrics(t, daemonSetKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:  3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:   1,
		{metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}: 2,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:           1,
	}, expectedSuccessMetrics)

	secondDaemonSetKindIndex := 1
	validateMetrics(t, daemonSetKind, &secondDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:  3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:   2,
		{metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}: 1,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:           2,
	}, expectedSuccessMetrics)
}

func TestMetricsPopulation_ItemsToParse(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		deploymentKind: {
			{
				Identifier:   ".item.name",
				ItemsToParse: ".spec.template.spec.containers",
			},
		},
	})
	d1 := newDeployment(stateKey, guuid.NewString())
	d2 := newDeployment(stateKey, guuid.NewString())
	d3 := newDeployment(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(d1), newUnstructured(d2), newUnstructured(d3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	firstDaemonSetKindIndex := 0
	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         float64(metrics.PhaseSucceeded),
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSucceeded),
		{deploymentKind, metrics.MetricPhaseResync}:                   float64(metrics.PhaseSucceeded),
	}
	validateMetrics(t, deploymentKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:  9,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:          9,
	}, expectedSuccessMetrics)
}

func TestMetricsPopulation_Delete(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Selector: ".metadata.name | contains(\"system\") | not",
			},
		},
	})
	ds1 := newDaemonSet(stateKey, "system-ds")
	ds2 := newDaemonSet(stateKey, "system-ds2")
	ds3 := newDaemonSet(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(ds1), newUnstructured(ds2), newUnstructured(ds3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	existingIntegration, err := integration.GetIntegration(f.portClient, f.stateKey)
	if err != nil {
		t.Errorf("error getting integration: %v", err)
	}
	existingIntegration.Config.Resources = buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Selector: ".metadata.name | contains(\"system\")",
			},
		},
	})
	integration.PatchIntegration(f.portClient, stateKey, existingIntegration)
	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.MAPPING_CHANGED)

	expectedSuccessMetrics := map[[2]string]float64{
		{metrics.MetricKindResync, metrics.MetricPhaseResync}:         float64(metrics.PhaseSucceeded),
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSucceeded),
		{daemonSetKind, metrics.MetricPhaseResync}:                    float64(metrics.PhaseSucceeded),
	}
	firstDaemonSetKindIndex := 0
	validateMetrics(t, daemonSetKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:  3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:   2,
		{metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}: 1,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:           2,
		{metrics.MetricDeletedResult, metrics.MetricPhaseDelete}:        1,
	}, expectedSuccessMetrics)
}

func TestMetricsPopulation_InvalidSelectorMapping(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Selector: ".notexist",
			},
			{
				Selector: ".metadata.name | contains(\"system\")",
			},
		},
	})
	ds1 := newDaemonSet(stateKey, "system-ds")
	ds2 := newDaemonSet(stateKey, "system-ds2")
	ds3 := newDaemonSet(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(ds1), newUnstructured(ds2), newUnstructured(ds3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	firstDaemonSetKindIndex := 0
	validateMetrics(t, daemonSetKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 3,
		{metrics.MetricFailedResult, metrics.MetricPhaseTransform}:     3,
	}, map[[2]string]float64{
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSkipped),
	})

	secondDaemonSetKindIndex := 1
	validateMetrics(t, daemonSetKind, &secondDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}:  3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:   2,
		{metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform}: 1,
		{metrics.MetricLoadedResult, metrics.MetricPhaseLoad}:           2,
	}, map[[2]string]float64{
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSkipped),
	})
}

func TestMetricsPopulation_InvalidIdentifierMapping(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Identifier: ".notexist",
			},
		},
	})
	ds1 := newDaemonSet(stateKey, "system-ds")
	ds2 := newDaemonSet(stateKey, "system-ds2")
	ds3 := newDaemonSet(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(ds1), newUnstructured(ds2), newUnstructured(ds3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	firstDaemonSetKindIndex := 0
	validateMetrics(t, daemonSetKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 3,
		{metrics.MetricFailedResult, metrics.MetricPhaseTransform}:     3,
	}, map[[2]string]float64{
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSkipped),
	})
}

func TestMetricsPopulation_NonExistBlueprintMapping(t *testing.T) {
	stateKey := guuid.NewString()
	resources := buildMappings(stateKey, map[string][]OverrideableFields{
		daemonSetKind: {
			{
				Blueprint: "\"non-exist\"",
			},
		},
	})
	ds1 := newDaemonSet(stateKey, "system-ds")
	ds2 := newDaemonSet(stateKey, "system-ds2")
	ds3 := newDaemonSet(stateKey, guuid.NewString())

	f := newFixture(t, &fixtureConfig{stateKey: stateKey, resources: resources, existingObjects: []runtime.Object{newUnstructured(ds1), newUnstructured(ds2), newUnstructured(ds3)}})
	defer tearDownFixture(t, f)

	handlers.RunResync(&port.Config{StateKey: stateKey}, f.k8sClient, f.portClient, handlers.INITIAL_RESYNC)

	firstDaemonSetKindIndex := 0
	validateMetrics(t, daemonSetKind, &firstDaemonSetKindIndex, map[[2]string]float64{
		{metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract}: 3,
		{metrics.MetricTransformResult, metrics.MetricPhaseTransform}:  3,
		{metrics.MetricFailedResult, metrics.MetricPhaseLoad}:          3,
	}, map[[2]string]float64{
		{metrics.MetricKindReconciliation, metrics.MetricPhaseDelete}: float64(metrics.PhaseSkipped),
	})
}
