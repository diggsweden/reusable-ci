// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

// A release run can be retried — a transient push failure, an operator rerunning
// a workflow — and the version bump runs again with the same plan and the same
// version. Every version file simply gets the value it already has, which is
// idempotent by construction.
//
// Android's versionCode is not. It INCREMENTS, so a naive second run would
// ship 12 where the release recorded 11, and nothing downstream would notice:
// both are valid version codes and the build succeeds. Play Store rejects a
// duplicate versionCode, so the failure surfaces at the last possible moment,
// on the publish, after everything else has been signed.
//
// bump.go guards it — "Setting the requested version again is a retry, not a
// new Android release" — and that guard is one condition away from silently
// disappearing. Nothing asserted it until now.
func TestBumpPlan_RerunningTheSamePlanDoesNotIncrementTheAndroidVersionCode(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	const versionFile = "gradle.properties"

	writeFile(t, root, versionFile, "version=1.0.0\nversionName=1.0.0\nversionCode=11\nKEEP=yes\n")
	writeFile(t, root, "full.md", "# changelog\n")

	in := aliasPlanInput(t, []pipeline.PlannedArtifact{{
		Name:          "android",
		ProjectType:   projecttype.GradleAndroid,
		Gradle:        &config.GradleConfig{GradleVersionFile: versionFile},
		GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: versionFile},
	}}, "")

	run := func() string {
		t.Helper()

		var out bytes.Buffer

		require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{},
			fakeoutputsink.New(t), &out, &out, output.Annotator{}, in))

		return readFileAt(t, root, versionFile)
	}

	first := run()
	require.Contains(t, first, "versionCode=12", "the first bump must increment the version code")
	require.Contains(t, first, "versionName=2.0.0")
	require.Contains(t, first, "KEEP=yes", "unrelated properties must survive the bump")

	second := run()
	require.Equal(t, first, second,
		"rerunning the same plan changed the file. The version code advanced on a retry, so the artifact "+
			"carries a different code than the release recorded, and Play rejects the duplicate at publish time.")

	third := run()
	require.Equal(t, first, third, "a third run diverged; convergence must not depend on the attempt count")
}

// The control: a genuinely NEW version must still increment. Without it, the
// convergence above is satisfied by a bump that stopped incrementing at all,
// which would ship every Android release under one version code.
func TestBumpPlan_ANewVersionStillIncrementsTheAndroidVersionCode(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	const versionFile = "gradle.properties"

	writeFile(t, root, versionFile, "version=1.0.0\nversionName=1.0.0\nversionCode=11\n")
	writeFile(t, root, "full.md", "# changelog\n")

	artifacts := []pipeline.PlannedArtifact{{
		Name:          "android",
		ProjectType:   projecttype.GradleAndroid,
		Gradle:        &config.GradleConfig{GradleVersionFile: versionFile},
		GradleAndroid: &config.GradleAndroidConfig{GradleVersionFile: versionFile},
	}}

	var out bytes.Buffer

	first := aliasPlanInput(t, artifacts, "")
	require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{},
		fakeoutputsink.New(t), &out, &out, output.Annotator{}, first))
	require.Contains(t, readFileAt(t, root, versionFile), "versionCode=12")

	next := aliasPlanInput(t, artifacts, "")
	next.Version = "v3.0.0"

	require.NoError(t, appversion.BumpPlan(t.Context(), appversion.BumpOps{},
		fakeoutputsink.New(t), &out, &out, output.Annotator{}, next))

	body := readFileAt(t, root, versionFile)
	require.Contains(t, body, "versionCode=13", "a new version must advance the code")
	require.Contains(t, body, "versionName=3.0.0")
}

// readFileAt reads a file inside the owned temporary tree.
func readFileAt(t *testing.T, dir, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(dir, rel)) //nolint:gosec // owned temporary tree.
	require.NoError(t, err)

	return string(body)
}
