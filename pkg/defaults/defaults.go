package defaults

import (
	"encoding/json"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/port/scorecards"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
	"log"
	"os"
	"sync"
)

type ScorecardDefault struct {
	Blueprint  string           `json:"blueprint"`
	Scorecards []port.Scorecard `json:"data"`
}

type Defaults struct {
	Blueprints []port.Blueprint
	Scorecards []ScorecardDefault
	AppConfig  *port.IntegrationAppConfig
}

func getDefaults() (*Defaults, error) {
	var bp []port.Blueprint
	file, err := os.ReadFile("assets/defaults/blueprints.json")
	if err != nil {
		klog.Infof("No default blueprints found. Skipping...")
	} else {
		err = json.Unmarshal(file, &bp)
		if err != nil {
			return nil, err
		}
	}

	var sc []ScorecardDefault
	file, err = os.ReadFile("./assets/defaults/scorecards.json")
	if err != nil {
		klog.Infof("No default scorecards found. Skipping...")
	} else {
		err = json.Unmarshal(file, &sc)
		if err != nil {
			return nil, err
		}
	}

	var appConfig *port.IntegrationAppConfig
	file, err = os.ReadFile("./assets/defaults/appConfig.yaml")
	if err != nil {
		klog.Infof("No default appConfig found. Skipping...")
	} else {
		err = yaml.Unmarshal(file, &appConfig)
		if err != nil {
			return nil, err
		}
	}

	return &Defaults{
		Blueprints: bp,
		Scorecards: sc,
		AppConfig:  appConfig,
	}, nil
}

func deconstructBlueprintsToCreationSteps(rawBlueprints []port.Blueprint) ([]port.Blueprint, [][]port.Blueprint) {
	var (
		bareBlueprints []port.Blueprint
		withRelations  []port.Blueprint
		fullBlueprints []port.Blueprint
	)

	for _, bp := range append([]port.Blueprint{}, rawBlueprints...) {
		bareBlueprint := port.Blueprint{
			Identifier:  bp.Identifier,
			Title:       bp.Title,
			Icon:        bp.Icon,
			Description: bp.Description,
			Schema:      bp.Schema,
		}
		bareBlueprints = append(bareBlueprints, bareBlueprint)

		withRelation := bareBlueprint
		withRelation.Relations = bp.Relations
		withRelations = append(withRelations, withRelation)

		fullBlueprint := withRelation
		fullBlueprint.FormulaProperties = bp.FormulaProperties
		fullBlueprint.MirrorProperties = bp.MirrorProperties
		fullBlueprints = append(fullBlueprints, fullBlueprint)
	}

	return bareBlueprints, [][]port.Blueprint{withRelations, fullBlueprints}
}

type AbortDefaultCreationError struct {
	BlueprintsToRollback []string
	Errors               []error
}

func (e *AbortDefaultCreationError) Error() string {
	return "AbortDefaultCreationError"
}

func validateBlueprintErrors(createdBlueprints []string, blueprintErrors []error) *AbortDefaultCreationError {
	if len(blueprintErrors) > 0 {
		for _, err := range blueprintErrors {
			log.Printf("Failed to create resources: %v.", err.Error())
		}
		return &AbortDefaultCreationError{BlueprintsToRollback: createdBlueprints, Errors: blueprintErrors}
	}
	return nil
}

func createResources(portClient *cli.PortClient, defaults *Defaults, config *port.Config) *AbortDefaultCreationError {
	if _, err := integration.GetIntegration(portClient, config.StateKey); err == nil {
		return &AbortDefaultCreationError{Errors: []error{
			fmt.Errorf("integration with state key %s already exists", config.StateKey),
		}}
	}

	bareBlueprints, patchStages := deconstructBlueprintsToCreationSteps(defaults.Blueprints)

	waitGroup := sync.WaitGroup{}

	var blueprintErrors []error
	var createdBlueprints []string

	for _, bp := range bareBlueprints {
		waitGroup.Add(1)
		go func(bp port.Blueprint) {
			defer waitGroup.Done()
			result, err := blueprint.NewBlueprint(portClient, bp)

			if err != nil {
				blueprintErrors = append(blueprintErrors, err)
			} else {
				createdBlueprints = append(createdBlueprints, result.Identifier)
			}
		}(bp)
	}
	waitGroup.Wait()

	if err := validateBlueprintErrors(createdBlueprints, blueprintErrors); err != nil {
		return err
	}

	for _, patchStage := range patchStages {
		for _, bp := range patchStage {
			waitGroup.Add(1)
			go func(bp port.Blueprint) {
				defer waitGroup.Done()
				if _, err := blueprint.PatchBlueprint(portClient, bp); err != nil {
					blueprintErrors = append(blueprintErrors, err)
				}
			}(bp)
		}
		waitGroup.Wait()
	}

	if err := validateBlueprintErrors(createdBlueprints, blueprintErrors); err != nil {
		return err
	}

	for _, blueprintScorecards := range defaults.Scorecards {
		for _, scorecard := range blueprintScorecards.Scorecards {
			waitGroup.Add(1)
			go func(blueprintIdentifier string, scorecard port.Scorecard) {
				defer waitGroup.Done()
				if _, err := scorecards.NewScorecard(portClient, blueprintIdentifier, scorecard); err != nil {
					blueprintErrors = append(blueprintErrors, err)
				}
			}(blueprintScorecards.Blueprint, scorecard)
		}
	}
	waitGroup.Wait()

	if err := validateBlueprintErrors(createdBlueprints, blueprintErrors); err != nil {
		return err
	}

	if err := integration.NewIntegration(portClient, config.StateKey, config.EventListenerType, defaults.AppConfig); err != nil {
		log.Printf("Failed to create resources: %v.", err.Error())
		return &AbortDefaultCreationError{BlueprintsToRollback: createdBlueprints, Errors: []error{err}}
	}

	return nil
}

func initializeDefaults(portClient *cli.PortClient, config *port.Config) error {
	defaults, err := getDefaults()
	if err != nil {
		return err
	}

	if err := createResources(portClient, defaults, config); err != nil {
		if err != nil {
			log.Printf("Failed to create resources. Rolling back blueprints: %v", err.BlueprintsToRollback)
			var rollbackWg sync.WaitGroup
			for _, identifier := range err.BlueprintsToRollback {
				rollbackWg.Add(1)
				go func(identifier string) {
					defer rollbackWg.Done()
					_ = blueprint.DeleteBlueprint(portClient, identifier)
				}(identifier)
			}
			rollbackWg.Wait()
			return &ExceptionGroup{Message: err.Error(), Errors: err.Errors}
		}
		return fmt.Errorf("unknown error during resource creation")
	}

	return nil
}

type ExceptionGroup struct {
	Message string
	Errors  []error
}

func (e *ExceptionGroup) Error() string {
	return e.Message
}
