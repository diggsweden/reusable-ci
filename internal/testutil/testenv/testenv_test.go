// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package testenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

func TestNew_IsolatesAndProvidesTempPaths(t *testing.T) {
	env := testenv.New(t)
	if env.Home == "" || env.Temp == "" {
		t.Fatalf("env = %+v", env)
	}
	if got := os.Getenv("HOME"); got != env.Home {
		t.Errorf("HOME = %q, want %q", got, env.Home)
	}
	if got := os.Getenv("TMPDIR"); got != env.Temp {
		t.Errorf("TMPDIR = %q, want %q", got, env.Temp)
	}
	if filepath.Dir(env.Path("x", "y")) != filepath.Join(env.Temp, "x") {
		t.Errorf("unexpected derived path: %q", env.Path("x", "y"))
	}
}

func TestSetenv_ForwardsInsideIsolatedEnv(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("FOO", "bar")
	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q", got)
	}
}

func TestMkdirAll_CreatesDirectoryUnderTemp(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("nested", "dir")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}
	if filepath.Dir(dir) != env.Path("nested") {
		t.Errorf("dir = %q, want under %q", dir, env.Path("nested"))
	}
}
