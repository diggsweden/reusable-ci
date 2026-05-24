// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gradle_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/gradle"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestNew(t *testing.T) {
	if gradle.New() == nil {
		t.Fatal("New returned nil")
	}
}

// stubGradlew writes a fake ./gradlew script in the test's tmpdir and
// returns its absolute path. Tests pass that path via Adapter.Bin.
func stubGradlew(t *testing.T, body string) string {
	t.Helper()
	fsys := testfs.NewReal(t)

	path := fsys.Path("gradlew")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { //nolint:gosec // test fixture; must be exec'able.
		t.Fatal(err)
	}

	return path
}

func TestRunInherit_PassesArgsAndStreamsOutput(t *testing.T) {
	bin := stubGradlew(t, `printf 'gradle running %s\n' "$*"; printf 'warn\n' >&2`)
	a := &gradle.Adapter{Bin: bin}

	var stdout, stderr bytes.Buffer
	if err := a.RunInherit(context.Background(), &stdout, &stderr, "build", "-x", "test"); err != nil {
		t.Fatalf("RunInherit: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "gradle running build -x test" {
		t.Errorf("stdout = %q", got)
	}

	if got := strings.TrimSpace(stderr.String()); got != "warn" {
		t.Errorf("stderr = %q", got)
	}
}

func TestRunInherit_NonZeroExitWraps(t *testing.T) {
	bin := stubGradlew(t, `printf 'oops\n' >&2; exit 7`)
	a := &gradle.Adapter{Bin: bin}

	err := a.RunInherit(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "cyclonedxBom")
	if err == nil {
		t.Fatal("expected error from exit 7")
	}

	if !strings.Contains(err.Error(), "cyclonedxBom") {
		t.Errorf("error missing args: %v", err)
	}
}
