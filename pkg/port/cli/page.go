package cli

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) CreatePage(p port.Page) error {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(p).
		SetResult(&pb).
		Post("v1/pages")
	if err != nil {
		return err
	}

	if resp.IsError() {
		return fmt.Errorf("failed to create page, got: %s", resp.Body())
	}
	return nil
}

func (c *PortClient) GetPage(identifier string) (*port.Page, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		SetPathParam("page", identifier).
		Get("v1/pages/{page}")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("failed to get page, got: %s", resp.Body())
	}
	return &pb.Pages, nil
}

func (c *PortClient) DeletePage(identifier string) error {
	resp, err := c.Client.R().
		SetPathParam("page", identifier).
		Delete("v1/pages/{page}")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("failed to delete page, got: %s", resp.Body())
	}
	return nil
}
