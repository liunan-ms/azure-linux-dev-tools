// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

// White-box tests for unexported helpers in this package.
//
//nolint:testpackage // Intentional: tests access unexported packageNameFromRPM and resolveRPMResults.
package component

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/azure-linux-dev-tools/internal/projectconfig"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rpmTestdataPath returns the absolute path to the testdata directory.
func rpmTestdataPath(t *testing.T) string {
	t.Helper()

	// runtime.Caller is not available here so we resolve relative to the test binary's
	// working directory, which Go sets to the package directory.
	abs, err := filepath.Abs("testdata")
	require.NoError(t, err)

	return abs
}

// loadTestRPMIntoMemFS reads the epel-release testdata RPM from the real FS and writes it into
// an in-memory filesystem, returning both the FS and the in-memory path.
func loadTestRPMIntoMemFS(t *testing.T) (afero.Fs, string) {
	t.Helper()

	realPath := filepath.Join(rpmTestdataPath(t), "epel-release-7-5.noarch.rpm")
	data, err := os.ReadFile(realPath)
	require.NoError(t, err, "failed to read testdata RPM %q", realPath)

	memFS := afero.NewMemMapFs()

	const inMemPath = "/rpm/test.rpm"

	require.NoError(t, memFS.MkdirAll("/rpm", 0o755))
	require.NoError(t, afero.WriteFile(memFS, inMemPath, data, 0o644))

	return memFS, inMemPath
}

// TestPackageNameFromRPM_Success verifies that a valid RPM's Name tag is extracted correctly.
func TestPackageNameFromRPM_Success(t *testing.T) {
	memFS, rpmPath := loadTestRPMIntoMemFS(t)

	name, err := packageNameFromRPM(memFS, rpmPath)

	require.NoError(t, err)
	assert.Equal(t, "epel-release", name)
}

// TestPackageNameFromRPM_FileNotFound verifies that a missing RPM returns a clear error.
func TestPackageNameFromRPM_FileNotFound(t *testing.T) {
	memFS := afero.NewMemMapFs() // empty — no files

	_, err := packageNameFromRPM(memFS, "/nonexistent/path/package.rpm")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open RPM")
}

// TestPackageNameFromRPM_CorruptFile verifies that a file with invalid RPM content returns
// a clear error rather than panicking.
func TestPackageNameFromRPM_CorruptFile(t *testing.T) {
	memFS := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(memFS, "/bad.rpm", []byte("this is not an RPM"), 0o644))

	_, err := packageNameFromRPM(memFS, "/bad.rpm")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read RPM headers")
}

// TestResolveRPMResults_NoProjectConfig verifies that channels are left empty when no
// project config is loaded.
func TestResolveRPMResults_NoProjectConfig(t *testing.T) {
	memFS, rpmPath := loadTestRPMIntoMemFS(t)

	results, err := resolveRPMResults(memFS, []string{rpmPath}, nil, &projectconfig.ComponentConfig{})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "epel-release", results[0].PackageName)
	assert.Equal(t, rpmPath, results[0].Path)
	assert.Empty(t, results[0].Channel, "channel should be empty when no project config is present")
}

// TestResolveRPMResults_ProjectDefaultChannel verifies that the project-level default
// package config channel is propagated to the result.
func TestResolveRPMResults_ProjectDefaultChannel(t *testing.T) {
	memFS, rpmPath := loadTestRPMIntoMemFS(t)

	proj := &projectconfig.ProjectConfig{
		DefaultPackageConfig: projectconfig.PackageConfig{
			Publish: projectconfig.PackagePublishConfig{Channel: "stable"},
		},
		PackageGroups: make(map[string]projectconfig.PackageGroupConfig),
	}
	compConfig := &projectconfig.ComponentConfig{}

	results, err := resolveRPMResults(memFS, []string{rpmPath}, proj, compConfig)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "epel-release", results[0].PackageName)
	assert.Equal(t, "stable", results[0].Channel)
}

// TestResolveRPMResults_PerPackageOverride verifies that an explicit per-package entry in
// the component config takes precedence over the project default channel.
func TestResolveRPMResults_PerPackageOverride(t *testing.T) {
	memFS, rpmPath := loadTestRPMIntoMemFS(t)

	proj := &projectconfig.ProjectConfig{
		DefaultPackageConfig: projectconfig.PackageConfig{
			Publish: projectconfig.PackagePublishConfig{Channel: "stable"},
		},
		PackageGroups: make(map[string]projectconfig.PackageGroupConfig),
	}
	compConfig := &projectconfig.ComponentConfig{
		Packages: map[string]projectconfig.PackageConfig{
			"epel-release": {
				Publish: projectconfig.PackagePublishConfig{Channel: "none"},
			},
		},
	}

	results, err := resolveRPMResults(memFS, []string{rpmPath}, proj, compConfig)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "none", results[0].Channel, "per-package override should win over project default")
}

// TestResolveRPMResults_PackageGroupChannel verifies that a matching package-group channel
// overrides the project default but is itself overridden by the component default.
func TestResolveRPMResults_PackageGroupChannel(t *testing.T) {
	memFS, rpmPath := loadTestRPMIntoMemFS(t)

	proj := &projectconfig.ProjectConfig{
		DefaultPackageConfig: projectconfig.PackageConfig{
			Publish: projectconfig.PackagePublishConfig{Channel: "base"},
		},
		PackageGroups: map[string]projectconfig.PackageGroupConfig{
			"epel-group": {
				Packages: []string{"epel-release"},
				DefaultPackageConfig: projectconfig.PackageConfig{
					Publish: projectconfig.PackagePublishConfig{Channel: "extras"},
				},
			},
		},
	}
	compConfig := &projectconfig.ComponentConfig{}

	results, err := resolveRPMResults(memFS, []string{rpmPath}, proj, compConfig)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "extras", results[0].Channel, "package-group channel should override project default")
}

// TestResolveRPMResults_CorruptRPM verifies that an unreadable RPM surfaces an error
// rather than silently producing a result with an empty package name.
func TestResolveRPMResults_CorruptRPM(t *testing.T) {
	memFS := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(memFS, "/bad.rpm", []byte("garbage"), 0o644))

	_, err := resolveRPMResults(memFS, []string{"/bad.rpm"}, nil, &projectconfig.ComponentConfig{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to determine package name")
}
