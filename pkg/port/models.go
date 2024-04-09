package port

import (
	"time"
)

type (
	Meta struct {
		CreatedAt *time.Time `json:"createdAt,omitempty"`
		UpdatedAt *time.Time `json:"updatedAt,omitempty"`
		CreatedBy string     `json:"createdBy,omitempty"`
		UpdatedBy string     `json:"updatedBy,omitempty"`
	}
	AccessTokenResponse struct {
		Ok          bool   `json:"ok"`
		AccessToken string `json:"accessToken"`
		ExpiresIn   int64  `json:"expiresIn"`
		TokenType   string `json:"tokenType"`
	}
	Entity struct {
		Meta
		Identifier string                 `json:"identifier,omitempty"`
		Title      string                 `json:"title"`
		Blueprint  string                 `json:"blueprint"`
		Icon       string                 `json:"icon,omitempty"`
		Team       interface{}            `json:"team,omitempty"`
		Properties map[string]interface{} `json:"properties"`
		Relations  map[string]interface{} `json:"relations"`
	}

	Integration struct {
		InstallationId      string                 `json:"installationId,omitempty"`
		Title               string                 `json:"title,omitempty"`
		Version             string                 `json:"version,omitempty"`
		InstallationAppType string                 `json:"installationAppType,omitempty"`
		EventListener       *EventListenerSettings `json:"changelogDestination,omitempty"`
		Config              *IntegrationAppConfig  `json:"config,omitempty"`
		UpdatedAt           *time.Time             `json:"updatedAt,omitempty"`
	}

	Property struct {
		Type        string            `json:"type,omitempty"`
		Title       string            `json:"title,omitempty"`
		Identifier  string            `json:"identifier,omitempty"`
		Default     any               `json:"default,omitempty"`
		Icon        string            `json:"icon,omitempty"`
		Format      string            `json:"format,omitempty"`
		Description string            `json:"description,omitempty"`
		Blueprint   string            `json:"blueprint,omitempty"`
		Pattern     string            `json:"pattern,omitempty"`
		Enum        []string          `json:"enum,omitempty"`
		EnumColors  map[string]string `json:"enumColors,omitempty"`
	}

	ActionProperty struct {
		Type        string            `json:"type,omitempty"`
		Title       string            `json:"title,omitempty"`
		Identifier  string            `json:"identifier,omitempty"`
		Default     any               `json:"default,omitempty"`
		Icon        string            `json:"icon,omitempty"`
		Format      string            `json:"format,omitempty"`
		Description string            `json:"description,omitempty"`
		Blueprint   string            `json:"blueprint,omitempty"`
		Pattern     string            `json:"pattern,omitempty"`
		Enum        []string          `json:"enum,omitempty"`
		EnumColors  map[string]string `json:"enumColors,omitempty"`
		Visible     *bool             `json:"visible,omitempty"`
	}

	BlueprintMirrorProperty struct {
		Identifier string `json:"identifier,omitempty"`
		Title      string `json:"title,omitempty"`
		Path       string `json:"path,omitempty"`
	}

	BlueprintCalculationProperty struct {
		Identifier  string            `json:"identifier,omitempty"`
		Title       string            `json:"title,omitempty"`
		Calculation string            `json:"calculation,omitempty"`
		Colors      map[string]string `json:"colors,omitempty"`
		Colorized   bool              `json:"colorized,omitempty"`
		Format      string            `json:"format,omitempty"`
		Type        string            `json:"type,omitempty"`
	}

	BlueprintAggregationProperty struct {
		Title           string      `json:"title"`
		Target          string      `json:"target"`
		CalculationSpec interface{} `json:"calculationSpec"`
		Query           interface{} `json:"query,omitempty"`
		Description     string      `json:"description,omitempty"`
		Icon            string      `json:"icon,omitempty"`
		Type            string      `json:"type,omitempty"`
	}

	Schema struct {
		Properties map[string]Property `json:"properties"`
		Required   []string            `json:"required,omitempty"`
	}

	InvocationMethod struct {
		Type                 string `json:"type,omitempty"`
		Url                  string `json:"url,omitempty"`
		Organization         string `json:"org,omitempty"`
		Repository           string `json:"repo,omitempty"`
		Workflow             string `json:"workflow,omitempty"`
		OmitUserInputs       bool   `json:"omitUserInputs,omitempty"`
		OmitPayload          bool   `json:"omitPayload,omitempty"`
		ReportWorkflowStatus bool   `json:"reportWorkflowStatus,omitempty"`
	}

	ChangelogDestination struct {
		Type string `json:"type,omitempty"`
		Url  string `json:"url,omitempty"`
	}

	ActionUserInputs struct {
		Properties map[string]ActionProperty `json:"properties"`
		Required   []string                  `json:"required,omitempty"`
	}

	Blueprint struct {
		Meta
		Identifier            string                                  `json:"identifier,omitempty"`
		Title                 string                                  `json:"title,omitempty"`
		Icon                  string                                  `json:"icon"`
		Description           string                                  `json:"description"`
		Schema                Schema                                  `json:"schema"`
		CalculationProperties map[string]BlueprintCalculationProperty `json:"calculationProperties,omitempty"`
		AggregationProperties map[string]BlueprintAggregationProperty `json:"aggregationProperties,omitempty"`
		MirrorProperties      map[string]BlueprintMirrorProperty      `json:"mirrorProperties,omitempty"`
		ChangelogDestination  *ChangelogDestination                   `json:"changelogDestination,omitempty"`
		Relations             map[string]Relation                     `json:"relations,omitempty"`
	}

	Page struct {
		Identifier string      `json:"identifier"`
		Blueprint  string      `json:"blueprint,omitempty"`
		Title      string      `json:"title,omitempty"`
		Icon       string      `json:"icon,omitempty"`
		Widgets    interface{} `json:"widgets,omitempty"`
		Type       string      `json:"type,omitempty"`
	}

	Action struct {
		ID               string            `json:"id,omitempty"`
		Identifier       string            `json:"identifier,omitempty"`
		Description      string            `json:"description,omitempty"`
		Title            string            `json:"title,omitempty"`
		Icon             string            `json:"icon,omitempty"`
		UserInputs       ActionUserInputs  `json:"userInputs"`
		Trigger          string            `json:"trigger"`
		InvocationMethod *InvocationMethod `json:"invocationMethod,omitempty"`
	}

	Scorecard struct {
		Identifier string        `json:"identifier,omitempty"`
		Title      string        `json:"title,omitempty"`
		Filter     interface{}   `json:"filter,omitempty"`
		Rules      []interface{} `json:"rules,omitempty"`
	}

	Relation struct {
		Identifier string `json:"identifier,omitempty"`
		Title      string `json:"title,omitempty"`
		Target     string `json:"target,omitempty"`
		Required   bool   `json:"required,omitempty"`
		Many       bool   `json:"many,omitempty"`
	}

	Rule struct {
		Property string      `json:"property"`
		Operator string      `json:"operator"`
		Value    interface{} `json:"value"`
	}

	OrgKafkaCredentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	OrgDetails struct {
		OrgId string `json:"id"`
	}
)

