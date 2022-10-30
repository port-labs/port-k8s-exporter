package k8s

import (
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/dynamic/fake"
	"os"
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
	t *testing.T

	resource config.Resource

	kubeclient *k8sfake.FakeDynamicClient
	portClient *cli.PortClient

	deploymentLister []runtime.Object
}

func newFixture(t *testing.T, portClientId string, portClientSecret string) *fixture {
	var err error
	f := &fixture{}

	f.t = t

	if portClientId == "" {
		portClientId = os.Getenv("PORT_CLIENT_ID")
	}
	if portClientSecret == "" {
		portClientSecret = os.Getenv("PORT_CLIENT_SECRET")
	}
	f.portClient, err = cli.New("https://api.getport.io", cli.WithHeader("User-Agent", "port-k8s-exporter/0.1"),
		cli.WithClientID(portClientId), cli.WithClientSecret(portClientSecret))
	if err != nil {
		t.Errorf("Error building Port client: %s", err.Error())
	}

	return f
}

func newResource(selectorQuery string, mappings []port.EntityMapping) config.Resource {
	return config.Resource{
		Kind: "apps/v1/deployments",
		Selector: config.Selector{
			Query: selectorQuery,
		},
		Port: config.Port{
			Entity: config.Entity{
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

func (f *fixture) newController() *Controller {
	f.kubeclient = k8sfake.NewSimpleDynamicClient(runtime.NewScheme())
	k8sI := dynamicinformer.NewDynamicSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())
	s := strings.SplitN(f.resource.Kind, "/", 3)
	gvr := schema.GroupVersionResource{Group: s[0], Version: s[1], Resource: s[2]}
	informer := k8sI.ForResource(gvr).Informer()
	c := NewController(f.resource, f.portClient, informer)

	for _, d := range f.deploymentLister {
		informer.GetIndexer().Add(d)
	}

	return c
}

func (f *fixture) run(item EventItem) {
	f.runController(item, false)
}

func (f *fixture) runExpectError(item EventItem) {
	f.runController(item, true)
}

func (f *fixture) runController(item EventItem, expectError bool) {
	c := f.newController()

	err := c.syncHandler(item)
	if !expectError && err != nil {
		f.t.Errorf("error syncing item: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing item, got nil")
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
	f := newFixture(t, "", "")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
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
	}
	f.resource = newResource("", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: CreateEventItem}

	f.run(item)
}

func TestUpdateDeployment(t *testing.T) {
	f := newFixture(t, "", "")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Title:      ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
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
	}
	f.resource = newResource("", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: UpdateEventItem}

	f.run(item)
}

func TestDeleteDeployment(t *testing.T) {
	f := newFixture(t, "", "")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	}
	f.resource = newResource("", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: DeleteEventItem}

	f.run(item)
}

func TestSelectorQueryFilterDeployment(t *testing.T) {
	f := newFixture(t, "", "")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"wrong-k8s-export-test-bp\"",
		},
	}
	f.resource = newResource(".metadata.name != \"port-k8s-exporter\"", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: DeleteEventItem}

	f.run(item)
}

func TestFailPortAuth(t *testing.T) {
	f := newFixture(t, "wrongclientid", "wrongclientsecret")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"k8s-export-test-bp\"",
		},
	}
	f.resource = newResource("", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: CreateEventItem}

	f.runExpectError(item)
}

func TestFailDeletePortEntity(t *testing.T) {
	f := newFixture(t, "", "")
	d := newDeployment()
	du := newUnstructured(d)

	f.deploymentLister = append(f.deploymentLister, du)

	entityMappings := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  "\"wrong-k8s-export-test-bp\"",
		},
	}
	f.resource = newResource("", entityMappings)

	item := EventItem{Key: getKey(d, t), Type: DeleteEventItem}

	f.runExpectError(item)
}
