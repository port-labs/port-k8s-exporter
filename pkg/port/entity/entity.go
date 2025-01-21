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
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CheckIfOwnEntity(entity port.EntityRequest, portClient *cli.PortClient) (*bool, error) {
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
		portEntity, err := newEntityRequest(obj, entityMapping)
		if err != nil {
			return nil, fmt.Errorf("invalid entity mapping '%#v': %v", entityMapping, err)
		}
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
		return nil, err
	}
	if mapping.Title != "" {
		entity.Title, err = jq.ParseString(mapping.Title, obj)
		if err != nil {
			return nil, err
		}
	}
	entity.Blueprint, err = jq.ParseString(mapping.Blueprint, obj)
	if err != nil {
		return nil, err
	}
	if mapping.Team != "" {
		entity.Team, err = jq.ParseInterface(mapping.Team, obj)
		if err != nil {
			return nil, err
		}
	}
	if mapping.Icon != "" {
		entity.Icon, err = jq.ParseString(mapping.Icon, obj)
		if err != nil {
			return nil, err
		}
	}
	entity.Properties, err = jq.ParseMapInterface(mapping.Properties, obj)
	if err != nil {
		return nil, err
	}
	entity.Relations, err = jq.ParseMapRecursively(mapping.Relations, obj)
	if err != nil {
		return nil, err
	}

	return entity, err

}
