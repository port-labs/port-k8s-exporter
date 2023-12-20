package signal

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func SetupSignalHandler() (stopCh chan struct{}) {

	stop := make(chan struct{})
	gracefulStop := false
	shutdownCh := make(chan os.Signal, 2)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdownCh
		if gracefulStop == false {
			fmt.Fprint(os.Stderr, "Received SIGTERM, exiting gracefully...\n")
			close(stop)
		}
		<-shutdownCh
		if gracefulStop == false {
			fmt.Fprint(os.Stderr, "Received SIGTERM again, exiting forcefully...\n")
			os.Exit(1)
		}
	}()

	go func() {
		<-stop
		gracefulStop = true
		close(shutdownCh)
	}()

	return stop
}
