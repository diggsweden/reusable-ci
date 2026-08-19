// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ghaoutput"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestMaterializeBuildSecrets_MaterializesOnlyTheDeclaredNames covers the happy
// path: two declared names become two files the builder can mount, and the
// envelope's third entry becomes nothing at all.
//
// Both halves are asserted exhaustively rather than by looking for what should
// be there. These are secrets on a shared runner: a file nobody declared, or a
// mount entry pointing at one, is the failure worth catching, and a
// contains-check cannot see either.
func TestMaterializeBuildSecrets_MaterializesOnlyTheDeclaredNames(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, &out, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD\nAPI_TOKEN",
		EnvelopeJSON: `{"DB_PASSWORD":"pw-value","API_TOKEN":"tok-value","EXTRA":"ignored"}`,
		OutputDir:    fsys.Root,
	}); err != nil {
		t.Fatal(err)
	}

	// Files are written at the lowercased id.
	for _, want := range []struct{ name, body string }{
		{"db_password", "pw-value"},
		{"api_token", "tok-value"},
	} {
		path := filepath.Join(fsys.Root, want.name)

		got, err := os.ReadFile(path) //nolint:gosec // test fixture path
		if err != nil {
			t.Errorf("read %s: %v", path, err)

			continue
		}

		if string(got) != want.body {
			t.Errorf("%s = %q, want %q", path, got, want.body)
		}

		// Mode must be 0600 — an adversary on the same runner should not be
		// able to read the materialized secret.
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat %s: %v", path, err)

			continue
		}

		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", path, info.Mode().Perm())
		}
	}

	// Exactly those two files. EXTRA is in the envelope and undeclared, so it
	// must not be written -- and neither must anything else.
	entries, err := os.ReadDir(fsys.Root)
	if err != nil {
		t.Fatal(err)
	}

	gotNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		gotNames = append(gotNames, entry.Name())
	}

	sort.Strings(gotNames)

	if wantNames := []string{"api_token", "db_password"}; !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("materialized %v, want %v", gotNames, wantNames)
	}

	// One mount line per declared name, in the order they were declared.
	wantMounts := strings.Join([]string{
		"id=db_password,src=" + filepath.Join(fsys.Root, "db_password"),
		"id=api_token,src=" + filepath.Join(fsys.Root, "api_token"),
	}, "\n")
	if got := sink.Single("secret-mounts"); got != wantMounts {
		t.Errorf("secret-mounts =\n%s\nwant\n%s", got, wantMounts)
	}
}

func TestMaterializeBuildSecrets_TightensPreexistingLooseDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "build-secrets")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // deliberately loose: the SUT must tighten it.
		t.Fatal(err)
	}
	// Defend against a permissive umask masking the test setup.
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // deliberately loose: the SUT must tighten it.
		t.Fatal(err)
	}

	sink := fakeoutputsink.New(t)
	if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD",
		EnvelopeJSON: `{"DB_PASSWORD":"pw-value"}`,
		OutputDir:    dir,
	}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o700 {
		t.Errorf("secret dir mode = %v, want 0700", info.Mode().Perm())
	}
}

// TestMaterializeBuildSecrets_EmptyNamesIsNoop covers the no-build-
// secrets case: workflow input is empty so the step emits empty output
// without touching the envelope. docker/build-push-action treats an
// empty `secrets:` value as no mounts.
func TestMaterializeBuildSecrets_EmptyNamesIsNoop(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names: "",
		// Envelope deliberately bogus — should not be parsed.
		EnvelopeJSON: "garbage",
	}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("secret-mounts"); got != "" {
		t.Errorf("secret-mounts = %q, want empty", got)
	}
}

// TestMaterializeBuildSecrets_MissingEnvelopeKey is the fail-fast
// regression: artifacts.yml declared a name that the caller didn't
// pack into the envelope. The error message must point at the missing
// key so an adopter can fix it in one round-trip.
func TestMaterializeBuildSecrets_MissingEnvelopeKey(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD\nAPI_TOKEN",
		EnvelopeJSON: `{"DB_PASSWORD":"pw-only"}`,
		OutputDir:    fsys.Root,
	})
	if err == nil {
		t.Fatal("expected error when envelope is missing a declared key")
	}

	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("err = %v, want wrapped ErrInvalidConfig", err)
	}

	if !strings.Contains(err.Error(), "API_TOKEN") {
		t.Errorf("err should mention the missing key API_TOKEN: %v", err)
	}
}

