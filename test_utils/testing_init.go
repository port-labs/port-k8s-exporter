package testing_init

import (
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"os"
	"path"
	"runtime"
	"testing"
)

func init() {
	_, filename, _, _ := runtime.Caller(0)
	dir := path.Join(path.Dir(filename), "..")
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}
	testing.Init()
	config.Init()
}
