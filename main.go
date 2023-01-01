package main

import (
	"flag"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
	"k8s.io/klog/v2"
	"os"
)

var (
	configFilePath   string
	resyncInterval   uint
	stateKey         string
	deleteDependents bool
	portBaseURL      string
	portClientId     string
	portClientSecret string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	stopCh := signal.SetupSignalHandler()

	exporterConfig, err := config.New(configFilePath, resyncInterval, stateKey)
	if err != nil {
		klog.Fatalf("Error building Port K8s Exporter config: %s", err.Error())
	}

	k8sClient, err := k8s.NewClient()
	if err != nil {
		klog.Fatalf("Error building K8s client: %s", err.Error())
	}

	portClient, err := cli.New(portBaseURL,
		cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", exporterConfig.StateKey)),
		cli.WithClientID(portClientId), cli.WithClientSecret(portClientSecret), cli.WithDeleteDependents(deleteDependents),
	)
	if err != nil {
		klog.Fatalf("Error building Port client: %s", err.Error())
	}

	klog.Info("Starting controllers handler")
	controllersHandler := handlers.NewControllersHandler(exporterConfig, k8sClient, portClient)
	controllersHandler.Handle(stopCh)
	klog.Info("Started controllers handler")
}

func init() {
	flag.StringVar(&configFilePath, "config", "", "Path to Port K8s Exporter config file. Required.")
	flag.StringVar(&stateKey, "state-key", "", "Port K8s Exporter state key id. Required.")
	flag.BoolVar(&deleteDependents, "delete-dependents", false, "Flag to enable deletion of dependent Port Entities. Optional.")
	flag.UintVar(&resyncInterval, "resync-interval", 0, "The re-sync interval in minutes. Optional.")
	flag.StringVar(&portBaseURL, "port-base-url", "https://api.getport.io", "Port base URL. Optional.")
	portClientId = os.Getenv("PORT_CLIENT_ID")
	portClientSecret = os.Getenv("PORT_CLIENT_SECRET")
}
