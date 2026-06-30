package cli

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) CreateScorecard(blueprintIdentifier string, scorecard port.Scorecard) (*port.Scorecard, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		SetBody(scorecard).
		SetPathParam("blueprint", blueprintIdentifier).
		Post("v1/blueprints/{blueprint}/scorecards")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create scorecard, got: %s", resp.Body())
	}
	return &pb.Scorecard, nil
}
