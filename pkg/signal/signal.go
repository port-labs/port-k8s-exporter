package signal

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func SetupSignalHandler() (stopCh chan struct{}) {
	mutex := sync.Mutex{}
	stop := make(chan struct{})
	gracefulStop := false
	shutdownCh := make(chan os.Signal, 2)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdownCh
		mutex.Lock()
		if gracefulStop == false {
			fmt.Fprint(os.Stderr, "Received SIGTERM, exiting gracefully...\n")
			close(stop)
		}
		mutex.Unlock()
		<-shutdownCh
		mutex.Lock()
		if gracefulStop == false {
			fmt.Fprint(os.Stderr, "Received SIGTERM again, exiting forcefully...\n")
			os.Exit(1)
		}
		mutex.Unlock()
	}()

	go func() {
		<-stop
		mutex.Lock()
		gracefulStop = true
		mutex.Unlock()
		close(shutdownCh)
	}()

	return stop
}
