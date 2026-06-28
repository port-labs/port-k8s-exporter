package scorecards

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func DeleteScorecard(portClient *cli.PortClient, blueprintIdentifier string, scorecardIdentifier string) error {
	err := portClient.DeleteScorecard(blueprintIdentifier, scorecardIdentifier)
	if err != nil {
		return fmt.Errorf("error deleting Port scorecard: %v", err)
	}
	return nil
}

func CreateScorecard(portClient *cli.PortClient, blueprintIdentifier string, scorecard port.Scorecard) error {
	_, err := portClient.CreateScorecard(blueprintIdentifier, scorecard)
	if err != nil {
		return fmt.Errorf("error creating Port integration: %v", err)
	}

	return nil
}
