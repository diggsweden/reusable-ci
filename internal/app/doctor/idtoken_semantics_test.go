// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// doctor's id-token check exists for one situation: an operator has configured
// sigstore-keyless signing and forgotten to grant the OIDC token, so every
// release will fail at the signing step. The check is the only warning they get
// before that happens.
//
// It used to answer by searching the workflow text for `id-token: write`, which
// is wrong in both directions. The false-OK direction is the one that matters:
// telling an operator their setup is fine because the phrase appears in a
// comment removes the warning entirely, and they find out at release time.
//
// Each case below says which side it is on, because a reader should be able to
// tell at a glance which of these the old check got wrong.

func TestWorkflowGrantsIDToken_ReadsPermissionsNotProse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want bool
		why  string
	}{
		{
			name: "workflow-level grant",
			body: "on: push\npermissions:\n  id-token: write\njobs:\n  a:\n    steps: []\n",
			want: true, why: "the ordinary spelling",
		},
		{
			name: "job-level grant",
			body: "on: push\njobs:\n  release:\n    permissions:\n      contents: read\n      id-token: write\n    steps: []\n",
			want: true, why: "a grant on the job that signs is the more common shape",
		},
		{
			name: "flow-style mapping",
			body: "on: push\njobs:\n  release:\n    permissions: {contents: read, id-token: write}\n    steps: []\n",
			want: true, why: "YAML flow style is the same document; a line-oriented search would miss it",
		},
		{
			name: "extra spacing",
			body: "on: push\npermissions:\n  id-token:    write\n",
			want: true, why: "FALSE NEGATIVE under the old check: the substring `id-token: write` never appears",
		},
		{
			name: "write-all shorthand",
			body: "on: push\npermissions: write-all\n",
			want: true, why: "FALSE NEGATIVE under the old check: write-all grants id-token without naming it",
		},
		{
			name: "quoted value",
			body: "on: push\npermissions:\n  id-token: \"write\"\n",
			want: true, why: "quoting is a YAML detail, not a different permission",
		},

		{
			name: "commented-out grant",
			body: "on: push\npermissions:\n  contents: read\n  # id-token: write\njobs:\n  a:\n    steps: []\n",
			want: false, why: "FALSE OK under the old check, and the worst case: the operator is told they are done",
		},
		{
			name: "the phrase inside a description",
			body: "on:\n  workflow_call:\n    inputs:\n      note:\n        description: \"set id-token: write in your caller\"\n        type: string\n",
			want: false, why: "FALSE OK under the old check: documentation is not a grant",
		},
		{
			name: "the phrase in a run block",
			body: "on: push\njobs:\n  a:\n    steps:\n      - run: \"echo 'remember id-token: write'\"\n",
			want: false, why: "FALSE OK under the old check",
		},
		{
			name: "read-all shorthand",
			body: "on: push\npermissions: read-all\n",
			want: false, why: "read-all grants nothing writable",
		},
		{
			name: "id-token read",
			body: "on: push\npermissions:\n  id-token: read\n",
			want: false, why: "read is not the level Sigstore needs; there is no read level for id-token, and none is a grant",
		},
		{
			name: "id-token none",
			body: "on: push\npermissions:\n  id-token: none\n",
			want: false, why: "an explicit revocation",
		},
		{
			name: "other scopes only",
			body: "on: push\npermissions:\n  contents: write\n  packages: write\n",
			want: false, why: "writing elsewhere is not minting a token",
		},
		{
			name: "no permissions at all",
			body: "on: push\njobs:\n  a:\n    steps: []\n",
			want: false, why: "the situation the check exists for",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := workflowGrantsIDToken(parseWorkflow(t, tc.body)); got != tc.want {
				t.Errorf("granted=%v, want %v: %s", got, tc.want, tc.why)
			}
		})
	}
}

func TestWorkflowEnablesOpenIDConnect_ReadsKeysNotProse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want bool
		why  string
	}{
		{
			name: "set at the job level",
			body: "on: push\njobs:\n  release:\n    enable-openid-connect: true\n    steps: []\n",
			want: true, why: "Forgejo ignores `permissions` and uses this instead",
		},
		{
			name: "set at the workflow level",
			body: "on: push\nenable-openid-connect: true\njobs:\n  a:\n    steps: []\n",
			want: true, why: "the docs do not fix the level, so both are accepted",
		},
		{
			name: "explicitly disabled",
			body: "on: push\njobs:\n  release:\n    enable-openid-connect: false\n    steps: []\n",
			want: false, why: "an operator who turned it off has not granted anything",
		},
		{
			name: "mentioned in a comment",
			body: "on: push\n# enable-openid-connect: true\njobs:\n  a:\n    steps: []\n",
			want: false, why: "FALSE OK under the old check",
		},
		{
			name: "mentioned in a run block",
			body: "on: push\njobs:\n  a:\n    steps:\n      - run: echo enable-openid-connect\n",
			want: false, why: "FALSE OK under the old check",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := workflowEnablesOpenIDConnect(parseWorkflow(t, tc.body)); got != tc.want {
				t.Errorf("enabled=%v, want %v: %s", got, tc.want, tc.why)
			}
		})
	}
}

// The examples this repository ships to adopters must satisfy the check that
// tells adopters they got it right. If they did not, the check and the
// documentation would be giving opposite advice.
func TestShippedKeylessExampleGrantsIDToken(t *testing.T) {
	t.Parallel()

	found, err := anyWorkflowSatisfies("../../../examples/signing/sigstore-keyless", workflowGrantsIDToken)
	if err != nil {
		t.Fatalf("scan the shipped example: %v", err)
	}

	if !found {
		t.Error("the sigstore-keyless example does not grant id-token, but doctor's remediation points operators at it")
	}
}

func parseWorkflow(t *testing.T, body string) *yaml.Node {
	t.Helper()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("parse fixture: %v\n%s", err, body)
	}

	if len(doc.Content) == 0 {
		t.Fatalf("empty fixture:\n%s", body)
	}

	return doc.Content[0]
}
