// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package component

import (
	"testing"

	"github.com/microsoft/azure-linux-dev-tools/internal/app/azldev/core/testutils"
	"github.com/microsoft/azure-linux-dev-tools/internal/utils/fileperms"
	"github.com/microsoft/azure-linux-dev-tools/internal/utils/fileutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaceRPMsByChannel_EmptyChannelStaysInPlace(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	const (
		rpmsDir = "/output/rpms"
		rpmPath = "/output/rpms/curl-8.0-1.rpm"
	)

	require.NoError(t, fileutils.WriteFile(testEnv.TestFS, rpmPath, []byte("rpm"), fileperms.PrivateFile))

	rpmResults := []RPMResult{
		{Path: rpmPath, PackageName: "curl", Channel: ""},
	}

	require.NoError(t, placeRPMsByChannel(testEnv.Env, rpmResults, rpmsDir))

	// File stays at its original path; RPMResult.Path is unchanged.
	assert.Equal(t, rpmPath, rpmResults[0].Path)
	_, err := testEnv.TestFS.Stat(rpmPath)
	assert.NoError(t, err, "original path should still exist")
}

func TestPlaceRPMsByChannel_NoneChannelStaysInPlace(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	const (
		rpmsDir = "/output/rpms"
		rpmPath = "/output/rpms/debuginfo-8.0-1.rpm"
	)

	require.NoError(t, fileutils.WriteFile(testEnv.TestFS, rpmPath, []byte("rpm"), fileperms.PrivateFile))

	rpmResults := []RPMResult{
		{Path: rpmPath, PackageName: "debuginfo", Channel: "none"},
	}

	require.NoError(t, placeRPMsByChannel(testEnv.Env, rpmResults, rpmsDir))

	assert.Equal(t, rpmPath, rpmResults[0].Path)
	_, err := testEnv.TestFS.Stat(rpmPath)
	assert.NoError(t, err, "original path should still exist")
}

func TestPlaceRPMsByChannel_NamedChannelMovesFile(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	const (
		rpmsDir      = "/output/rpms"
		rpmPath      = "/output/rpms/curl-8.0-1.rpm"
		expectedPath = "/output/rpms/base/curl-8.0-1.rpm"
	)

	require.NoError(t, fileutils.WriteFile(testEnv.TestFS, rpmPath, []byte("rpm"), fileperms.PrivateFile))

	rpmResults := []RPMResult{
		{Path: rpmPath, PackageName: "curl", Channel: "base"},
	}

	require.NoError(t, placeRPMsByChannel(testEnv.Env, rpmResults, rpmsDir))

	// RPMResult.Path must be updated to the new location.
	assert.Equal(t, expectedPath, rpmResults[0].Path)

	// File must exist at the channel subdirectory.
	_, err := testEnv.TestFS.Stat(expectedPath)
	require.NoError(t, err, "file should exist at channel subdirectory")

	// File must no longer exist at the original location.
	_, err = testEnv.TestFS.Stat(rpmPath)
	assert.Error(t, err, "file should have been moved away from the original path")
}

func TestPlaceRPMsByChannel_MultipleRPMsDifferentChannels(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	const rpmsDir = "/output/rpms"

	type rpmInput struct {
		path    string
		channel string
	}

	inputs := []rpmInput{
		{"/output/rpms/curl-8.0-1.rpm", "base"},
		{"/output/rpms/curl-devel-8.0-1.rpm", "devel"},
		{"/output/rpms/curl-debuginfo-8.0-1.rpm", "none"},
		{"/output/rpms/curl-static-8.0-1.rpm", ""},
	}

	rpmResults := make([]RPMResult, 0, len(inputs))

	for _, in := range inputs {
		require.NoError(t, fileutils.WriteFile(testEnv.TestFS, in.path, []byte("rpm"), fileperms.PrivateFile))

		rpmResults = append(rpmResults, RPMResult{
			Path: in.path, PackageName: "curl", Channel: in.channel,
		})
	}

	require.NoError(t, placeRPMsByChannel(testEnv.Env, rpmResults, rpmsDir))

	for _, result := range rpmResults {
		switch result.Channel {
		case "base":
			assert.Equal(t, "/output/rpms/base/curl-8.0-1.rpm", result.Path)
		case "devel":
			assert.Equal(t, "/output/rpms/devel/curl-devel-8.0-1.rpm", result.Path)
		case "none":
			assert.Equal(t, "/output/rpms/curl-debuginfo-8.0-1.rpm", result.Path)
		case "":
			assert.Equal(t, "/output/rpms/curl-static-8.0-1.rpm", result.Path)
		}

		_, statErr := testEnv.TestFS.Stat(result.Path)
		assert.NoError(t, statErr, "RPM should exist at its final resolved path")
	}
}

func TestPlaceRPMsByChannel_MultipleRPMsSameChannel(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	const rpmsDir = "/output/rpms"

	paths := []string{
		"/output/rpms/curl-8.0-1.rpm",
		"/output/rpms/libcurl-8.0-1.rpm",
	}

	rpmResults := make([]RPMResult, 0, len(paths))

	for _, path := range paths {
		require.NoError(t, fileutils.WriteFile(testEnv.TestFS, path, []byte("rpm"), fileperms.PrivateFile))

		rpmResults = append(rpmResults, RPMResult{
			Path: path, PackageName: "curl", Channel: "base",
		})
	}

	require.NoError(t, placeRPMsByChannel(testEnv.Env, rpmResults, rpmsDir))

	assert.Equal(t, "/output/rpms/base/curl-8.0-1.rpm", rpmResults[0].Path)
	assert.Equal(t, "/output/rpms/base/libcurl-8.0-1.rpm", rpmResults[1].Path)

	for _, result := range rpmResults {
		_, err := testEnv.TestFS.Stat(result.Path)
		assert.NoError(t, err, "both RPMs should exist in the shared channel subdirectory")
	}
}

func TestPlaceRPMsByChannel_EmptyInput(t *testing.T) {
	t.Parallel()

	testEnv := testutils.NewTestEnv(t)

	err := placeRPMsByChannel(testEnv.Env, nil, "/output/rpms")
	assert.NoError(t, err)
}
