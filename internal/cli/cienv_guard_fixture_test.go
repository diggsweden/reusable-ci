// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

func TestRunContextFlagNameCoverage(t *testing.T) {
	t.Parallel()

	for _, concept := range runcontext.All() {
		for _, key := range concept.Keys() {
			t.Run(concept.Concept+"/"+key, func(t *testing.T) {
				root := &urfavecli.Command{Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "value", Sources: urfavecli.EnvVars(key)},
				}}
				require.Equal(t, []string{fmt.Sprintf("reusable-ci --value reads $%s via an inline chain", key)},
					runContextFlagViolations(t, root, nil))
				root.Flags = []urfavecli.Flag{&urfavecli.StringFlag{Name: "value", Sources: cienv.Sources(concept)}}
				require.Empty(t, runContextFlagViolations(t, root, nil))
			})
		}
	}
}

func TestRunContextFlagExemptionFixtures(t *testing.T) {
	t.Parallel()

	const path = "reusable-ci release sign"

	key := runContextFlagKey{path, "expected-tag", "RELEASE_TAG"}

	unused := "unused run-context exemption: " + path + " --expected-tag $RELEASE_TAG"
	for _, tc := range []struct {
		name, command, flag string
		sources             urfavecli.ValueSourceChain
		exemptions          map[runContextFlagKey]string
		want                []string
	}{
		{name: "exact exception", command: "sign", flag: "expected-tag", sources: urfavecli.EnvVars("RELEASE_TAG"), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}},
		{name: "another command", command: "verify", flag: "expected-tag", sources: urfavecli.EnvVars("RELEASE_TAG"), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{"reusable-ci release verify --expected-tag reads $RELEASE_TAG via an inline chain", unused}},
		{name: "another flag", command: "sign", flag: "tag", sources: urfavecli.EnvVars("RELEASE_TAG"), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{path + " --tag reads $RELEASE_TAG via an inline chain", unused}},
		{name: "another tag key", command: "sign", flag: "expected-tag", sources: urfavecli.EnvVars("TAG_NAME"), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{path + " --expected-tag reads $TAG_NAME via an inline chain", unused}},
		{name: "added ref fallback", command: "sign", flag: "expected-tag", sources: urfavecli.EnvVars("RELEASE_TAG", "GITHUB_REF_NAME"), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{path + " --expected-tag reads $GITHUB_REF_NAME via an inline chain"}},
		{name: "removed source", command: "sign", flag: "expected-tag", exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{unused}},
		{name: "migrated to cienv", command: "sign", flag: "expected-tag", sources: cienv.Tag(), exemptions: map[runContextFlagKey]string{key: "explicit expected tag"}, want: []string{unused}},
		{name: "unjustified exemption", command: "sign", flag: "expected-tag", sources: urfavecli.EnvVars("RELEASE_TAG"), exemptions: map[runContextFlagKey]string{key: " \t"}, want: []string{path + " --expected-tag reads $RELEASE_TAG via an inline chain", unused}},
		{name: "non-owned", command: "sign", flag: "value", sources: urfavecli.EnvVars("DOCKER_CONFIG", "SOURCE_DATE_EPOCH")},
		{name: "tag family not exempt", command: "sign", flag: "tag", sources: urfavecli.EnvVars("RELEASE_TAG", "TAG_NAME"), want: []string{path + " --tag reads $RELEASE_TAG via an inline chain", path + " --tag reads $TAG_NAME via an inline chain"}},
		{name: "mixed cienv and inline", command: "sign", flag: "value", sources: urfavecli.NewValueSourceChain(append(cienv.Repository().Chain, urfavecli.EnvVars("GITHUB_ACTOR").Chain...)...), want: []string{path + " --value reads $GITHUB_ACTOR via an inline chain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &urfavecli.Command{Commands: []*urfavecli.Command{{Name: "release", Commands: []*urfavecli.Command{{
				Name: tc.command,
				Flags: []urfavecli.Flag{&urfavecli.StringFlag{
					Name: tc.flag, Aliases: []string{"alias"}, Sources: tc.sources,
				}},
			}}}}}
			require.Equal(t, tc.want, runContextFlagViolations(t, root, tc.exemptions))
		})
	}
}

func TestRunContextFlagShippedExemptionsStayNarrow(t *testing.T) {
	t.Parallel()

	for key, reason := range runContextFlagExemptions() {
		t.Run(key.command+"/"+key.flag+"/"+key.key, func(t *testing.T) {
			// Even a justified exemption must not silently survive removal of its
			// command or name a non-owned key. The assembled-tree test proves use.
			want := fmt.Sprintf("unused run-context exemption: %s --%s $%s", key.command, key.flag, key.key)
			require.Equal(t, []string{want}, runContextFlagViolations(t, &urfavecli.Command{}, map[runContextFlagKey]string{key: reason}))
			require.NotEmpty(t, reason)
			require.Contains(t, []string{"RELEASE_TAG", "TAG_NAME"}, key.key)
		})
	}
}
