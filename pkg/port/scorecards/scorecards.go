package scorecards

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CreateScorecard(portClient *cli.PortClient, blueprintIdentifier string, scorecard port.Scorecard) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	_, err = portClient.CreateScorecard(blueprintIdentifier, scorecard)
	if err != nil {
		return fmt.Errorf("error creating Port integration: %v", err)
	}

	return nil
}
