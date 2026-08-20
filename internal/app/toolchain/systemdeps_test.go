// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// InstallSystemDependencies was uncovered. The strings it accepts become
// argv for `apt-get install` on a CI runner, and they arrive from
// workflow configuration -- so the name check is the boundary between
// "install these packages" and "run this instead".

// recordingInstaller captures what would have been installed.
type recordingInstaller struct {
	calls    int
	packages []string
}

func (r *recordingInstaller) Install(_ context.Context, packages []string, _ io.Writer) error {
	r.calls++

	r.packages = append([]string(nil), packages...)

	return nil
}

// TestInstallSystemDependencies_RefusesUnsafePackageNames is the claim
// that matters. A leading dash is the sharpest case: apt-get would read
// it as an option, and options like --allow-downgrades or
// --allow-unauthenticated change what gets installed and whether its
// signature is checked.
func TestInstallSystemDependencies_RefusesUnsafePackageNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
	}{
		{name: "a leading dash reads as an option", input: "--allow-unauthenticated"},
		{name: "a short option", input: "-y"},
		{name: "a command separator", input: "curl; rm -rf /"},
		{name: "a shell substitution", input: "curl $(id)"},
		{name: "backticks", input: "curl `id`"},
		{name: "a pipe", input: "curl|sh"},
		{name: "an ampersand", input: "curl&"},
		{name: "a path", input: "/tmp/evil.deb"},
		{name: "a relative path", input: "../evil.deb"},
		{name: "a package with a slash suite pin", input: "curl/bookworm-backports"},
		{name: "an equals version pin", input: "curl=1.2.3"},
		{name: "a glob", input: "curl*"},
		{name: "one bad name among good ones", input: "curl jq -y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			installer := &recordingInstaller{}

			err := toolchain.InstallSystemDependencies(context.Background(), installer, io.Discard,
				toolchain.InstallSystemDependenciesInput{Packages: tc.input})
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage for %q", err, tc.input)
			}

			// The refusal has to happen before anything is installed:
			// rejecting the batch after installing the good half would
			// leave the runner in a state nobody asked for.
			if installer.calls != 0 {
				t.Errorf("apt-get was invoked despite the refusal, with %v", installer.packages)
			}
		})
	}
}

// This one reaches the apt-get lookup, so the runner must have one --
// and it must be ours, not the host's. Without the stub the test passes
// on a Debian developer machine and fails on Alpine or macOS.
func TestInstallSystemDependencies_AcceptsRealPackageNames(t *testing.T) {
	// No t.Parallel(): mockbinary prepends to PATH via t.Setenv.
	bins := mockbinary.New(t)
	bins.Add("apt-get", ":")

	installer := &recordingInstaller{}

	// Newline-separated is how a YAML block scalar arrives; the
	// duplicate is what a merged default plus explicit list produces.
	err := toolchain.InstallSystemDependencies(context.Background(), installer, io.Discard,
		toolchain.InstallSystemDependenciesInput{Packages: "curl\njq  libssl-dev\ng++\ncurl\n"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"curl", "jq", "libssl-dev", "g++"}
	if !slices.Equal(installer.packages, want) {
		t.Errorf("packages = %v, want %v (deduplicated, order preserved)", installer.packages, want)
	}
}

// TestInstallSystemDependencies_NoPackagesIsANoOp keeps an empty
// configuration from running apt-get with no arguments, which on some
// apt versions is an interactive prompt rather than a no-op.
func TestInstallSystemDependencies_NoPackagesIsANoOp(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "   ", "\n\n"} {
		installer := &recordingInstaller{}

		if err := toolchain.InstallSystemDependencies(context.Background(), installer, io.Discard,
			toolchain.InstallSystemDependenciesInput{Packages: input}); err != nil {
			t.Fatalf("%q: %v", input, err)
		}

		if installer.calls != 0 {
			t.Errorf("%q: apt-get was invoked with no packages", input)
		}
	}
}

// TestInstallSystemDependencies_MissingAPT covers the two answers on a
// runner without apt. The flag is the whole difference, so both are
// asserted: silently skipping when the caller did not ask to skip would
// let a build proceed without dependencies it declared.
func TestInstallSystemDependencies_MissingAPT(t *testing.T) {
	// No t.Parallel(): PATH is replaced via t.Setenv.
	t.Setenv("PATH", t.TempDir())

	installer := &recordingInstaller{}

	err := toolchain.InstallSystemDependencies(context.Background(), installer, io.Discard,
		toolchain.InstallSystemDependenciesInput{Packages: "curl", SkipIfMissingAPT: false})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("err = %v, want ErrDependencyUnavailable", err)
	}

	if err := toolchain.InstallSystemDependencies(context.Background(), installer, io.Discard,
		toolchain.InstallSystemDependenciesInput{Packages: "curl", SkipIfMissingAPT: true}); err != nil {
		t.Fatalf("with SkipIfMissingAPT this must be a no-op: %v", err)
	}

	if installer.calls != 0 {
		t.Errorf("the installer ran on a runner with no apt-get")
	}
}

func TestTrustMiseConfig_RequiresARunner(t *testing.T) {
	t.Parallel()

	if err := toolchain.TrustMiseConfig(context.Background(), nil, io.Discard,
		toolchain.TrustMiseConfigInput{}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}