// TestMaterializeBuildSecrets_EmptyEnvelopeWithNamesFails: the caller
// workflow forgot to set REUSABLE_CI_BUILD_SECRETS_JSON entirely.
// Catch it with a clear error rather than silently producing empty
// mounts that surface as cryptic BuildKit errors.
func TestMaterializeBuildSecrets_EmptyEnvelopeWithNamesFails(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD",
		EnvelopeJSON: "",
	})
	if err == nil {
		t.Fatal("expected error when envelope is empty but names are declared")
	}

	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err = %v, want wrapped ErrMissingInput", err)
	}
}

// TestMaterializeBuildSecrets_MalformedEnvelopeIsActionable: clear
// error when the JSON is malformed.
func TestMaterializeBuildSecrets_MalformedEnvelopeIsActionable(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD",
		EnvelopeJSON: `{"DB_PASSWORD": "missing closing brace`,
	})
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}

	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("err = %v, want wrapped ErrInvalidConfig", err)
	}
}

// TestMaterializeBuildSecrets_SplitsNewlinesCommasOrSpaces: workflow
// passes the names as a multi-line YAML string; the parser tolerates
// whatever separator the caller chose.
func TestMaterializeBuildSecrets_SplitsNewlinesCommasOrSpaces(t *testing.T) {
	t.Parallel()

	cases := []string{
		"A\nB\nC",
		"A, B, C",
		"A B C",
		"  A  \n  B  \n  C  ",
	}

	for _, input := range cases {
		fsys := testfs.NewReal(t)

		sink := fakeoutputsink.New(t)
		if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
			Names:        input,
			EnvelopeJSON: `{"A":"a","B":"b","C":"c"}`,
			OutputDir:    fsys.Root,
		}); err != nil {
			t.Errorf("input %q rejected: %v", input, err)
		}
	}
}

// TestMaterializeBuildSecrets_MultipleSecretsFailOnARealSink pins a defect
// the fake sink cannot see.
//
// secret-mounts is a newline-separated list emitted through the scalar
// Set. Both line-oriented sinks refuse a scalar containing a newline --
// ghaoutput and gitlaboutput each guard it, because such a value would
// forge further entries in their key=value files. So the command works
// with one declared build secret and fails with two, on every forge.
//
// The fake sink accepts any value, which is why the tests above pass. This
// one drives the real GitHub Actions sink instead. See
// docs/open-questions.md -- the fix is a design decision, not a one-liner,
// because gitlaboutput has no multiline path at all.
func TestMaterializeBuildSecrets_MultipleSecretsFailOnARealSink(t *testing.T) {
	for _, tc := range []struct {
		name       string
		names      string
		envelope   string
		wantErr    error
		wantOutput string
	}{
		{
			name:       "one secret is emitted",
			names:      "A",
			envelope:   `{"A":"a"}`,
			wantOutput: "secret-mounts=id=a,src=",
		},
		{
			name:     "two secrets are refused by the sink",
			names:    "A\nB",
			envelope: `{"A":"a","B":"b"}`,
			wantErr:  errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			outPath := filepath.Join(dir, "gha_output")

			if err := os.WriteFile(outPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}

			t.Setenv("GITHUB_OUTPUT", outPath)

			err := appcontainer.MaterializeBuildSecrets(context.Background(), ghaoutput.NewFromEnv(), io.Discard, appcontainer.MaterializeBuildSecretsInput{
				Names:        tc.names,
				EnvelopeJSON: tc.envelope,
				OutputDir:    filepath.Join(dir, "secrets"),
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			body, readErr := os.ReadFile(outPath)
			if readErr != nil {
				t.Fatal(readErr)
			}

			if tc.wantErr != nil {
				// Nothing written: the run fails after the secret files
				// are on disk but before anything names them.
				if len(body) != 0 {
					t.Errorf("wrote %q despite the refusal", body)
				}

				return
			}

			if !strings.HasPrefix(string(body), tc.wantOutput) {
				t.Errorf("output = %q, want it to start with %q", body, tc.wantOutput)
			}
		})
	}
}
