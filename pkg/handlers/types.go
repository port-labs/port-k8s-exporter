package handlers

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

// SyncResult represents the result of a sync operation
type SyncResult struct {
	EntitiesSet               map[string]interface{}
	RawDataExamples           []interface{}
	ShouldDeleteStaleEntities bool
}

// EventActionType represents the type of event action
type EventActionType string

const (
	// CreateAction represents a create event
	CreateAction EventActionType = "create"
	// UpdateAction represents an update event
	UpdateAction EventActionType = "update"
	// DeleteAction represents a delete event
	DeleteAction EventActionType = "delete"
)

// EntityWithKind represents an entity with its associated kind
type EntityWithKind struct {
	Entity port.EntityRequest
	Kind   string
}
