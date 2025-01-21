package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"k8s.io/klog/v2"
)

func (c *PortClient) SearchEntities(ctx context.Context, body port.SearchBody) ([]port.Entity, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(body).
		SetHeader("Accept", "application/json").
		SetQueryParam("exclude_calculated_properties", "true").
		SetQueryParamsFromValues(url.Values{
			"include": []string{"blueprint", "identifier"},
		}).
		SetResult(&pb).
		Post("/v1/entities/search")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to search entities, got: %s", resp.Body())
	}
	return pb.Entities, nil
}

func (c *PortClient) ReadEntity(ctx context.Context, id string, blueprint string) (*port.Entity, error) {
	resp, err := c.Client.R().
		SetHeader("Accept", "application/json").
		SetQueryParam("exclude_calculated_properties", "true").
		SetPathParam(("blueprint"), blueprint).
		SetPathParam("identifier", id).
		Get("v1/blueprints/{blueprint}/entities/{identifier}")
	if err != nil {
		return nil, err
	}
	var pb port.ResponseBody
	err = json.Unmarshal(resp.Body(), &pb)
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to read entity, got: %s", resp.Body())
	}
	return &pb.Entity, nil
}

func (c *PortClient) CreateEntity(ctx context.Context, e *port.EntityRequest, runID string, createMissingRelatedEntities bool) (*port.Entity, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(e).
		SetPathParam(("blueprint"), e.Blueprint).
		SetQueryParam("upsert", "true").
		SetQueryParam("merge", "true").
		SetQueryParam("run_id", runID).
		SetQueryParam("create_missing_related_entities", strconv.FormatBool(createMissingRelatedEntities)).
		SetResult(&pb).
		Post("v1/blueprints/{blueprint}/entities")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create entity, got: %s", resp.Body())
	}
	return &pb.Entity, nil
}

func (c *PortClient) DeleteEntity(ctx context.Context, id string, blueprint string, deleteDependents bool) error {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetHeader("Accept", "application/json").
		SetPathParam("blueprint", blueprint).
		SetPathParam("identifier", id).
		SetQueryParam("delete_dependents", strconv.FormatBool(deleteDependents)).
		SetResult(pb).
		Delete("v1/blueprints/{blueprint}/entities/{identifier}")
	if err != nil {
		return err
	}
	if !pb.OK {
		return fmt.Errorf("failed to delete entity, got: %s", resp.Body())
	}
	return nil
}

func (c *PortClient) DeleteStaleEntities(ctx context.Context, stateKey string, existingEntitiesSet map[string]interface{}) error {
	portEntities, err := c.SearchEntities(ctx, port.SearchBody{
		Rules: []port.Rule{
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    "port-k8s-exporter",
			},
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    fmt.Sprintf("(statekey/%s)", stateKey),
			},
		},
		Combinator: "and",
	})
	if err != nil {
		return fmt.Errorf("error searching Port entities: %v", err)
	}

	for _, portEntity := range portEntities {
		_, ok := existingEntitiesSet[c.GetEntityIdentifierKey(&portEntity)]
		if !ok {
			err := c.DeleteEntity(ctx, portEntity.Identifier, portEntity.Blueprint, c.DeleteDependents)
			if err != nil {
				klog.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
				continue
			}
			klog.V(0).Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		}
	}

	return nil
}

func (c *PortClient) GetEntityIdentifierKey(portEntity *port.Entity) string {
	return fmt.Sprintf("%s;%s", portEntity.Blueprint, portEntity.Identifier)
}
