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

// TestSet_RefusesTwoKeysThatBecomeOneVariable covers the one way this sink can
// lose a value without reporting anything.
//
// The key mapping is not injective: "-" and "_" both become "_", so
// "image-digest" and "image_digest" are one GitLab variable. dotenv is
// last-wins, so before the refusal both writes succeeded, the file held two
// IMAGE_DIGEST lines, and a release step reading it got the other key's value
// with no error raised anywhere.
//
// This replaces a test that recorded the collision as tolerated. That test
// said it was "here to be found by whoever adds the key that collides", which
// it was not: it used two hardcoded keys, so adding a colliding production key
// would not have failed it, and its claim that no live pair collides had gone
// stale by a dozen keys with nothing rechecking it. Refusing in the sink is
// the protection the comment described.
func TestSet_RefusesTwoKeysThatBecomeOneVariable(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out.env", nil)

	s := gitlaboutput.New(path)
	require.NoError(t, s.Set(context.Background(), "image-digest", "first"))

	err := s.Set(context.Background(), "image_digest", "second")
	require.ErrorIs(t, err, errs.ErrValidation)
	// Both spellings are named: the operator has to rename one, and cannot
	// without being told which two collided.
	require.Contains(t, err.Error(), `"image-digest"`)
	require.Contains(t, err.Error(), `"image_digest"`)
	require.Contains(t, err.Error(), "IMAGE_DIGEST")
	require.NoError(t, s.Close(context.Background()))

	// The refused value is not written. A second IMAGE_DIGEST line would be
	// the loss this refusal exists to prevent, reported and then committed
	// anyway.
	require.Equal(t, "IMAGE_DIGEST=first\n", string(fsys.ReadFile("out.env")))
}

// TestSet_AllowsTheSameKeyTwice is the boundary of that refusal. A caller
// overwriting its own key is one value with a later revision, not two values
// merging, and the collision guard must not turn it into an error.
func TestSet_AllowsTheSameKeyTwice(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("out.env", nil)

	s := gitlaboutput.New(path)
	require.NoError(t, s.Set(context.Background(), "image-digest", "first"))
	require.NoError(t, s.Set(context.Background(), "image-digest", "second"))
	require.NoError(t, s.Close(context.Background()))

	require.Equal(t, "IMAGE_DIGEST=first\nIMAGE_DIGEST=second\n", string(fsys.ReadFile("out.env")))
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

	// SetMultiline is refused whether the sink is open or not -- a dotenv
	// file has no multi-line form -- so this pins the capability gap, not
	// the closed-sink guard. Asserting only "it errored" would read as
	// the latter and pass for the wrong reason.
	if err := s.SetMultiline(context.Background(), "key", []string{"v"}); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("SetMultiline after Close = %v, want ErrUnsupported", err)
	}

	// The refusal has to mean nothing was appended: the runner sources this
	// dotenv file after the job ends, so a write that errored but landed
	// anyway would still become a variable.
	if data := fsys.ReadFile("out.env"); len(data) != 0 {
		t.Errorf("dotenv written after Close: %q", data)
	}
}

func TestNewFromEnv_WritesUppercasedKeysToCIOutput(t *testing.T) {
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
