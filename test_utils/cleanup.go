package testing_init

import (
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	"github.com/stretchr/testify/assert"
)

func CheckResourcesExist(portClient *cli.PortClient, t *testing.T, blueprints []string, pages []string) {
	for _, bp := range blueprints {
		_, err := blueprint.GetBlueprint(portClient, bp)
		if err == nil {
			_ = blueprint.DeleteBlueprint(portClient, bp)
		}
		assert.Nil(t, err)
	}

	for _, p := range pages {
		_, err := page.GetPage(portClient, p)
		if err == nil {
			_ = page.DeletePage(portClient, p)
		}
		assert.Nil(t, err)
	}
}

func CheckResourcesDoesNotExist(portClient *cli.PortClient, t *testing.T, blueprints []string, pages []string) {
	for _, bp := range blueprints {
		_, err := blueprint.GetBlueprint(portClient, bp)
		if err != nil {
			_ = blueprint.DeleteBlueprint(portClient, bp)
		}
		assert.NotNil(t, err)
	}

	for _, p := range pages {
		_, err := page.GetPage(portClient, p)
		if err != nil {
			_ = page.DeletePage(portClient, p)
		}
		assert.NotNil(t, err)
	}
}
