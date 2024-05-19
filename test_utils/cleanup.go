package testing_init

import (
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	"github.com/stretchr/testify/assert"
)

func CheckResourcesExistence(shouldExist bool, portClient *cli.PortClient, t *testing.T, blueprints []string, pages []string, actions []string) {
	for _, a := range actions {
		_, err := cli.GetAction(portClient, a)
		if err == nil {
			_ = cli.DeleteAction(portClient, a)
		}
		if shouldExist {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
		}
	}

	for _, bp := range blueprints {
		_, err := blueprint.GetBlueprint(portClient, bp)
		if err == nil {
			_ = blueprint.DeleteBlueprint(portClient, bp)
		}
		if shouldExist {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
		}
	}

	for _, p := range pages {
		_, err := page.GetPage(portClient, p)
		if err == nil {
			_ = page.DeletePage(portClient, p)
		}
		if shouldExist {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
		}
	}
}
