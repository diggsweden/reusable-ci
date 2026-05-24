// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// TestMaterializeBuildSecrets_WritesFilesAndEmitsBuildxFormat is the
// happy path: a 2-entry envelope produces two tmpfiles + the right
// docker/build-push-action `secrets:` payload.
func TestMaterializeBuildSecrets_WritesFilesAndEmitsBuildxFormat(t *testing.T) {
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

	// Files written at the lowercased id.
	for _, want := range []struct{ name, body string }{
		{"db_password", "pw-value"},
		{"api_token", "tok-value"},
	} {
		path := filepath.Join(fsys.Root, want.name)

		got, err := os.ReadFile(path) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		if string(got) != want.body {
			t.Errorf("%s = %q, want %q", path, got, want.body)
		}

		// Mode must be 0600 — adversary on the same runner shouldn't
		// be able to read the materialized secret.
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", path, info.Mode().Perm())
		}
	}

	// EXTRA must not produce a file — extras in the envelope are
	// ignored, only declared names get materialized.
	if _, err := os.Stat(filepath.Join(fsys.Root, "extra")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("EXTRA should not have been materialized")
	}

	// buildx-secrets output: one line per declared name, lowercased id.
	got := sink.Single("buildx-secrets")
	for _, want := range []string{
		"id=db_password,src=" + filepath.Join(fsys.Root, "db_password"),
		"id=api_token,src=" + filepath.Join(fsys.Root, "api_token"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildx-secrets missing %q\nfull:\n%s", want, got)
		}
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

	if got := sink.Single("buildx-secrets"); got != "" {
		t.Errorf("buildx-secrets = %q, want empty", got)
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
