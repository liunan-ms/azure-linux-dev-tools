// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package component_test

import (
	"testing"

	"github.com/microsoft/azure-linux-dev-tools/internal/app/azldev/cmds/component"
	"github.com/microsoft/azure-linux-dev-tools/internal/app/azldev/core/components"
	"github.com/microsoft/azure-linux-dev-tools/internal/app/azldev/core/testutils"
	"github.com/microsoft/azure-linux-dev-tools/internal/projectconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComponentListCommand(t *testing.T) {
	cmd := component.NewComponentListCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
}

func TestComponentListCmd_NoMatch(t *testing.T) {
	const testComponentName = "test-component"

	testEnv := testutils.NewTestEnv(t)

	cmd := component.NewComponentListCommand()
	cmd.SetArgs([]string{testComponentName})

	err := cmd.ExecuteContext(testEnv.Env)

	// We expect an error because we haven't set up any components.
	require.Error(t, err)
}

func TestListComponents_OneComponent(t *testing.T) {
	const testComponentName = "test-component"

	testEnv := testutils.NewTestEnv(t)
	testEnv.Config.Components[testComponentName] = projectconfig.ComponentConfig{
		Name: testComponentName,
		Spec: projectconfig.SpecSource{Path: "/path/to/spec"},
	}

	options := component.ListComponentOptions{
		ComponentFilter: components.ComponentFilter{
			ComponentNamePatterns: []string{testComponentName},
		},
	}

	results, err := component.ListComponentConfigs(testEnv.Env, &options)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, testComponentName, results[0].Name)
	assert.Empty(t, results[0].SRPMPublishChannel, "SRPMPublishChannel should be empty when no channel is configured")
}

func TestListComponents_SRPMPublishChannel(t *testing.T) {
	const testComponentName = "curl"

	testEnv := testutils.NewTestEnv(t)
	testEnv.Config.Components[testComponentName] = projectconfig.ComponentConfig{
		Name: testComponentName,
	}
	testEnv.Config.DefaultSRPMConfig = projectconfig.SRPMConfig{
		Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
	}

	options := component.ListComponentOptions{
		ComponentFilter: components.ComponentFilter{
			ComponentNamePatterns: []string{testComponentName},
		},
	}

	results, err := component.ListComponentConfigs(testEnv.Env, &options)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, testComponentName, results[0].Name)
	assert.Equal(t, "sdk-src", results[0].SRPMPublishChannel)
}

func TestListComponents_MultipleComponents(t *testing.T) {
	testEnv := testutils.NewTestEnv(t)

	testEnv.Config.Components["curl"] = projectconfig.ComponentConfig{
		Name: "curl",
		Spec: projectconfig.SpecSource{Path: "/specs/curl.spec"},
	}
	testEnv.Config.Components["vim"] = projectconfig.ComponentConfig{
		Name: "vim",
		Spec: projectconfig.SpecSource{Path: "/specs/vim.spec"},
	}

	options := component.ListComponentOptions{
		ComponentFilter: components.ComponentFilter{
			IncludeAllComponents: true,
		},
	}

	results, err := component.ListComponentConfigs(testEnv.Env, &options)
	require.NoError(t, err)
	require.Len(t, results, 2)

	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Name)
	}

	assert.ElementsMatch(t, []string{"curl", "vim"}, names)
}
