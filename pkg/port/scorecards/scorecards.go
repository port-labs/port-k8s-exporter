package scorecards

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewScorecard(portClient *cli.PortClient, blueprintIdentifier string, scorecard port.Scorecard) (*port.Scorecard, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(scorecard).
		Post("v1/blueprints/" + blueprintIdentifier + "/scorecards")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create scorecard, got: %s", resp.Body())
	}
	return &pb.Scorecard, nil
}
