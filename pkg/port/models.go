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
		Team       interface{}            `json:"team,omitempty"`
		Properties map[string]interface{} `json:"properties"`
		Relations  map[string]interface{} `json:"relations"`
	}

	BlueprintProperty struct {
		Type        string            `json:"type,omitempty"`
		Title       string            `json:"title,omitempty"`
		Identifier  string            `json:"identifier,omitempty"`
		Default     string            `json:"default,omitempty"`
		Icon        string            `json:"icon,omitempty"`
		Format      string            `json:"format,omitempty"`
		Description string            `json:"description,omitempty"`
		Blueprint   string            `json:"blueprint,omitempty"`
		Pattern     string            `json:"pattern,omitempty"`
		Enum        []string          `json:"enum,omitempty"`
		EnumColors  map[string]string `json:"enumColors,omitempty"`
	}

	BlueprintMirrorProperty struct {
		Identifier string `json:"identifier,omitempty"`
		Title      string `json:"title,omitempty"`
		Path       string `json:"path,omitempty"`
	}

	BlueprintFormulaProperty struct {
		Identifier string `json:"identifier,omitempty"`
		Title      string `json:"title,omitempty"`
		Formula    string `json:"formula,omitempty"`
	}

	BlueprintSchema struct {
		Properties map[string]BlueprintProperty `json:"properties"`
		Required   []string                     `json:"required,omitempty"`
	}

	InvocationMethod struct {
		Type string `json:"type,omitempty"`
		Url  string `json:"url,omitempty"`
	}

	ChangelogDestination struct {
		Type string `json:"type,omitempty"`
		Url  string `json:"url,omitempty"`
	}

	ActionUserInputs = BlueprintSchema

	Blueprint struct {
		Meta
		Identifier           string                              `json:"identifier,omitempty"`
		Title                string                              `json:"title"`
		Icon                 string                              `json:"icon"`
		Description          string                              `json:"description"`
		Schema               BlueprintSchema                     `json:"schema"`
		FormulaProperties    map[string]BlueprintFormulaProperty `json:"formulaProperties"`
		MirrorProperties     map[string]BlueprintMirrorProperty  `json:"mirrorProperties"`
		ChangelogDestination *ChangelogDestination               `json:"changelogDestination,omitempty"`
		Relations            map[string]Relation                 `json:"relations"`
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
)

type SearchBody struct {
	Rules      []Rule `json:"rules"`
	Combinator string `json:"combinator"`
}

type ResponseBody struct {
	OK        bool      `json:"ok"`
	Entity    Entity    `json:"entity"`
	Blueprint Blueprint `json:"blueprint"`
	Action    Action    `json:"action"`
	Entities  []Entity  `json:"entities"`
}

type EntityMapping struct {
	Identifier string
	Title      string
	Blueprint  string
	Team       string
	Properties map[string]string
	Relations  map[string]string
}
