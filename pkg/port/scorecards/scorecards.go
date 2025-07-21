package scorecards

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CreateScorecard(portClient *cli.PortClient, blueprintIdentifier string, scorecard port.Scorecard) error {
	_, err := portClient.CreateScorecard(blueprintIdentifier, scorecard)
	if err != nil {
		return fmt.Errorf("error creating Port integration: %v", err)
	}

	return nil
}
