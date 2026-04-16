// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package projectconfig_test

import (
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/microsoft/azure-linux-dev-tools/internal/projectconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSRPMPublishConfig_Validate(t *testing.T) {
	t.Parallel()

	validCases := []struct {
		name    string
		channel string
	}{
		{name: "empty channel is valid (means inherit)", channel: ""},
		{name: "simple channel name", channel: "sdk-src"},
		{name: "channel with hyphens", channel: "base-src"},
		{name: "reserved none value", channel: "none"},
	}

	for _, testCase := range validCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := projectconfig.SRPMPublishConfig{Channel: testCase.channel}
			assert.NoError(t, validator.New().Struct(&cfg))
		})
	}

	invalidCases := []struct {
		name        string
		channel     string
		errContains string
	}{
		{name: "absolute path", channel: "/etc/passwd", errContains: "excludesall"},
		{name: "traversal with slash", channel: "../secret", errContains: "excludesall"},
		{name: "dot traversal", channel: "..", errContains: "'ne'"},
		{name: "single dot", channel: ".", errContains: "'ne'"},
	}

	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := projectconfig.SRPMPublishConfig{Channel: testCase.channel}
			err := validator.New().Struct(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.errContains)
		})
	}
}

//nolint:dupl // Same merge semantics as PackageConfig.MergeUpdatesFrom; duplication is intentional.
func TestSRPMConfig_MergeUpdatesFrom(t *testing.T) {
	t.Run("non-zero other overrides zero base", func(t *testing.T) {
		base := projectconfig.SRPMConfig{}
		other := projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
		}
		require.NoError(t, base.MergeUpdatesFrom(&other))
		assert.Equal(t, "sdk-src", base.Publish.Channel)
	})

	t.Run("non-zero other overrides non-zero base", func(t *testing.T) {
		base := projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
		}
		other := projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "base-src"},
		}
		require.NoError(t, base.MergeUpdatesFrom(&other))
		assert.Equal(t, "base-src", base.Publish.Channel)
	})

	t.Run("zero other does not override non-zero base", func(t *testing.T) {
		base := projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
		}
		other := projectconfig.SRPMConfig{}
		require.NoError(t, base.MergeUpdatesFrom(&other))
		assert.Equal(t, "sdk-src", base.Publish.Channel)
	})
}

func TestResolveSRPMConfig(t *testing.T) {
	makeProj := func(groups map[string]projectconfig.ComponentGroupConfig) *projectconfig.ProjectConfig {
		proj := projectconfig.NewProjectConfig()
		proj.ComponentGroups = groups

		// Build GroupsByComponent index.
		for groupName, group := range groups {
			for _, member := range group.Components {
				proj.GroupsByComponent[member] = append(proj.GroupsByComponent[member], groupName)
			}
		}

		return &proj
	}

	baseProj := makeProj(map[string]projectconfig.ComponentGroupConfig{
		"base-components": {
			Components: []string{"curl", "wget2"},
			DefaultSRPMConfig: projectconfig.SRPMConfig{
				Publish: projectconfig.SRPMPublishConfig{Channel: "base-src"},
			},
		},
		"sdk-components": {
			Components: []string{"gcc", "binutils"},
			DefaultSRPMConfig: projectconfig.SRPMConfig{
				Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
			},
		},
	})

	testCases := []struct {
		name            string
		compName        string
		expectedChannel string
	}{
		{
			name:            "component not in any group returns zero channel",
			compName:        "python3",
			expectedChannel: "",
		},
		{
			name:            "component in base-components group gets base-src channel",
			compName:        "curl",
			expectedChannel: "base-src",
		},
		{
			name:            "component in sdk-components group gets sdk-src channel",
			compName:        "gcc",
			expectedChannel: "sdk-src",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			comp := &projectconfig.ComponentConfig{
				Name: testCase.compName,
			}

			got, err := projectconfig.ResolveSRPMConfig(comp, baseProj)
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedChannel, got.Publish.Channel)
		})
	}

	t.Run("project default applies when no other config matches", func(t *testing.T) {
		proj := projectconfig.NewProjectConfig()
		proj.DefaultSRPMConfig = projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
		}

		comp := &projectconfig.ComponentConfig{Name: "python3"}

		got, err := projectconfig.ResolveSRPMConfig(comp, &proj)
		require.NoError(t, err)
		assert.Equal(t, "sdk-src", got.Publish.Channel)
	})

	t.Run("component group overrides project default", func(t *testing.T) {
		proj := projectconfig.NewProjectConfig()
		proj.DefaultSRPMConfig = projectconfig.SRPMConfig{
			Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
		}
		proj.ComponentGroups = map[string]projectconfig.ComponentGroupConfig{
			"base-components": {
				Components: []string{"curl"},
				DefaultSRPMConfig: projectconfig.SRPMConfig{
					Publish: projectconfig.SRPMPublishConfig{Channel: "base-src"},
				},
			},
		}
		proj.GroupsByComponent["curl"] = []string{"base-components"}

		comp := &projectconfig.ComponentConfig{Name: "curl"}

		got, err := projectconfig.ResolveSRPMConfig(comp, &proj)
		require.NoError(t, err)
		assert.Equal(t, "base-src", got.Publish.Channel)
	})

	t.Run("multiple groups merged in alphabetical order", func(t *testing.T) {
		// "z-group" sets sdk-src, "a-group" sets base-src — a-group wins (last alphabetically is "z-group").
		// Alphabetical order: a-group first, z-group second — z-group wins.
		proj := projectconfig.NewProjectConfig()
		proj.ComponentGroups = map[string]projectconfig.ComponentGroupConfig{
			"a-group": {
				Components: []string{"shared"},
				DefaultSRPMConfig: projectconfig.SRPMConfig{
					Publish: projectconfig.SRPMPublishConfig{Channel: "base-src"},
				},
			},
			"z-group": {
				Components: []string{"shared"},
				DefaultSRPMConfig: projectconfig.SRPMConfig{
					Publish: projectconfig.SRPMPublishConfig{Channel: "sdk-src"},
				},
			},
		}
		proj.GroupsByComponent["shared"] = []string{"a-group", "z-group"}

		comp := &projectconfig.ComponentConfig{Name: "shared"}

		got, err := projectconfig.ResolveSRPMConfig(comp, &proj)
		require.NoError(t, err)
		// z-group is applied last (alphabetically after a-group), so sdk-src wins.
		assert.Equal(t, "sdk-src", got.Publish.Channel)
	})

	t.Run("empty project config returns zero-value SRPMConfig", func(t *testing.T) {
		proj := projectconfig.NewProjectConfig()
		comp := &projectconfig.ComponentConfig{Name: "curl"}

		got, err := projectconfig.ResolveSRPMConfig(comp, &proj)
		require.NoError(t, err)
		assert.Empty(t, got.Publish.Channel)
	})
}
