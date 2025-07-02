package defaults

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	"github.com/port-labs/port-k8s-exporter/pkg/port/scorecards"
	"gopkg.in/yaml.v3"
)

type ScorecardDefault struct {
	Blueprint  string           `json:"blueprint"`
	Scorecards []port.Scorecard `json:"data"`
}

type Defaults struct {
	Blueprints []port.Blueprint
	Scorecards []ScorecardDefault
	AppConfig  *port.IntegrationAppConfig
	Pages      []port.Page
}

var BlueprintsAsset = "assets/defaults/blueprints.json"
var ScorecardsAsset = "assets/defaults/scorecards.json"
var PagesAsset = "assets/defaults/pages.json"
var AppConfigAsset = "assets/defaults/appConfig.yaml"

func getDefaults() (*Defaults, error) {
	var bp []port.Blueprint
	file, err := os.ReadFile(BlueprintsAsset)
	if err != nil {
		logger.Infof("No default blueprints found. Skipping...")
	} else {
		err = json.Unmarshal(file, &bp)
		if err != nil {
			return nil, err
		}
	}

	var sc []ScorecardDefault
	file, err = os.ReadFile(ScorecardsAsset)
	if err != nil {
		logger.Infof("No default scorecards found. Skipping...")
	} else {
		err = json.Unmarshal(file, &sc)
		if err != nil {
			return nil, err
		}
	}

	var appConfig *port.IntegrationAppConfig
	file, err = os.ReadFile(AppConfigAsset)
	if err != nil {
		logger.Infof("No default appConfig found. Skipping...")
	} else {
		err = yaml.Unmarshal(file, &appConfig)
		if err != nil {
			return nil, err
		}
	}

	var pages []port.Page
	file, err = os.ReadFile(PagesAsset)
	if err != nil {
		logger.Infof("No default pages found. Skipping...")
	} else {
		err = yaml.Unmarshal(file, &pages)
		if err != nil {
			return nil, err
		}
	}

	return &Defaults{
		Blueprints: bp,
		Scorecards: sc,
		AppConfig:  appConfig,
		Pages:      pages,
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
		fullBlueprint.AggregationProperties = bp.AggregationProperties
		fullBlueprint.CalculationProperties = bp.CalculationProperties
		fullBlueprint.MirrorProperties = bp.MirrorProperties
		fullBlueprints = append(fullBlueprints, fullBlueprint)
	}

	return bareBlueprints, [][]port.Blueprint{withRelations, fullBlueprints}
}

type AbortDefaultCreationError struct {
	BlueprintsToRollback []string
	PagesToRollback      []string
	Errors               []error
}

func (e *AbortDefaultCreationError) Error() string {
	return "AbortDefaultCreationError"
}

func createResources(portClient *cli.PortClient, defaults *Defaults, shouldCreatePageForBlueprints bool) error {
	existingBlueprints := []string{}
	for _, bp := range defaults.Blueprints {
		if _, err := blueprint.GetBlueprint(portClient, bp.Identifier); err == nil {
			existingBlueprints = append(existingBlueprints, bp.Identifier)
		}
	}

	if len(existingBlueprints) > 0 {
		logger.Infof("Found existing blueprints: %v, skipping default resources creation", existingBlueprints)
		return nil
	}

	bareBlueprints, patchStages := deconstructBlueprintsToCreationSteps(defaults.Blueprints)
	waitGroup := sync.WaitGroup{}
	var resourceErrors []error
	mutex := sync.Mutex{}
	createdBlueprints := []string{}

	for _, bp := range bareBlueprints {
		waitGroup.Add(1)
		go func(bp port.Blueprint) {
			defer waitGroup.Done()
			var result *port.Blueprint
			var err error
			if shouldCreatePageForBlueprints {
				result, err = blueprint.NewBlueprint(portClient, bp)
			} else {
				result, err = blueprint.NewBlueprintWithoutPage(portClient, bp)
			}
			mutex.Lock()
			if err != nil {
				logger.Warningf("Failed to create blueprint %s: %v", bp.Identifier, err.Error())
				resourceErrors = append(resourceErrors, err)
			} else {
				logger.Infof("Created blueprint %s", result.Identifier)
				createdBlueprints = append(createdBlueprints, bp.Identifier)
			}
			mutex.Unlock()
		}(bp)
	}
	waitGroup.Wait()

	if len(resourceErrors) > 0 {
		return &AbortDefaultCreationError{
			BlueprintsToRollback: createdBlueprints,
			Errors:               resourceErrors,
		}
	}

	for _, patchStage := range patchStages {
		for _, bp := range patchStage {
			waitGroup.Add(1)
			go func(bp port.Blueprint) {
				defer waitGroup.Done()
				if _, err := blueprint.PatchBlueprint(portClient, bp); err != nil {
					mutex.Lock()
					logger.Warningf("Failed to patch blueprint %s: %v", bp.Identifier, err.Error())
					resourceErrors = append(resourceErrors, err)
					mutex.Unlock()
				}
			}(bp)
		}
		waitGroup.Wait()

		if len(resourceErrors) > 0 {
			return &AbortDefaultCreationError{
				BlueprintsToRollback: createdBlueprints,
				Errors:               resourceErrors,
			}
		}
	}

	for _, blueprintScorecards := range defaults.Scorecards {
		for _, scorecard := range blueprintScorecards.Scorecards {
			waitGroup.Add(1)
			go func(blueprintIdentifier string, scorecard port.Scorecard) {
				defer waitGroup.Done()
				if err := scorecards.CreateScorecard(portClient, blueprintIdentifier, scorecard); err != nil {
					logger.Warningf("Failed to create scorecard for blueprint %s: %v", blueprintIdentifier, err.Error())
				}
			}(blueprintScorecards.Blueprint, scorecard)
		}
	}
	waitGroup.Wait()

	for _, pageToCreate := range defaults.Pages {
		waitGroup.Add(1)
		go func(p port.Page) {
			defer waitGroup.Done()
			if err := page.CreatePage(portClient, p); err != nil {
				logger.Warningf("Failed to create page %s: %v", p.Identifier, err.Error())
			} else {
				logger.Infof("Created page %s", p.Identifier)
			}
		}(pageToCreate)
	}
	waitGroup.Wait()

	return nil
}

func initializeDefaults(portClient *cli.PortClient, defaults *Defaults, shouldCreatePageForBlueprints bool) error {
	if err := createResources(portClient, defaults, shouldCreatePageForBlueprints); err != nil {
		if abortErr, ok := err.(*AbortDefaultCreationError); ok {
			logger.Warningf("Rolling back blueprints due to creation error")
			for _, blueprintID := range abortErr.BlueprintsToRollback {
				if err := blueprint.DeleteBlueprint(portClient, blueprintID); err != nil {
					logger.Warningf("Failed to rollback blueprint %s: %v", blueprintID, err)
				} else {
					logger.Infof("Successfully rolled back blueprint %s", blueprintID)
				}
			}
		}
		logger.Warningf("Error creating default resources: %v", err)
		return err
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
