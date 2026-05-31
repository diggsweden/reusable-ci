// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlaboutput_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlaboutput"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestSet_WritesDotenvScalar(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out.env", nil)

	s := gitlaboutput.New(path)
	require.NoError(t, s.Set(context.Background(), "version-no-v", "1.2.3"))
	require.NoError(t, s.Set(context.Background(), "project_name", "api"))
	require.NoError(t, s.Close(context.Background()))

	if got := string(fsys.ReadFile("out.env")); got != "VERSION_NO_V=1.2.3\nPROJECT_NAME=api\n" {
		t.Errorf("output = %q", got)
	}
}

func TestSet_RejectsInvalidDotenvData(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := gitlaboutput.New(fsys.WriteFile("out.env", nil)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for _, key := range []string{"", "1bad", "bad key", "bad=value", "bad\nkey"} {
		if err := s.Set(context.Background(), key, "value"); err == nil {
			t.Fatalf("expected key %q to be rejected", key)
		}
	}

	if err := s.Set(context.Background(), "bad", "one\ntwo"); err == nil {
		t.Fatal("expected newline value to be rejected")
	}
}

func TestSetMultiline_Unsupported(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := gitlaboutput.New(fsys.WriteFile("out.env", nil))

	err := s.SetMultiline(context.Background(), "result-json", []string{"{}"})
	if err == nil || !strings.Contains(err.Error(), "multiline output") {
		t.Fatalf("SetMultiline error = %v", err)
	}
}

func TestSet_MissingCIOutputErrors(t *testing.T) {
	s := gitlaboutput.New("")

	err := s.Set(context.Background(), "key", "value")
	if err == nil || !strings.Contains(err.Error(), "CI_OUTPUT is required") {
		t.Fatalf("Set error = %v", err)
	}
}

func TestSetAfterClose_Errors(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := gitlaboutput.New(fsys.WriteFile("out.env", nil))

	require.NoError(t, s.Close(context.Background()))

	if err := s.Set(context.Background(), "key", "value"); err == nil {
		t.Fatal("expected Set after Close to error")
	}
}

func TestNewFromEnv(t *testing.T) {
	fsys := testfs.NewReal(t)
	env := testenv.New(t)
	env.Setenv("CI_OUTPUT", fsys.WriteFile("out.env", nil))

	s := gitlaboutput.NewFromEnv()
	require.NoError(t, s.Set(context.Background(), "key", "value"))
	require.NoError(t, s.Close(context.Background()))

	if got := string(fsys.ReadFile("out.env")); got != "KEY=value\n" {
		t.Errorf("output = %q", got)
	}
}
