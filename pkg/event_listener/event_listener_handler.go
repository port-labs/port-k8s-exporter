package event_listener

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type IEventListener interface {
	Run(resync func()) error
}

type Handler struct {
	eventListener     IEventListener
	controllerHandler *handlers.ControllersHandler
}

func StartEventHandler(eventListener IEventListener, controllerHandler *handlers.ControllersHandler, resync func(*handlers.ControllersHandler) (*handlers.ControllersHandler, error)) error {
	err := eventListener.Run(func() {
		klog.Infof("Resync request received. Recreating controllers for the new port configuration")
		newController, resyncErr := resync(controllerHandler)
		controllerHandler = newController

		if resyncErr != nil {
			utilruntime.HandleError(fmt.Errorf("error resyncing: %s", resyncErr.Error()))
		}
	})
	if err != nil {
		return err
	}

	return nil
}
