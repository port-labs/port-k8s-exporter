package defaults

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	"github.com/port-labs/port-k8s-exporter/pkg/port/scorecards"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
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

	var pages []port.Page
	file, err = os.ReadFile(PagesAsset)
	if err != nil {
		klog.Infof("No default pages found. Skipping...")
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

func validateResourcesErrors(createdBlueprints []string, createdPages []string, resourceErrors []error) *AbortDefaultCreationError {
	if len(resourceErrors) > 0 {
		for _, err := range resourceErrors {
			klog.Infof("Failed to create resources: %v.", err.Error())
		}
		return &AbortDefaultCreationError{BlueprintsToRollback: createdBlueprints, PagesToRollback: createdPages, Errors: resourceErrors}
	}
	return nil
}

func validateResourcesDoesNotExist(portClient *cli.PortClient, defaults *Defaults, config *port.Config) *AbortDefaultCreationError {
	var errors []error
	if _, err := integration.GetIntegration(portClient, config.StateKey); err == nil {
		klog.Warningf("Integration with state key %s already exists", config.StateKey)
		return &AbortDefaultCreationError{Errors: []error{
			fmt.Errorf("integration with state key %s already exists", config.StateKey),
		}}
	}

	for _, bp := range defaults.Blueprints {
		if _, err := blueprint.GetBlueprint(portClient, bp.Identifier); err == nil {
			klog.Warningf("Blueprint with identifier %s already exists", bp.Identifier)
			errors = append(errors, fmt.Errorf("blueprint with identifier %s already exists", bp.Identifier))
		}
	}

	for _, p := range defaults.Pages {
		if _, err := page.GetPage(portClient, p.Identifier); err == nil {
			klog.Warningf("Page with identifier %s already exists", p.Identifier)
			errors = append(errors, fmt.Errorf("page with identifier %s already exists", p.Identifier))
		}
	}

	if errors != nil {
		return &AbortDefaultCreationError{Errors: errors}
	}
	return nil
}

func createResources(portClient *cli.PortClient, defaults *Defaults, config *port.Config) *AbortDefaultCreationError {
	if err := validateResourcesDoesNotExist(portClient, defaults, config); err != nil {
		klog.Warningf("Failed to create resources: %v.", err.Errors)
		return err
	}

	bareBlueprints, patchStages := deconstructBlueprintsToCreationSteps(defaults.Blueprints)

	waitGroup := sync.WaitGroup{}

	var resourceErrors []error
	var createdBlueprints []string
	var createdPages []string
	mutex := sync.Mutex{}

	for _, bp := range bareBlueprints {
		waitGroup.Add(1)
		go func(bp port.Blueprint) {
			defer waitGroup.Done()
			result, err := blueprint.NewBlueprint(portClient, bp)

			mutex.Lock()
			if err != nil {
				klog.Warningf("Failed to create blueprint %s: %v", bp.Identifier, err.Error())
				resourceErrors = append(resourceErrors, err)
			} else {
				klog.Infof("Created blueprint %s", result.Identifier)
				createdBlueprints = append(createdBlueprints, result.Identifier)
			}
			mutex.Unlock()
		}(bp)
	}
	waitGroup.Wait()

	if err := validateResourcesErrors(createdBlueprints, createdPages, resourceErrors); err != nil {
		return err
	}

	for _, patchStage := range patchStages {
		for _, bp := range patchStage {
			waitGroup.Add(1)
			go func(bp port.Blueprint) {
				defer waitGroup.Done()
				if _, err := blueprint.PatchBlueprint(portClient, bp); err != nil {
					klog.Warningf("Failed to patch blueprint %s: %v", bp.Identifier, err.Error())
					resourceErrors = append(resourceErrors, err)
				}
			}(bp)
		}
		waitGroup.Wait()
	}

	if err := validateResourcesErrors(createdBlueprints, createdPages, resourceErrors); err != nil {
		return err
	}

	for _, blueprintScorecards := range defaults.Scorecards {
		for _, scorecard := range blueprintScorecards.Scorecards {
			waitGroup.Add(1)
			go func(blueprintIdentifier string, scorecard port.Scorecard) {
				defer waitGroup.Done()
				if err := scorecards.CreateScorecard(portClient, blueprintIdentifier, scorecard); err != nil {
					klog.Warningf("Failed to create scorecard for blueprint %s: %v", blueprintIdentifier, err.Error())
					resourceErrors = append(resourceErrors, err)
				}
			}(blueprintScorecards.Blueprint, scorecard)
		}
	}
	waitGroup.Wait()

	if err := validateResourcesErrors(createdBlueprints, createdPages, resourceErrors); err != nil {
		return err
	}

	for _, pageToCreate := range defaults.Pages {
		waitGroup.Add(1)
		go func(p port.Page) {
			defer waitGroup.Done()
			if err := page.CreatePage(portClient, p); err != nil {
				klog.Warningf("Failed to create page %s: %v", p.Identifier, err.Error())
				resourceErrors = append(resourceErrors, err)
			} else {
				klog.Infof("Created page %s", p.Identifier)
				createdPages = append(createdPages, p.Identifier)
			}
		}(pageToCreate)
	}
	waitGroup.Wait()

	if err := validateResourcesErrors(createdBlueprints, createdPages, resourceErrors); err != nil {
		return err
	}

	if err := integration.CreateIntegration(portClient, config.StateKey, config.EventListenerType, defaults.AppConfig); err != nil {
		klog.Warningf("Failed to create integration with default configuration. state key %s: %v", config.StateKey, err.Error())
		return &AbortDefaultCreationError{BlueprintsToRollback: createdBlueprints, PagesToRollback: createdPages, Errors: []error{err}}
	}

	return nil
}

func initializeDefaults(portClient *cli.PortClient, config *port.Config) error {
	defaults, err := getDefaults()
	if err != nil {
		return err
	}

	if err := createResources(portClient, defaults, config); err != nil {
		klog.Infof("Failed to create resources. Rolling back blueprints: %v", err.BlueprintsToRollback)
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

		for _, identifier := range err.PagesToRollback {
			rollbackWg.Add(1)
			go func(identifier string) {
				defer rollbackWg.Done()
				if err := page.DeletePage(portClient, identifier); err != nil {
					klog.Warningf("Failed to rollback page %s creation: %v", identifier, err)
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
