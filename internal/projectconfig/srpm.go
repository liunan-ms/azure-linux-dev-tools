// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package projectconfig

import (
	"fmt"
	"slices"
	"sort"

	"dario.cat/mergo"
)

// SRPMPublishConfig holds publish settings for a component's source RPM.
// The zero value means the channel is inherited from a higher-priority config layer.
type SRPMPublishConfig struct {
	// Channel identifies the publish channel for this source RPM.
	// The special value "none" is a convention meaning the SRPM should not be published;
	// azldev records this value in build results but enforcement is left to downstream tooling.
	// When empty, the value is inherited from the next layer in the resolution order.
	Channel string `toml:"channel,omitempty" json:"channel,omitempty" validate:"omitempty,ne=.,ne=..,excludesall=/\\" jsonschema:"title=Channel,description=Publish channel for this source RPM; use 'none' to signal to downstream tooling that this SRPM should not be published"`
}

// SRPMConfig holds all configuration applied to a component's source RPM.
// Currently only publish settings are supported; additional fields may be added in the future.
type SRPMConfig struct {
	// Publish holds the publish settings for this source RPM.
	Publish SRPMPublishConfig `toml:"publish,omitempty" json:"publish,omitempty" jsonschema:"title=Publish settings,description=Publishing settings for this source RPM"`
}

// MergeUpdatesFrom updates the SRPM config with non-zero values from other.
func (s *SRPMConfig) MergeUpdatesFrom(other *SRPMConfig) error {
	err := mergo.Merge(s, other, mergo.WithOverride)
	if err != nil {
		return fmt.Errorf("failed to merge SRPM config:\n%w", err)
	}

	return nil
}

// ResolveSRPMConfig returns the effective [SRPMConfig] for the source RPM produced
// by a component, merging contributions from all applicable config layers.
//
// Resolution order (each layer overrides the previous — later wins):
//  1. The project's DefaultSRPMConfig (lowest priority)
//  2. The [ComponentGroupConfig] entries that contain this component, merged in alphabetical order
//  3. The component's own SRPMConfig (highest priority)
func ResolveSRPMConfig(comp *ComponentConfig, proj *ProjectConfig) (SRPMConfig, error) {
	// 1. Start from the project-level default (lowest priority).
	result := proj.DefaultSRPMConfig

	// 2. Apply each component group's default SRPM config in alphabetical order for
	// deterministic layering. A component may belong to multiple groups.
	if groupNames, ok := proj.GroupsByComponent[comp.Name]; ok {
		sortedGroupNames := slices.Clone(groupNames)
		sort.Strings(sortedGroupNames)

		for _, groupName := range sortedGroupNames {
			group, ok := proj.ComponentGroups[groupName]
			if !ok {
				return SRPMConfig{}, fmt.Errorf("component group %#q not found", groupName)
			}

			if err := result.MergeUpdatesFrom(&group.DefaultSRPMConfig); err != nil {
				return SRPMConfig{}, fmt.Errorf(
					"failed to apply defaults from component group %#q to SRPM config for component %#q:\n%w",
					groupName, comp.Name, err,
				)
			}
		}
	}

	// 3. Apply the component's own SRPM config (highest priority).
	if err := result.MergeUpdatesFrom(&comp.SRPMConfig); err != nil {
		return SRPMConfig{}, fmt.Errorf(
			"failed to apply component-level SRPM config for component %#q:\n%w",
			comp.Name, err,
		)
	}

	return result, nil
}
