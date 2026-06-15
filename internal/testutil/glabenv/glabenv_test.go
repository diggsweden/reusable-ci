// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package glabenv_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/testutil/glabenv"
)

func TestSetup_ExportsCIVars(t *testing.T) {
	e := glabenv.Setup(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for _, k := range []string{
		"GITLAB_CI", "CI_OUTPUT", "CI_COMMIT_SHA", "CI_COMMIT_REF_NAME",
		"CI_PROJECT_PATH", "CI_PROJECT_URL", "CI_SERVER_URL",
	} {
		if got := os.Getenv(k); got == "" {
			t.Errorf("%s not set", k)
		}
	}

	if got := os.Getenv("CI_OUTPUT"); got != e.OutputPath {
		t.Errorf("CI_OUTPUT = %q, want %q", got, e.OutputPath)
	}

	e.Setenv("CUSTOM_GITLAB_ENV", "set")

	if got := os.Getenv("CUSTOM_GITLAB_ENV"); got != "set" {
		t.Errorf("CUSTOM_GITLAB_ENV = %q, want set", got)
	}
}

func TestOutput_ReadsScalar(t *testing.T) {
	e := glabenv.Setup(t)                                                                     //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	require.NoError(t, os.WriteFile(e.OutputPath, []byte("KEY=value\nOTHER=stuff\n"), 0o644)) //nolint:gosec // test fixture

	if got := e.Output("KEY"); got != "value" {
		t.Errorf("Output(KEY) = %q, want %q", got, "value")
	}

	if got := e.Output("OTHER"); got != "stuff" {
		t.Errorf("Output(OTHER) = %q, want %q", got, "stuff")
	}

	if got := e.Output("MISSING"); got != "" {
		t.Errorf("Output(MISSING) = %q, want empty", got)
	}
}

func TestSetTagRef(t *testing.T) {
	e := glabenv.Setup(t)
	e.SetTagRef("v1.2.3")

	if got := os.Getenv("CI_COMMIT_TAG"); got != "v1.2.3" {
		t.Errorf("CI_COMMIT_TAG = %q, want %q", got, "v1.2.3")
	}

	if got := os.Getenv("CI_COMMIT_REF_NAME"); got != "v1.2.3" {
		t.Errorf("CI_COMMIT_REF_NAME = %q, want %q", got, "v1.2.3")
	}

	if got := os.Getenv("CI_COMMIT_BRANCH"); got != "" {
		t.Errorf("CI_COMMIT_BRANCH = %q, want empty (tag push)", got)
	}
}

func TestSetMergeRequest(t *testing.T) {
	e := glabenv.Setup(t)
	e.SetMergeRequest("42", "feat/x", "main")

	if got := os.Getenv("CI_MERGE_REQUEST_IID"); got != "42" {
		t.Errorf("CI_MERGE_REQUEST_IID = %q, want %q", got, "42")
	}

	if got := os.Getenv("CI_PIPELINE_SOURCE"); got != "merge_request_event" {
		t.Errorf("CI_PIPELINE_SOURCE = %q, want %q", got, "merge_request_event")
	}

	if got := os.Getenv("CI_COMMIT_REF_NAME"); got != "feat/x" {
		t.Errorf("CI_COMMIT_REF_NAME = %q, want %q", got, "feat/x")
	}
}
