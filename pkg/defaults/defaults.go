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

var BlueprintsAsset = "assets/defaults/blueprints.json"
var ScorecardsAsset = "assets/defaults/scorecards.json"
var AppConfigAsset = "assets/defaults/appConfig.yaml"

func getDefaults() (*Defaults, error) {
	var bp []port.Blueprint
	file, err := os.ReadFile(BlueprintsAsset)
	if err != nil {
		klog.Infof("No default blueprints found. Skipping...")
	} else {
		err = json.Unmarshal(file, &bp)
		if err != nil {
			return nil, err
		}
	}

	var sc []ScorecardDefault
	file, err = os.ReadFile(ScorecardsAsset)
	if err != nil {
		klog.Infof("No default scorecards found. Skipping...")
	} else {
		err = json.Unmarshal(file, &sc)
		if err != nil {
			return nil, err
		}
	}

	var appConfig *port.IntegrationAppConfig
	file, err = os.ReadFile(AppConfigAsset)
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

// deconstructBlueprintsToCreationSteps takes a list of blueprints and returns a list of blueprints with only the
// required fields for creation, a list of blueprints with the required fields for creation and relations, and a list
// of blueprints with all fields for creation, relations, and calculation properties.
// This is done because there might be a case where a blueprint has a relation to another blueprint that
// hasn't been created yet.
func deconstructBlueprintsToCreationSteps(rawBlueprints []port.Blueprint) ([]port.Blueprint, [][]port.Blueprint) {
	var (
		bareBlueprints []port.Blueprint
		withRelations  []port.Blueprint
		fullBlueprints []port.Blueprint
	)

	for _, bp := range rawBlueprints {
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
		fullBlueprint.CalculationProperties = bp.CalculationProperties
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
			klog.Infof("Failed to create resources: %v.", err.Error())
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
	mutex := sync.Mutex{}

	for _, bp := range bareBlueprints {
		waitGroup.Add(1)
		go func(bp port.Blueprint) {
			defer waitGroup.Done()
			result, err := blueprint.NewBlueprint(portClient, bp)

			mutex.Lock()
			if err != nil {
				blueprintErrors = append(blueprintErrors, err)
			} else {
				createdBlueprints = append(createdBlueprints, result.Identifier)
			}
			mutex.Unlock()
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
				if err := scorecards.CreateScorecard(portClient, blueprintIdentifier, scorecard); err != nil {
					blueprintErrors = append(blueprintErrors, err)
				}
			}(blueprintScorecards.Blueprint, scorecard)
		}
	}
	waitGroup.Wait()

	if err := validateBlueprintErrors(createdBlueprints, blueprintErrors); err != nil {
		return err
	}

	if err := integration.CreateIntegration(portClient, config.StateKey, config.EventListenerType, defaults.AppConfig); err != nil {
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
		log.Printf("Failed to create resources. Rolling back blueprints: %v", err.BlueprintsToRollback)
		var rollbackWg sync.WaitGroup
		for _, identifier := range err.BlueprintsToRollback {
			rollbackWg.Add(1)
			go func(identifier string) {
				defer rollbackWg.Done()
				if err := blueprint.DeleteBlueprint(portClient, identifier); err != nil {
					klog.Warningf("Failed to rollback blueprint %s creation: %v", identifier, err)
				}
			}(identifier)
		}
		rollbackWg.Wait()
		return &ExceptionGroup{Message: err.Error(), Errors: err.Errors}
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
