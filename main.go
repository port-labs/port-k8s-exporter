package main

import (
	"flag"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/klog/v2"
	"os"
	"time"
)

var (
	configFilePath   string
	resyncInterval   uint
	portBaseURL      string
	portClientId     string
	portClientSecret string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	stopCh := signal.SetupSignalHandler()

	klog.Info("Reading config file")
	exporterConfig, err := config.New(configFilePath)
	if err != nil {
		klog.Fatalf("Error building Port K8s Exporter config: %s", err.Error())
	}

	k8sClient, err := k8s.NewClient()
	if err != nil {
		klog.Fatalf("Error building K8s client: %s", err.Error())
	}

	portClient, err := cli.New(portBaseURL, cli.WithHeader("User-Agent", "port-k8s-exporter/0.1"),
		cli.WithClientID(portClientId), cli.WithClientSecret(portClientSecret))
	if err != nil {
		klog.Fatalf("Error building Port client: %s", err.Error())
	}

	resync := time.Minute * time.Duration(resyncInterval)
	informersFactory := dynamicinformer.NewDynamicSharedInformerFactory(k8sClient.DynamicClient, resync)
	controllers := make([]*k8s.Controller, 0, len(exporterConfig.Resources))

	for _, resource := range exporterConfig.Resources {
		var gvr schema.GroupVersionResource
		gvr, err = k8s.GetGVRFromResource(k8sClient.DiscoveryMapper, resource.Kind)
		if err != nil {
			klog.Fatalf("Error getting GVR for resource '%s': %s", resource.Kind, err.Error())
		}

		informer := informersFactory.ForResource(gvr)
		controller := k8s.NewController(resource, portClient, informer.Informer())
		controllers = append(controllers, controller)
	}

	klog.Info("Starting informers")
	informersFactory.Start(stopCh)
	klog.Info("Starting controllers")
	for _, controller := range controllers {
		if err = controller.Run(1, stopCh); err != nil {
			klog.Fatalf("Error running controller: %s", err.Error())
		}
	}
	klog.Info("Started controllers")
	<-stopCh
	klog.Info("Shutting down controllers")
	for _, controller := range controllers {
		controller.Shutdown()
	}
	klog.Info("Exporter exiting")
}

func init() {
	flag.StringVar(&configFilePath, "config", "", "Path to Port K8s Exporter config file. Required.")
	flag.UintVar(&resyncInterval, "resync-interval", 0, "The re-sync interval in minutes. Optional.")
	flag.StringVar(&portBaseURL, "port-base-url", "https://api.getport.io", "Port base URL. Optional.")
	portClientId = os.Getenv("PORT_CLIENT_ID")
	portClientSecret = os.Getenv("PORT_CLIENT_SECRET")
}
