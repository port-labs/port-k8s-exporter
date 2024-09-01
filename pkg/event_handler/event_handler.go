package event_handler

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type IListener interface {
	Run(resync func()) error
}

type IStoppableRsync interface {
	Stop()
}

func Start(eventListener IListener, initControllerHandler func() (IStoppableRsync, error)) error {
	controllerHandler, err := initControllerHandler()
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error resyncing: %s", err.Error()))
	}

	return eventListener.Run(func() {
		klog.Infof("Resync request received. Recreating controllers for the new port configuration")
		if controllerHandler != (*handlers.ControllersHandler)(nil) {
			controllerHandler.Stop()
		}

		newController, resyncErr := initControllerHandler()
		controllerHandler = newController

		if resyncErr != nil {
			utilruntime.HandleError(fmt.Errorf("error resyncing: %s", resyncErr.Error()))
		}
	})
}
