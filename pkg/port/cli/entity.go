package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
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

func (c *PortClient) SearchEntitiesByDatasource(ctx context.Context, datasourcePrefix, datasourceSuffix string) ([]port.Entity, error) {
	pb := &port.ResponseBody{}
	body := port.DatasourceSearchBody{
		DatasourcePrefix: datasourcePrefix,
		DatasourceSuffix: datasourceSuffix,
	}
	resp, err := c.Client.R().
		SetBody(body).
		SetHeader("Accept", "application/json").
		SetQueryParam("exclude_calculated_properties", "true").
		SetQueryParamsFromValues(url.Values{
			"include": []string{"blueprint", "identifier"},
		}).
		SetResult(&pb).
		Post("/v1/blueprints/entities/datasource-entities")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to search entities by datasource, got: %s", resp.Body())
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
	processedStateKey := fmt.Sprintf("(statekey/%s)", stateKey)
	portEntities, err := c.SearchEntitiesByDatasource(ctx, "port-k8s-exporter", processedStateKey)
	if err != nil {
		return fmt.Errorf("error searching Port entities: %v", err)
	}

	successCount := 0
	failedCount := 0
	for _, portEntity := range portEntities {
		_, ok := existingEntitiesSet[c.GetEntityIdentifierKey(&portEntity)]
		if !ok {
			err := c.DeleteEntity(ctx, portEntity.Identifier, portEntity.Blueprint, c.DeleteDependents)
			if err != nil {
				logger.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
				failedCount++
				continue
			}
			successCount++
			logger.Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		}
	}

	metrics.AddObjectCount(metrics.MetricKindReconciliation, metrics.MetricDeletedResult, metrics.MetricPhaseDelete, float64(successCount))
	metrics.AddObjectCount(metrics.MetricKindReconciliation, metrics.MetricFailedResult, metrics.MetricPhaseDelete, float64(failedCount))
	return nil
}

func (c *PortClient) GetEntityIdentifierKey(portEntity *port.Entity) string {
	return fmt.Sprintf("%s;%s", portEntity.Blueprint, portEntity.Identifier)
}

func (c *PortClient) BulkUpsertEntities(ctx context.Context, blueprint string, entities []port.EntityRequest, runID string, createMissingRelatedEntities bool) (*port.BulkUpsertResponse, error) {
	if len(entities) == 0 {
		return &port.BulkUpsertResponse{OK: true, Entities: []port.BulkEntityResult{}, Errors: []port.BulkEntityError{}}, nil
	}
	if len(entities) > 20 {
		return nil, fmt.Errorf("bulk upsert supports maximum 20 entities per request, got %d", len(entities))
	}

	requestBody := port.BulkUpsertRequest{
		Entities: entities,
	}

	pb := &port.BulkUpsertResponse{}
	resp, err := c.Client.R().
		SetBody(requestBody).
		SetPathParam("blueprint_identifier", blueprint).
		SetQueryParam("upsert", "true").
		SetQueryParam("merge", "true").
		SetQueryParam("run_id", runID).
		SetQueryParam("create_missing_related_entities", strconv.FormatBool(createMissingRelatedEntities)).
		SetResult(&pb).
		Post("v1/blueprints/{blueprint_identifier}/entities/bulk")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 {
		return nil, fmt.Errorf("failed to bulk upsert entities, got status %d: %s", resp.StatusCode(), resp.Body())
	}
	return pb, nil
}
