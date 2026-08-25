// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gradle shells out to the project-local `./gradlew` wrapper.
// Pure helpers (init-script rendering, summary rendering) live in
// internal/domain/build.
package gradle

import (
	"context"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Adapter wraps the gradle wrapper. Bin is overridable for tests; the
// production path is the project-local "./gradlew".
type Adapter struct {
	Bin string // empty → "./gradlew"
}

// New returns an Adapter using the project-local ./gradlew.
func New() *Adapter { return &Adapter{} }

// RunInherit invokes ./gradlew with the given arguments and streams
// stdout and stderr to the provided writers. The build / SBOM phases
// want this so the user sees gradle's own progress in CI.
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	return a.RunInDirInherit(ctx, "", stdout, stderr, args...)
}

// RunInDirInherit invokes ./gradlew in dir and streams stdout/stderr.
func (a *Adapter) RunInDirInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	return a.RunInDirEnvInherit(ctx, dir, nil, stdout, stderr, args...)
}

// RunInDirEnvInherit is RunInDirInherit with extra environment entries
// layered over the parent environment. It exists for the publish path,
// where Gradle takes credentials as ORG_GRADLE_PROJECT_* project
// properties.
//
// The environment is the delivery channel on purpose: gradle project
// properties passed as -P<name>=<value> argv would land the credential
// in the process table, where any other process on the runner can read
// it. Entries are keyed "NAME" → "value"; a nil or empty map is exactly
// RunInDirInherit.
//
// Values may be secrets. They are never logged here, and the caller is
// responsible for keeping them out of stdout/stderr sinks.
func (a *Adapter) RunInDirEnvInherit(ctx context.Context, dir string, env map[string]string, stdout, stderr io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if len(env) > 0 {
		// Start from the parent env: gradle needs PATH, HOME, JAVA_HOME
		// and the runner's GRADLE_USER_HOME to work at all. Sorted so the
		// child environment is deterministic, which makes the adapter
		// testable without depending on Go's map iteration order.
		parent := os.Environ()
		merged := make([]string, 0, len(parent)+len(env))
		merged = append(merged, parent...)

		for _, name := range slices.Sorted(maps.Keys(env)) {
			merged = append(merged, name+"="+env[name])
		}

		cmd.Env = merged
	}

	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), safeexec.FirstArg(args))
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "./gradlew"
}
