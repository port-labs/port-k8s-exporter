package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"golang.org/x/sync/errgroup"
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

	staleByBlueprint := make(map[string][]string)
	staleCount := 0
	for _, portEntity := range portEntities {
		if _, ok := existingEntitiesSet[c.GetEntityIdentifierKey(&portEntity)]; !ok {
			staleByBlueprint[portEntity.Blueprint] = append(staleByBlueprint[portEntity.Blueprint], portEntity.Identifier)
			staleCount++
		}
	}

	deletedCount, err := c.BulkDeleteEntities(ctx, staleByBlueprint, c.DeleteDependents)
	if err != nil {
		logger.Errorf("error deleting stale entities: %v", err)
	}
	logger.Infof("Deleted %d/%d stale entities", deletedCount, staleCount)

	metrics.AddObjectCount(metrics.MetricKindReconciliation, metrics.MetricDeletedResult, metrics.MetricPhaseDelete, float64(deletedCount))
	metrics.AddObjectCount(metrics.MetricKindReconciliation, metrics.MetricFailedResult, metrics.MetricPhaseDelete, float64(staleCount-deletedCount))
	return nil
}

func (c *PortClient) GetEntityIdentifierKey(portEntity *port.Entity) string {
	return fmt.Sprintf("%s;%s", portEntity.Blueprint, portEntity.Identifier)
}

type BulkUpsertError struct {
	StatusCode int
	Body       string
}

func (e *BulkUpsertError) Error() string {
	return fmt.Sprintf("failed to bulk upsert entities, got status %d: %s", e.StatusCode, e.Body)
}

func IsNonRetryableStatusCode(statusCode int) bool {
	return statusCode == 401 || statusCode == 403 || statusCode == 404 || statusCode == 422
}

func IsBulkNonRetryableError(err error) bool {
	bulkErr, ok := err.(*BulkUpsertError)
	if !ok {
		return false
	}
	return IsNonRetryableStatusCode(bulkErr.StatusCode)
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
		return nil, &BulkUpsertError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}
	return pb, nil
}

const (
	// maxBulkDeleteBatchSize is the Port API limit on identifiers per bulk delete request.
	maxBulkDeleteBatchSize   = 100
	maxConcurrentBulkDeletes = 10
)

// BulkDeleteEntities deletes the identifiers of each blueprint in concurrent batches and returns how many were deleted.
// Every batch is attempted and the first batch error is returned. Entities Port could not delete
// (e.g. already gone or protected by dependents) are logged and don't fail the call.
func (c *PortClient) BulkDeleteEntities(ctx context.Context, identifiersByBlueprint map[string][]string, deleteDependents bool) (int, error) {
	var g errgroup.Group
	g.SetLimit(maxConcurrentBulkDeletes)
	var deleted atomic.Int64
	for blueprint, identifiers := range identifiersByBlueprint {
		for batch := range slices.Chunk(identifiers, maxBulkDeleteBatchSize) {
			g.Go(func() error {
				n, err := c.bulkDeleteBatch(ctx, blueprint, batch, deleteDependents)
				deleted.Add(int64(n))
				return err
			})
		}
	}
	err := g.Wait()
	return int(deleted.Load()), err
}

func (c *PortClient) bulkDeleteBatch(ctx context.Context, blueprint string, identifiers []string, deleteDependents bool) (int, error) {
	pb := &port.BulkDeleteResponse{}
	resp, err := c.Client.R().
		SetContext(ctx).
		SetBody(port.BulkDeleteRequest{Entities: identifiers}).
		SetPathParam("blueprint_identifier", blueprint).
		SetQueryParam("delete_dependents", strconv.FormatBool(deleteDependents)).
		SetQueryParam("allow_partial_success", "true").
		SetResult(pb).
		Post("v1/blueprints/{blueprint_identifier}/bulk/entities/delete")
	if err != nil {
		return 0, err
	}
	if !pb.OK {
		return 0, fmt.Errorf("failed to bulk delete entities of blueprint '%s', got: %s", blueprint, resp.Body())
	}
	for _, entityErr := range pb.Errors {
		logger.Warningf("Could not delete Port entity '%s' of blueprint '%s': %s", entityErr.Identifier, blueprint, entityErr.Message)
	}
	return len(pb.DeletedEntities), nil
}
