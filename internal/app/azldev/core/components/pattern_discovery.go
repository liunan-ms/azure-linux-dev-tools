// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package components

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/microsoft/azure-linux-dev-tools/internal/app/azldev"
	"github.com/microsoft/azure-linux-dev-tools/internal/projectconfig"
	"github.com/microsoft/azure-linux-dev-tools/internal/utils/fileutils"
)

// patternDiscoveredComponent captures the result of expanding a single
// project-level [projectconfig.ComponentConfig.OverlayFiles] entry that
// contains the `{component}` placeholder.
type patternDiscoveredComponent struct {
	// Name of the discovered component (the captured {component} segment).
	Name string
	// ReferenceDir is the absolute directory the pattern captured for this
	// component (i.e. the prefix directory + captured name). Used as the
	// component's reference directory for downstream operations.
	ReferenceDir string
	// OverlayFiles is the deduplicated, sorted list of absolute overlay file
	// paths matched by the winning pattern for this component.
	OverlayFiles []string
	// SourcePattern is the exact entry string that produced this match, used
	// in shadow-lint diagnostics.
	SourcePattern string
}

// discoverComponentsFromPatterns walks the project-level
// default-component-config, expands every 'overlay-files' entry that contains
// the `{component}` placeholder, and returns the resulting pattern-discovered
// components. Two entries at project scope producing the same component name
// is a hard error.
//
// The caller is responsible for further precedence against explicitly-declared
// components (that override happens at the resolver layer; see
// [Resolver.patternDiscoveredComponents]).
func discoverComponentsFromPatterns(env *azldev.Env) (map[string]*patternDiscoveredComponent, error) {
	result := make(map[string]*patternDiscoveredComponent)

	config := env.Config()
	if config == nil {
		return result, nil
	}

	var patterns []string

	for _, entry := range config.DefaultComponentConfig.OverlayFiles {
		if strings.Contains(entry, projectconfig.OverlayFilesComponentPlaceholder) {
			patterns = append(patterns, entry)
		}
	}

	if len(patterns) == 0 {
		return result, nil
	}

	for _, pattern := range patterns {
		byName, err := expandSinglePattern(env, pattern)
		if err != nil {
			return nil, fmt.Errorf("project 'overlay-files': %w", err)
		}

		for name, match := range byName {
			if existing, ok := result[name]; ok {
				return nil, fmt.Errorf(
					"%w: component %#q is discovered by both %#q and %#q; "+
						"restrict one of the patterns or rename one of the directories",
					ErrPatternDiscoveryCollision, name, existing.SourcePattern, match.SourcePattern,
				)
			}

			result[name] = match
		}
	}

	return result, nil
}

// expandSinglePattern expands a single validated pattern and groups matched
// files by captured component name.
func expandSinglePattern(env *azldev.Env, pattern string) (map[string]*patternDiscoveredComponent, error) {
	prefix, suffix := projectconfig.SplitOverlayFilesPlaceholder(pattern)

	// Substitute a single-segment wildcard for {component} to build the actual
	// glob. Because {component} is validated as a whole path segment (see
	// ConfigFile.Validate), replacing it with "*" produces a well-formed
	// doublestar pattern that captures exactly one path segment.
	globPattern := prefix + "*" + suffix

	// Ignore IO errors here, mirroring findComponentGroupSpecPaths: we may
	// legitimately hit unreadable subtrees under the project root.
	matches, err := fileutils.Glob(env.FS(), globPattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil, fmt.Errorf("failed to expand pattern %#q:\n%w", pattern, err)
	}

	byName := make(map[string]*patternDiscoveredComponent)
	// Deduplicate files per component before appending to preserve stable ordering.
	seen := make(map[string]map[string]bool)

	for _, match := range matches {
		name, referenceDir, err := extractCapturedName(pattern, prefix, match)
		if err != nil {
			// Skip matches we can't parse (should not happen with a validated
			// pattern, but guard anyway).
			slog.Debug("pattern-discovery: skipping unparseable match",
				"pattern", pattern, "match", match, "err", err)

			continue
		}

		bucket, ok := byName[name]
		if !ok {
			bucket = &patternDiscoveredComponent{
				Name:          name,
				ReferenceDir:  referenceDir,
				SourcePattern: pattern,
			}
			byName[name] = bucket
			seen[name] = make(map[string]bool)
		}

		if !seen[name][match] {
			seen[name][match] = true

			bucket.OverlayFiles = append(bucket.OverlayFiles, match)
		}
	}

	// Sort the overlay-files list per component (filename first, then full path).
	for _, bucket := range byName {
		slices.SortFunc(bucket.OverlayFiles, func(left, right string) int {
			if result := strings.Compare(filepath.Base(left), filepath.Base(right)); result != 0 {
				return result
			}

			return strings.Compare(left, right)
		})
	}

	return byName, nil
}

// extractCapturedName strips prefix from match and returns the first path
// segment (the captured component name) together with the concrete directory
// that captured segment lives in. The returned ReferenceDir is the natural
// per-component anchor for downstream operations.
func extractCapturedName(pattern, prefix, match string) (name, referenceDir string, err error) {
	rel := match
	if prefix != "" {
		if !strings.HasPrefix(match, prefix) {
			return "", "", fmt.Errorf(
				"match %#q does not start with expected prefix %#q for pattern %#q",
				match, prefix, pattern,
			)
		}

		rel = match[len(prefix):]
	}

	// The captured segment is everything up to (but not including) the first
	// path separator in the residual, or the entire residual if there is no
	// suffix. Patterns are validated to use '/' only.
	sep := strings.IndexByte(rel, '/')
	if sep < 0 {
		name = rel
	} else {
		name = rel[:sep]
	}

	if name == "" {
		return "", "", fmt.Errorf(
			"empty captured component name for match %#q against pattern %#q",
			match, pattern,
		)
	}

	// The reference dir is the prefix + captured segment (no trailing separator).
	referenceDir = filepath.Clean(prefix + name)

	return name, referenceDir, nil
}

// logShadowedByDeclaration emits a warning when an explicit component
// declaration overrides pattern-discovered overlay files for the same name.
func logShadowedByDeclaration(shadowed *patternDiscoveredComponent) {
	slog.Warn(
		"project 'overlay-files' pattern match shadowed by explicit component declaration; overlay files will not be applied",
		"component", shadowed.Name,
		"shadowed_pattern", shadowed.SourcePattern,
		"shadowed_files", shadowed.OverlayFiles,
	)
}

// discoveredComponentsCache holds a lazy result of
// [discoverComponentsFromPatterns] so the resolver can call it repeatedly
// without re-globbing the filesystem.
type discoveredComponentsCache struct {
	once    sync.Once
	entries map[string]*patternDiscoveredComponent
	err     error
}

func (c *discoveredComponentsCache) load(env *azldev.Env) (map[string]*patternDiscoveredComponent, error) {
	c.once.Do(func() {
		c.entries, c.err = discoverComponentsFromPatterns(env)
	})

	return c.entries, c.err
}

// ErrPatternDiscoveryCollision wraps project-scope pattern collision errors so
// tests can assert on them.
var ErrPatternDiscoveryCollision = errors.New("project 'overlay-files' pattern collision")
