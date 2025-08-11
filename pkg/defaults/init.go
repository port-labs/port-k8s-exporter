package defaults

import (
	"context"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/port/org_details"
)

func getEventListenerConfig(eventListenerType string) *port.EventListenerSettings {
	if eventListenerType == "KAFKA" {
		return &port.EventListenerSettings{
			Type: eventListenerType,
		}
	}
	return nil
}

func isPortProvisioningSupported(portClient *cli.PortClient) (bool, error) {
	logger.Info("Resources origin is set to be Port, verifying integration is supported")
	featureFlags, err := org_details.GetOrganizationFeatureFlags(portClient)
	if err != nil {
		return false, err
	}

	for _, flag := range featureFlags {
		if flag == port.OrgUseProvisionedDefaultsFeatureFlag {
			return true, nil
		}
	}

	logger.Info("Port origin for Integration is not supported, changing resources origin to use K8S")
	return false, nil
}

func InitIntegration(portClient *cli.PortClient, applicationConfig *port.Config, version string, isTest bool) error {
	logger.Infof("Initializing Port integration")
	defaults, err := getDefaults()
	if err != nil {
		return err
	}

	// Verify Port origin is supported via feature flags
	if applicationConfig.CreatePortResourcesOrigin == port.CreatePortResourcesOriginPort {
		shouldProvisionResourcesUsingPort, err := isPortProvisioningSupported(portClient)
		if err != nil {
			return err
		}
		if !shouldProvisionResourcesUsingPort {
			applicationConfig.CreatePortResourcesOrigin = port.CreatePortResourcesOriginK8S
		}
	}

	existingIntegration, err := integration.GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationAppConfig{
		Resources:                    applicationConfig.Resources,
		CRDSToDiscover:               applicationConfig.CRDSToDiscover,
		OverwriteCRDsActions:         applicationConfig.OverwriteCRDsActions,
		DeleteDependents:             applicationConfig.DeleteDependents,
		CreateMissingRelatedEntities: applicationConfig.CreateMissingRelatedEntities,
	}

	if err != nil {
		if applicationConfig.CreateDefaultResources {
			if applicationConfig.CreatePortResourcesOrigin != port.CreatePortResourcesOriginPort {
				defaultIntegrationConfig = defaults.AppConfig
			}
		}

		logger.Warningf("Could not get integration with state key %s, error: %s", applicationConfig.StateKey, err.Error())
		shouldCreateResourcesUsingPort := applicationConfig.CreatePortResourcesOrigin == port.CreatePortResourcesOriginPort
		existingIntegration, err = integration.CreateIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig, shouldCreateResourcesUsingPort, version)
		if err != nil {
			return err
		}
	} else {
		logger.Infof("Integration with state key %s already exists, patching it", applicationConfig.StateKey)
		integrationPatch := &port.Integration{
			EventListener: getEventListenerConfig(applicationConfig.EventListenerType),
			Version:       version,
		}

		if (existingIntegration.Config == nil && !(applicationConfig.CreatePortResourcesOrigin == port.CreatePortResourcesOriginPort)) || applicationConfig.OverwriteConfigurationOnRestart {
			integrationPatch.Config = defaultIntegrationConfig
		}

		if err := integration.PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch); err != nil {
			return err
		}
	}
	logger.SetHttpWriterParametersAndStart(existingIntegration.LogAttributes.IngestUrl, func() (string, int, error) {
		token, expiresIn, err := portClient.Authenticator.AuthenticateClient(context.Background(), portClient)
		if err != nil {
			return "", 0, err
		}
		return token, expiresIn, nil
	}, logger.LoggerIntegrationData{
		IntegrationVersion:    version,
		IntegrationIdentifier: existingIntegration.Identifier,
	})
	if applicationConfig.CreateDefaultResources && applicationConfig.CreatePortResourcesOrigin != port.CreatePortResourcesOriginPort {
		logger.Infof("Creating default resources (blueprints, pages, etc..)")
		if err := initializeDefaults(portClient, defaults, !isTest); err != nil {
			logger.Warningf("Error initializing defaults: %s", err.Error())
			logger.Warningf("Some default resources may not have been created. The integration will continue running.")
		}
	}
	return nil
}
