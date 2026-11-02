package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"reflect"
	"slices"
	"strconv"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

// ownershipSearchBatchSize caps identifiers per ownership search (Port "in" query).
const ownershipSearchBatchSize = 50

// FilterOwnedIdentifiers returns the subset of entities this exporter owns,
// grouped by blueprint. Ownership is checked with batched entity searches
// (one request per blueprint batch) instead of one search per entity.
func FilterOwnedIdentifiers(entities []port.EntityRequest, portClient *cli.PortClient, eventSource port.EventSource) (map[string][]string, error) {
	byBlueprint := make(map[string][]string)
	for _, e := range entities {
		identifier, ok := e.Identifier.(string)
		if !ok || identifier == "" {
			continue
		}
		byBlueprint[e.Blueprint] = append(byBlueprint[e.Blueprint], identifier)
	}

	ownedByBlueprint := make(map[string][]string)
	for blueprint, identifiers := range byBlueprint {
		for batch := range slices.Chunk(identifiers, ownershipSearchBatchSize) {
			ownedIDs, err := searchOwnedIdentifiers(portClient, blueprint, batch, "port-k8s-exporter")
			if err != nil {
				return nil, err
			}
			if eventSource == port.LiveEventsSource {
				autoOwned, err := searchOwnedIdentifiers(portClient, blueprint, batch, "port-auto-k8s-exporter")
				if err != nil {
					return nil, err
				}
				for id := range autoOwned {
					ownedIDs[id] = struct{}{}
				}
			}
			for _, id := range batch {
				if _, ok := ownedIDs[id]; ok {
					ownedByBlueprint[blueprint] = append(ownedByBlueprint[blueprint], id)
				}
			}
		}
	}
	return ownedByBlueprint, nil
}

func searchOwnedIdentifiers(portClient *cli.PortClient, blueprint string, identifiers []string, datasourcePrefix string) (map[string]struct{}, error) {
	entities, err := portClient.SearchEntities(context.Background(), port.SearchBody{
		Combinator: "and",
		Rules: []port.Rule{
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    datasourcePrefix,
			},
			{
				Property: "$identifier",
				Operator: "in",
				Value:    identifiers,
			},
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    fmt.Sprintf("(statekey/%s)", config.ApplicationConfig.StateKey),
			},
			{
				Property: "$blueprint",
				Operator: "=",
				Value:    blueprint,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	owned := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		owned[e.Identifier] = struct{}{}
	}
	return owned, nil
}

func HashAllEntities(entities []port.EntityRequest) (string, error) {
	h := fnv.New64a()
	for _, entity := range entities {
		entityBytes, err := json.Marshal(entity)
		if err != nil {
			return "", err
		}
		_, err = h.Write(entityBytes)
		if err != nil {
			return "", err
		}
	}
	return strconv.FormatUint(h.Sum64(), 10), nil
}

func MapEntities(obj interface{}, mappings []port.EntityMapping) ([]port.EntityRequest, error) {
	entities := make([]port.EntityRequest, 0, len(mappings))
	for _, entityMapping := range mappings {
		logger.Debugw("Mapping entity", "object", obj, "blueprint", entityMapping.Blueprint)
		portEntity, err := newEntityRequest(obj, entityMapping)
		if err != nil {
			logger.Errorw(fmt.Sprintf("failed to map entity. Error: %s", err.Error()), "object", obj, "blueprint", entityMapping.Blueprint, "error", err)
			return nil, fmt.Errorf("failed to map entity")
		}
		logger.Debugw("Mapped entity", "entity", portEntity.Identifier, "blueprint", portEntity.Blueprint)
		entities = append(entities, *portEntity)
	}

	return entities, nil
}

func newEntityRequest(obj interface{}, mapping port.EntityMapping) (*port.EntityRequest, error) {
	var err error
	entity := &port.EntityRequest{}

	if reflect.TypeOf(mapping.Identifier).Kind() == reflect.String {
		entity.Identifier, err = jq.ParseString(mapping.Identifier.(string), obj)
	} else if reflect.TypeOf(mapping.Identifier).Kind() == reflect.Map {
		entity.Identifier, err = jq.ParseMapRecursively(mapping.Identifier.(map[string]interface{}), obj)
	} else {
		return nil, fmt.Errorf("invalid identifier type '%T'", mapping.Identifier)
	}

	if err != nil {
		logger.Errorw(fmt.Sprintf("error parsing identifier. Error: %s", err.Error()), "object", obj, "mapping", mapping.Identifier, "error", err)
		return nil, err
	}
	if mapping.Title != "" {
		entity.Title, err = jq.ParseString(mapping.Title, obj)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error parsing title for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "mapping", mapping.Title, "entity", entity.Identifier, "error", err)
			return nil, err
		}
	}
	entity.Blueprint, err = jq.ParseString(mapping.Blueprint, obj)
	if err != nil {
		logger.Errorw(fmt.Sprintf("error parsing blueprint for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "mapping", mapping.Blueprint, "entity", entity.Identifier, "error", err)
		return nil, err
	}
	if mapping.Team != nil {
		if reflect.TypeOf(mapping.Team).Kind() == reflect.String {
			entity.Team, err = jq.ParseStringOrStringArray(mapping.Team.(string), obj)
		} else if reflect.TypeOf(mapping.Team).Kind() == reflect.Map {
			entity.Team, err = jq.ParseMapRecursively(mapping.Team.(map[string]interface{}), obj)
		} else {
			return nil, fmt.Errorf("invalid team type '%T'", mapping.Team)
		}
		if err != nil {
			logger.Errorw(fmt.Sprintf("error parsing team for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "mapping", mapping.Team, "entity", entity.Identifier, "error", err)
			return nil, err
		}
	}
	if mapping.Icon != "" {
		entity.Icon, err = jq.ParseString(mapping.Icon, obj)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error parsing icon for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "mapping", mapping.Icon, "entity", entity.Identifier, "error", err)
			return nil, err
		}
	}
	entity.Properties, err = jq.ParseMapInterface(mapping.Properties, obj)
	if err != nil {
		logger.Errorw(fmt.Sprintf("error parsing properties for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "entity", entity.Identifier, "error", err)
		return nil, err
	}
	entity.Relations, err = jq.ParseMapRecursively(mapping.Relations, obj)
	if err != nil {
		logger.Errorw(fmt.Sprintf("error parsing relations for entity %s. Error: %s", entity.Identifier, err.Error()), "object", obj, "entity", entity.Identifier, "error", err)
		return nil, err
	}

	return entity, err

}
