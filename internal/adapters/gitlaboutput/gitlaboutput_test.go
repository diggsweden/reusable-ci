// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlaboutput_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlaboutput"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

	// A leading dash or digit is rejected too: the result has to be a
	// usable shell variable name, and _FOO or 1FOO is not.
	for _, key := range []string{"", "1bad", "bad key", "bad=value", "bad\nkey", "bad\rkey", "-lead", "bad.key"} {
		if err := s.Set(context.Background(), key, "value"); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("key %q: err = %v, want ErrValidation", key, err)
		}
	}

	// The dotenv file is line-oriented, so either terminator would forge
	// a further variable.
	for _, value := range []string{"one\ntwo", "one\rtwo", "one\r\ntwo"} {
		if err := s.Set(context.Background(), "bad", value); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("value %q: err = %v, want ErrValidation", value, err)
		}
	}
}

// TestSet_DashAndUnderscoreKeysCollide records that the dotenv key
// mapping is not injective: "-" becomes "_", so two distinct sink keys
// can become one GitLab variable, and the later write wins.
//
// No two keys emitted today collide -- all 69 were checked -- but the
// codebase mixes both spellings, so this is the shape a future collision
// would take. It is here to be found by whoever adds the key that
// collides, rather than as a guard over the whole key set.
func TestSet_DashAndUnderscoreKeysCollide(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out.env", nil)

	s := gitlaboutput.New(path)
	require.NoError(t, s.Set(context.Background(), "image-digest", "first"))
	require.NoError(t, s.Set(context.Background(), "image_digest", "second"))
	require.NoError(t, s.Close(context.Background()))

	if got, want := string(fsys.ReadFile("out.env")), "IMAGE_DIGEST=first\nIMAGE_DIGEST=second\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestSetMultiline_Unsupported(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := gitlaboutput.New(fsys.WriteFile("out.env", nil))

	// ErrUnsupported, not a validation failure: a dotenv file has no
	// multi-line form at all, so this is a capability gap rather than bad
	// input, and callers can tell the two apart.
	err := s.SetMultiline(context.Background(), "result-json", []string{"{}"})
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("SetMultiline error = %v, want ErrUnsupported", err)
	}
}

func TestSet_MissingCIOutputErrors(t *testing.T) {
	s := gitlaboutput.New("")

	// ErrUsage: the variable is simply not set. Note this differs from
	// ghaoutput, which falls back to a /dev/null sink when $GITHUB_OUTPUT
	// is unset so a command can be run locally -- here an unset $CI_OUTPUT
	// is refused instead.
	err := s.Set(context.Background(), "key", "value")
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("Set error = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "CI_OUTPUT is required") {
		t.Errorf("error should name the variable to set: %v", err)
	}
}

func TestSetAfterClose_Errors(t *testing.T) {
	fsys := testfs.NewReal(t)
	s := gitlaboutput.New(fsys.WriteFile("out.env", nil))

	require.NoError(t, s.Close(context.Background()))

	if err := s.Set(context.Background(), "key", "value"); err == nil {
		t.Fatal("expected Set after Close to error")
	}

	// Both entry points, so a closed sink cannot be written through
	// either one.
	if err := s.SetMultiline(context.Background(), "key", []string{"v"}); err == nil {
		t.Error("expected SetMultiline after Close to error")
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
