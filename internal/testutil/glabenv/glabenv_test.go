// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package glabenv_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/glabenv"
)

func TestSetup_ExportsCIVars(t *testing.T) {
	e := glabenv.Setup(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	// GITLAB_CI is the marker forge detection reads, and it is read for its
	// VALUE, not its presence. Checking only that it is non-empty accepts
	// "false" — which is the one value that makes every consumer conclude it
	// is NOT running on GitLab. A fixture that sets it that way would leave
	// each test exercising the wrong forge branch while still passing here.
	if got := os.Getenv("GITLAB_CI"); got != "true" {
		t.Errorf("GITLAB_CI = %q, want %q", got, "true")
	}

	for _, k := range []string{
		"CI_OUTPUT", "CI_COMMIT_SHA", "CI_COMMIT_REF_NAME",
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

func TestSetTagRef_SetsTagVarsAndClearsBranch(t *testing.T) {
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

func TestSetMergeRequest_SetsMergeRequestVars(t *testing.T) {
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

func TestEventSetters_CoherentTransitions(t *testing.T) {
	for _, initial := range []string{"fresh", "tag", "merge-request"} {
		t.Run(initial, func(t *testing.T) {
			env := glabenv.Setup(t)
			if initial == "tag" {
				env.SetTagRef("v0.1.0")
			}

			if initial == "merge-request" {
				env.SetMergeRequest("7", "old-source", "old-target")
			}

			// Every case ends MR -> tag -> MR, including a fresh branch -> MR.
			for _, event := range []string{"merge-request", "tag", "merge-request"} {
				want := map[string]string{
					"CI_COMMIT_TAG":                       "",
					"CI_COMMIT_BRANCH":                    "",
					"CI_COMMIT_REF_NAME":                  "feat/x",
					"CI_PIPELINE_SOURCE":                  "merge_request_event",
					"CI_MERGE_REQUEST_IID":                "42",
					"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feat/x",
					"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "release",
				}

				if event == "tag" {
					env.SetTagRef("v1.2.3")

					want["CI_COMMIT_TAG"] = "v1.2.3"
					want["CI_COMMIT_REF_NAME"] = "v1.2.3"
					want["CI_PIPELINE_SOURCE"] = "push"
					want["CI_MERGE_REQUEST_IID"] = ""
					want["CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"] = ""
					want["CI_MERGE_REQUEST_TARGET_BRANCH_NAME"] = ""
				} else {
					env.SetMergeRequest("42", "feat/x", "release")
				}

				for key, value := range want {
					require.Equal(t, value, os.Getenv(key), "%s: %s", event, key)
				}

				require.Equal(t, "example/project", os.Getenv("CI_PROJECT_PATH"))
				require.Equal(t, "abcdef0123456789abcdef0123456789abcdef01", os.Getenv("CI_COMMIT_SHA"))
			}
		})
	}
}

// TestSetup_DoesNotAlsoClaimAnotherForge keeps the two runner fixtures
// mutually exclusive.
//
// Forge detection asks each marker in turn, so a fixture that leaves a second
// forge's marker set makes the answer depend on the order the detector happens
// to check in. That is not hypothetical here: the two fixtures are used in the
// same packages, and an inherited GITHUB_ACTIONS from the developer's own shell
// or from a previous helper is exactly the kind of ambient state these
// fixtures exist to control.
func TestSetup_DoesNotAlsoClaimAnotherForge(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	glabenv.Setup(t)

	if got := os.Getenv("GITHUB_ACTIONS"); got == "true" {
		t.Errorf("the GitLab fixture left GITHUB_ACTIONS=%q set; detection depends on which marker is read first", got)
	}
}

// TestOutput_ReaderBoundaries gives the dotenv reader the same treatment its
// GitHub sibling already had.
//
// Only the simple "one key, one value" case was covered here, so the two
// readers had drifted apart without anything noticing: this one returned the
// FIRST declaration of a key and the GitHub one returns the latest. A dotenv
// file is consumed the way a shell sources it, so the latest is what the
// pipeline actually gets — meaning a test whose code emitted a provisional
// value and then corrected it read the provisional one and passed.
func TestOutput_ReaderBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, data, key, want, why string
	}{
		{
			name: "the latest declaration wins",
			data: "KEY=first\nOTHER=x\nKEY=last\n",
			key:  "KEY", want: "last",
			why: "a corrected output must not read as its provisional value",
		},
		{
			name: "a value containing = is kept whole",
			data: "KEY=a=b=c\n",
			key:  "KEY", want: "a=b=c",
			why: "only the first separator delimits the key",
		},
		{name: "an empty value", data: "KEY=\n", key: "KEY", want: ""},
		{
			name: "a key that is a prefix of another does not match it",
			data: "KEYS=plural\n",
			key:  "KEY", want: "",
			why: "matching on the name alone would return a neighbour's value",
		},
		{
			name: "the longer key is not matched by the shorter one's line",
			data: "KEY=short\nKEYS=plural\n",
			key:  "KEYS", want: "plural",
		},
		{name: "an absent key", data: "OTHER=x\n", key: "KEY", want: ""},
		{name: "no trailing newline", data: "KEY=value", key: "KEY", want: "value"},
		{
			name: "a line without a separator is not a declaration",
			data: "KEY\nKEY=value\n",
			key:  "KEY", want: "value",
		},
		{name: "an empty file", data: "", key: "KEY", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := glabenv.Setup(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			require.NoError(t, os.WriteFile(e.OutputPath, []byte(tc.data), 0o600))
			require.Equal(t, tc.want, e.Output(tc.key), tc.why)
		})
	}
}