type SearchBody struct {
	Rules      []Rule `json:"rules"`
	Combinator string `json:"combinator"`
}

type ResponseBody struct {
	OK               bool                `json:"ok"`
	Entity           Entity              `json:"entity"`
	Blueprint        Blueprint           `json:"blueprint"`
	Action           Action              `json:"action"`
	Entities         []Entity            `json:"entities"`
	Integration      Integration         `json:"integration"`
	KafkaCredentials OrgKafkaCredentials `json:"credentials"`
	OrgDetails       OrgDetails          `json:"organization"`
	Scorecard        Scorecard           `json:"scorecard"`
	Pages            Page                `json:"pages"`
}

type EntityMapping struct {
	Identifier string            `json:"identifier"`
	Title      string            `json:"title"`
	Blueprint  string            `json:"blueprint"`
	Icon       string            `json:"icon,omitempty"`
	Team       string            `json:"team,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Relations  map[string]string `json:"relations,omitempty"`
}

type EntityMappings struct {
	Mappings []EntityMapping `json:"mappings"`
}

type Port struct {
	Entity EntityMappings `json:"entity"`
}

type Selector struct {
	Query string
}

type Resource struct {
	Kind     string   `json:"kind"`
	Selector Selector `json:"selector,omitempty"`
	Port     Port     `json:"port"`
}

type EventListenerSettings struct {
	Type string `json:"type,omitempty"`
}

type KindConfig struct {
	Selector Selector
	Port     Port
}

type AggregatedResource struct {
	Kind        string
	KindConfigs []KindConfig
}

type IntegrationAppConfig struct {
	DeleteDependents             bool       `json:"deleteDependents,omitempty"`
	CreateMissingRelatedEntities bool       `json:"createMissingRelatedEntities,omitempty"`
	Resources                    []Resource `json:"resources,omitempty"`
	CRDSToDiscover               string     `json:"crdsToDiscover,omitempty"`
	OverwriteCRDActions          bool       `json:"overwriteCRDsAction,omitempty"`
}

type Config struct {
	ResyncInterval                  uint   `yaml:"resyncInterval,omitempty"`
	StateKey                        string `yaml:"stateKey,omitempty"`
	EventListenerType               string `yaml:"eventListenerType,omitempty"`
	CreateDefaultResources          bool   `yaml:"createDefaultResources,omitempty"`
	OverwriteConfigurationOnRestart bool   `yaml:"overwriteConfigurationOnRestart,omitempty"`
	// These Configurations are used only for setting up the Integration on installation or when using OverwriteConfigurationOnRestart flag.
	Resources                    []Resource `yaml:"resources,omitempty"`
	CRDSToDiscover               string     `yaml:"crdsToDiscover,omitempty"`
	OverwriteCRDActions          bool       `yaml:"overwriteCRDsAction,omitempty"`
	DeleteDependents             bool       `yaml:"deleteDependents,omitempty"`
	CreateMissingRelatedEntities bool       `yaml:"createMissingRelatedEntities,omitempty"`
}
