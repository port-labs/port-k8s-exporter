package event_handler

import (
	"fmt"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type IListener interface {
	Run(resync func()) error
}

type IStoppableRsync interface {
	Stop()
}

func StartEventHandler(eventListener IListener, controllerHandlerFactory func() (IStoppableRsync, error)) error {
	controllerHandler, err := controllerHandlerFactory()
	if err != nil {
		return err
	}

	return eventListener.Run(func() {
		klog.Infof("Resync request received. Recreating controllers for the new port configuration")
		controllerHandler.Stop()
		newController, resyncErr := controllerHandlerFactory()
		controllerHandler = newController

		if resyncErr != nil {
			utilruntime.HandleError(fmt.Errorf("error resyncing: %s", resyncErr.Error()))
		}
	})
}
