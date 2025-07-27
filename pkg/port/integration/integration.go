package integration

import (
	"fmt"
	"strings"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

const (
	integrationPollingInitialSeconds = 3
	integrationPollingRetryLimit     = 30
	integrationPollingBackoffFactor  = 1.15
	createResourcesParamName         = "integration_modes"
)

var createResourcesParamValue = []string{"create_resources"}

type DefaultsProvisionFailedError struct {
	RetryLimit int
}

func (e *DefaultsProvisionFailedError) Error() string {
	return fmt.Sprintf("integration config was not provisioned after %d attempts", e.RetryLimit)
}

func CreateIntegration(portClient *cli.PortClient, stateKey string, eventListenerType string, appConfig *port.IntegrationAppConfig, createPortResourcesOriginInPort bool, version string) (*port.Integration, error) {
	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      stateKey,
		EventListener: &port.EventListenerSettings{
			Type: eventListenerType,
		},
		Config:  appConfig,
		Version: version,
	}
	queryParams := map[string]string{}
	if createPortResourcesOriginInPort {
		queryParams = map[string]string{
			createResourcesParamName: strings.Join(createResourcesParamValue, ","),
		}
	}

	createdIntegration, err := portClient.CreateIntegration(integration, queryParams)
	if err != nil {
		return nil, fmt.Errorf("error creating Port integration: %v", err)
	}

	if createPortResourcesOriginInPort {
		return PollIntegrationUntilDefaultProvisioningComplete(portClient, stateKey)
	}

	return createdIntegration, nil
}

func GetIntegration(portClient *cli.PortClient, stateKey string) (*port.Integration, error) {
	apiIntegration, err := portClient.GetIntegration(stateKey)
	if err != nil {
		return nil, fmt.Errorf("error getting Port integration: %v", err)
	}

	if apiIntegration.Config != nil {
		defaultTrue := true
		if apiIntegration.Config.SendRawDataExamples == nil {
			apiIntegration.Config.SendRawDataExamples = &defaultTrue
		}
	}

	return apiIntegration, nil
}

func DeleteIntegration(portClient *cli.PortClient, stateKey string) error {
	err := portClient.DeleteIntegration(stateKey)
	if err != nil {
		return fmt.Errorf("error deleting Port integration: %v", err)
	}
	return nil
}

func PatchIntegration(portClient *cli.PortClient, stateKey string, integration *port.Integration) error {
	err := portClient.PatchIntegration(stateKey, integration)
	if err != nil {
		return fmt.Errorf("error updating Port integration: %v", err)
	}
	return nil
}

func PostIntegrationKindExample(portClient *cli.PortClient, stateKey string, kind string, examples []interface{}) error {
	err := portClient.PostIntegrationKindExample(stateKey, kind, examples)
	if err != nil {
		return err
	}
	return nil
}

func PollIntegrationUntilDefaultProvisioningComplete(portClient *cli.PortClient, stateKey string) (*port.Integration, error) {
	attempts := 0
	currentIntervalSeconds := integrationPollingInitialSeconds

	for attempts < integrationPollingRetryLimit {
		fmt.Printf("Fetching created integration and validating config, attempt %d/%d\n", attempts+1, integrationPollingRetryLimit)

		integration, err := GetIntegration(portClient, stateKey)
		if err != nil {
			return nil, fmt.Errorf("error getting integration during polling: %v", err)
		}

		if integration.Config.Resources != nil {
			return integration, nil
		}

		fmt.Printf("Integration config is still being provisioned, retrying in %d seconds\n", currentIntervalSeconds)
		time.Sleep(time.Duration(currentIntervalSeconds) * time.Second)

		attempts++
		currentIntervalSeconds = int(float64(currentIntervalSeconds) * integrationPollingBackoffFactor)
	}

	return nil, &DefaultsProvisionFailedError{RetryLimit: integrationPollingRetryLimit}
}
