// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

func TestPublishTasks_Derived(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target publish.GradleTarget
		want   []string
	}{
		{publish.GradleTargetForgePackages, []string{"publishAllPublicationsToGitHubPackagesRepository"}},
		{publish.GradleTargetMavenCentral, []string{"publishAllPublicationsToMavenCentralRepository"}},
	}
	for _, tc := range cases {
		got, err := publish.PublishTasks(tc.target, "")
		if err != nil {
			t.Fatalf("PublishTasks(%q): %v", tc.target, err)
		}

		if !slices.Equal(got, tc.want) {
			t.Errorf("PublishTasks(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

// The bare `publish` task would push every publication to every
// configured repository, collapsing the two publish-stage jobs into one.
// Guard against a future "simplification" back to it.
func TestPublishTasks_NeverBarePublish(t *testing.T) {
	t.Parallel()

	for _, target := range publish.ValidGradleTargets {
		tasks, err := publish.PublishTasks(target, "")
		if err != nil {
			t.Fatal(err)
		}

		for _, task := range tasks {
			if task == "publish" {
				t.Errorf("PublishTasks(%q) returned the bare `publish` task, which is not per-destination", target)
			}
		}
	}
}

func TestPublishTasks_OverrideWins(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"publishToMavenCentral":       {"publishToMavenCentral"},
		"  publishToMavenCentral  ":   {"publishToMavenCentral"},
		"taskA taskB":                 {"taskA", "taskB"},
		"  taskA \t taskB \n taskC  ": {"taskA", "taskB", "taskC"},
		// A whitespace-only override must fall back to the derived task
		// rather than producing an empty list, which would make gradle a no-op.
		"   \t \n ": {"publishAllPublicationsToMavenCentralRepository"},
	}
	for override, want := range cases {
		got, err := publish.PublishTasks(publish.GradleTargetMavenCentral, override)
		if err != nil {
			t.Fatalf("override %q: %v", override, err)
		}

		if !slices.Equal(got, want) {
			t.Errorf("PublishTasks(override=%q) = %v, want %v", override, got, want)
		}
	}
}

func TestPublishTasks_UnknownTargetIsUsageError(t *testing.T) {
	t.Parallel()

	if _, err := publish.PublishTasks("nexus", ""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want errs.ErrUsage", err)
	}
}

func TestParseGradleTarget(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"forge-packages", " maven-central "} {
		if _, err := publish.ParseGradleTarget(raw); err != nil {
			t.Errorf("ParseGradleTarget(%q) = %v, want ok", raw, err)
		}
	}

	for _, raw := range []string{"", "nexus", "Forge-Packages", "maven_central"} {
		if _, err := publish.ParseGradleTarget(raw); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("ParseGradleTarget(%q) err = %v, want errs.ErrUsage", raw, err)
		}
	}
}

func TestCredentialBindings(t *testing.T) {
	t.Parallel()

	forge := publish.CredentialBindings(publish.GradleTargetForgePackages)
	if len(forge) != 2 {
		t.Fatalf("forge-packages bindings = %d, want 2", len(forge))
	}

	central := publish.CredentialBindings(publish.GradleTargetMavenCentral)
	if len(central) != 8 {
		t.Fatalf("maven-central bindings = %d, want 8", len(central))
	}

	if got := publish.CredentialBindings("nexus"); got != nil {
		t.Errorf("unknown target bindings = %v, want nil", got)
	}
}

func TestCredentialBinding_EnvVar(t *testing.T) {
	t.Parallel()

	b := publish.CredentialBinding{Property: "mavenCentralUsername"}
	if got, want := b.EnvVar(), "ORG_GRADLE_PROJECT_mavenCentralUsername"; got != want {
		t.Errorf("EnvVar() = %q, want %q", got, want)
	}
}

// Every binding must name a distinct property; a duplicate property with
// two slots would be a silent last-write-wins in the child env map.
func TestCredentialBindings_PropertiesUnique(t *testing.T) {
	t.Parallel()

	for _, target := range publish.ValidGradleTargets {
		seen := map[string]bool{}
		for _, b := range publish.CredentialBindings(target) {
			if seen[b.Property] {
				t.Errorf("target %q binds property %q twice", target, b.Property)
			}

			seen[b.Property] = true
		}
	}
}

func TestGradleRepositoryName(t *testing.T) {
	t.Parallel()

	if got := publish.GradleRepositoryName(publish.GradleTargetForgePackages); got != "GitHubPackages" {
		t.Errorf("got %q", got)
	}

	if got := publish.GradleRepositoryName("nexus"); got != "" {
		t.Errorf("unknown target = %q, want empty", got)
	}
}

// `release gpg import` emits the 16-character long key id; the signing
// plugin matches on the 8-character short form and reports "could not
// find secret key" for anything longer.
func TestShortSigningKeyID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"long id truncates to short", "A1B2C3D4E5F60718", "E5F60718"},
		{"short id passes through", "E5F60718", "E5F60718"},
		{"shorter than short id is left alone", "BEEF", "BEEF"},
		{"whitespace trimmed", "  A1B2C3D4E5F60718\n", "E5F60718"},
		{"empty stays empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := publish.ShortSigningKeyID(tc.in); got != tc.want {
				t.Errorf("ShortSigningKeyID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
