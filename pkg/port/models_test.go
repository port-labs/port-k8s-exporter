package port

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_ToIntegrationAppConfig(t *testing.T) {
	exporterConfig := &Config{
		SkipIntegration:                  true,
		Resources:                        []Resource{{Kind: "apps/v1/deployments"}},
		CRDSToDiscover:                   "true",
		OverwriteCRDsActions:             true,
		DeleteDependents:                 true,
		CreateMissingRelatedEntities:     true,
		AllowAllEnvironmentVariablesInJQ: true,
		AllowedEnvironmentVariablesInJQ:  []string{"^PORT_"},
	}

	appConfig := exporterConfig.ToIntegrationAppConfig()

	assert.NotNil(t, appConfig)
	assert.Equal(t, exporterConfig.Resources, appConfig.Resources)
	assert.Equal(t, exporterConfig.CRDSToDiscover, appConfig.CRDSToDiscover)
	assert.Equal(t, exporterConfig.OverwriteCRDsActions, appConfig.OverwriteCRDsActions)
	assert.Equal(t, exporterConfig.DeleteDependents, appConfig.DeleteDependents)
	assert.Equal(t, exporterConfig.CreateMissingRelatedEntities, appConfig.CreateMissingRelatedEntities)
	assert.Equal(t, exporterConfig.AllowAllEnvironmentVariablesInJQ, appConfig.AllowAllEnvironmentVariablesInJQ)
	assert.Equal(t, exporterConfig.AllowedEnvironmentVariablesInJQ, appConfig.AllowedEnvironmentVariablesInJQ)
	assert.NotNil(t, appConfig.SendRawDataExamples)
	assert.True(t, *appConfig.SendRawDataExamples)
}
