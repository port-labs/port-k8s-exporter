package testing_init

import (
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	"github.com/port-labs/port-k8s-exporter/pkg/port/scorecards"
	"github.com/stretchr/testify/assert"
)

var (
	defaultTestBlueprintIdentifiers = []string{"workload", "namespace", "cluster"}
	defaultTestScorecardIdentifiers = []string{"configuration", "highAvailability"}
	defaultTestPageIdentifiers      = []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}
)

// DeleteDefaultTestResources removes default integration resources from the shared Port CI org.
//
// Order matters: scorecard creation also writes rule entities to the _scorecard blueprint.
// Those must be removed (with dependents) before scorecard definitions, or later tests hit
// identifier_taken / has_dependents errors when cleaning up or deleting stale entities.
func DeleteDefaultTestResources(portClient *cli.PortClient) {
	_ = blueprint.DeleteBlueprintEntitiesWithDependents(portClient, "_scorecard")
	for _, scorecardIdentifier := range defaultTestScorecardIdentifiers {
		_ = scorecards.DeleteScorecard(portClient, "workload", scorecardIdentifier)
	}
	for _, blueprintIdentifier := range defaultTestBlueprintIdentifiers {
		_ = blueprint.DeleteBlueprintEntities(portClient, blueprintIdentifier)
		_ = blueprint.DeleteBlueprint(portClient, blueprintIdentifier)
	}
	for _, pageIdentifier := range defaultTestPageIdentifiers {
		_ = page.DeletePage(portClient, pageIdentifier)
	}
}

func CheckResourcesExistence(
	shouldExist bool,
	shouldDeleteEntities bool,
	portClient *cli.PortClient,
	t *testing.T,
	blueprints []string,
	pages []string,
	actions []string,
) {
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
			if shouldDeleteEntities {
				_ = blueprint.DeleteBlueprintEntities(portClient, bp)
			}
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
