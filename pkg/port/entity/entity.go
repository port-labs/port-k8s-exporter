package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"reflect"
	"strconv"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CheckIfOwnEntity(entity port.EntityRequest, portClient *cli.PortClient, eventSource port.EventSource) (*bool, error) {
	portEntities, err := portClient.SearchEntities(context.Background(), port.SearchBody{
		Rules: []port.Rule{
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    "port-k8s-exporter",
			},
			{
				Property: "$identifier",
				Operator: "=",
				Value:    entity.Identifier,
			},
			{
				Property: "$datasource",
				Operator: "contains",
				Value:    fmt.Sprintf("(statekey/%s)", config.ApplicationConfig.StateKey),
			},
			{
				Property: "$blueprint",
				Operator: "=",
				Value:    entity.Blueprint,
			},
		},
		Combinator: "and",
	})
	if err != nil {
		return nil, err
	}

	if eventSource == port.LiveEventsSource {
		portAutoEntities, err := portClient.SearchEntities(context.Background(), port.SearchBody{
			Rules: []port.Rule{
				{
					Property: "$datasource",
					Operator: "contains",
					Value:    "port-auto-k8s-exporter",
				},
				{
					Property: "$identifier",
					Operator: "=",
					Value:    entity.Identifier,
				},
				{
					Property: "$datasource",
					Operator: "contains",
					Value:    fmt.Sprintf("(statekey/%s)", config.ApplicationConfig.StateKey),
				},
				{
					Property: "$blueprint",
					Operator: "=",
					Value:    entity.Blueprint,
				},
			},
			Combinator: "and",
		})
		if err != nil {
			return nil, err
		}
		portEntities = append(portEntities, portAutoEntities...)
	}

	if len(portEntities) > 0 {
		result := true
		return &result, nil
	}
	result := false
	return &result, nil
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
			entity.Team, err = jq.ParseString(mapping.Team.(string), obj)
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
