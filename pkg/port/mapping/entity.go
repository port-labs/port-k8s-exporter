package mapping

import (
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func NewEntity(obj interface{}, mapping port.EntityMapping) (*port.Entity, error) {
	var err error
	entity := &port.Entity{}
	entity.Identifier, err = jq.ParseString(mapping.Identifier, obj)
	if err != nil {
		return &port.Entity{}, err
	}
	if mapping.Title != "" {
		entity.Title, err = jq.ParseString(mapping.Title, obj)
		if err != nil {
			return &port.Entity{}, err
		}
	}
	entity.Blueprint, err = jq.ParseString(mapping.Blueprint, obj)
	if err != nil {
		return &port.Entity{}, err
	}
	if mapping.Team != "" {
		entity.Team, err = jq.ParseInterface(mapping.Team, obj)
		if err != nil {
			return &port.Entity{}, err
		}
	}
	if mapping.Icon != "" {
		entity.Icon, err = jq.ParseString(mapping.Icon, obj)
		if err != nil {
			return &port.Entity{}, err
		}
	}
	entity.Properties, err = jq.ParseMapInterface(mapping.Properties, obj)
	if err != nil {
		return &port.Entity{}, err
	}
	entity.Relations, err = jq.ParseMapInterface(mapping.Relations, obj)
	if err != nil {
		return &port.Entity{}, err
	}

	return entity, err

}
