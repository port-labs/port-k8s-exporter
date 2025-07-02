package event_handler

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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
		logger.Errorw("error resyncing", "error", err.Error())
		utilruntime.HandleError(fmt.Errorf("error resyncing: %s", err.Error()))
	}

	return eventListener.Run(func() {
		logger.Info("Resync request received. Recreating controllers for the new port configuration")
		if controllerHandler != (*handlers.ControllersHandler)(nil) {
			controllerHandler.Stop()
		}

		newController, resyncErr := initControllerHandler()
		controllerHandler = newController

		if resyncErr != nil {
			logger.Errorw("error resyncing", "error", resyncErr.Error())
			utilruntime.HandleError(fmt.Errorf("error resyncing: %s", resyncErr.Error()))
		}
	})
}
