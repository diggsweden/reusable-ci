// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
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

	dir := sink.Single("secret-dir")
	if filepath.Dir(dir) != fsys.Root || !strings.HasPrefix(filepath.Base(dir), "reusable-ci-build-secrets-") {
		t.Fatalf("secret-dir = %q, want unique child of %q", dir, fsys.Root)
	}

	// Files are written at the lowercased id.
	for _, want := range []struct{ name, body string }{
		{"db_password", "pw-value"},
		{"api_token", "tok-value"},
	} {
		path := filepath.Join(dir, want.name)

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
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	gotNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		gotNames = append(gotNames, entry.Name())
	}

	slices.Sort(gotNames)

	if wantNames := []string{"api_token", "db_password"}; !slices.Equal(gotNames, wantNames) {
		t.Errorf("materialized %v, want %v", gotNames, wantNames)
	}

	// One mount spec per declared name, in declaration order, encoded as a
	// scalar-safe JSON array for every provider output sink.
	wantMounts := []string{
		"id=db_password,src=" + filepath.Join(dir, "db_password"),
		"id=api_token,src=" + filepath.Join(dir, "api_token"),
	}

	var gotMounts []string
	if err := json.Unmarshal([]byte(sink.Single("secret-mounts")), &gotMounts); err != nil {
		t.Fatalf("secret-mounts is not JSON: %v", err)
	}

	if !slices.Equal(gotMounts, wantMounts) {
		t.Errorf("secret-mounts = %v, want %v", gotMounts, wantMounts)
	}
}

func TestMaterializeBuildSecrets_CreatesPrivateUniqueDir(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()

	sink := fakeoutputsink.New(t)
	if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD",
		EnvelopeJSON: `{"DB_PASSWORD":"pw-value"}`,
		OutputDir:    parent,
	}); err != nil {
		t.Fatal(err)
	}

	dir := sink.Single("secret-dir")

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o700 {
		t.Errorf("secret dir mode = %v, want 0700", info.Mode().Perm())
	}

	second := fakeoutputsink.New(t)
	if err := appcontainer.MaterializeBuildSecrets(context.Background(), second, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "DB_PASSWORD",
		EnvelopeJSON: `{"DB_PASSWORD":"pw-value"}`,
		OutputDir:    parent,
	}); err != nil {
		t.Fatal(err)
	}

	if second.Single("secret-dir") == dir {
		t.Fatalf("two invocations reused secret directory %q", dir)
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

	if got := sink.Single("secret-dir"); got != "" {
		t.Errorf("secret-dir = %q, want empty", got)
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

// TestMaterializeBuildSecrets_MalformedEnvelopeIsActionable requires the
// refusal to be usable, not merely classified.
//
// The name promised "actionable" and the body asserted a sentinel, which any
// error carrying ErrInvalidConfig satisfies — including a bare "unexpected end
// of JSON input" with nothing naming what the operator has to fix. The
// envelope arrives as one environment variable set by a caller workflow, so
// the two things that make the message act are the variable's name and the
// shape it must hold.
//
// The one truncated literal is also widened. A truncated string is the case
// the JSON decoder rejects most obviously; the shapes that actually reach a
// pipeline are well-formed JSON of the wrong type, and every one of them has
// to land on the same refusal rather than on a decoder message that reads like
// a bug in this tool.
//
// Nothing in the message may echo the envelope. It holds the values verbatim,
// and this error travels to a CI log.
func TestMaterializeBuildSecrets_MalformedEnvelopeIsActionable(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t-canary-value"

	// Each row says which of the two refusals it must get. The split is not
	// cosmetic: a JSON null is not a decode failure in Go — it unmarshals into
	// a map as "leave it nil" — so an envelope of `null`, which is what a
	// caller workflow produces when it interpolates an unset secret, is a
	// well-formed EMPTY envelope and is correctly diagnosed as a missing key
	// rather than as malformed JSON. Asserting the parse message for it would
	// have pinned a worse diagnosis than the one the code gives.
	const (
		malformed = "{name: value}"
		missing   = `does not contain key "DB_PASSWORD"`
	)

	for name, tc := range map[string]struct{ envelope, want string }{
		"a truncated object":        {`{"DB_PASSWORD": "` + secret, malformed},
		"an array":                  {`["` + secret + `"]`, malformed},
		"a bare string":             {`"` + secret + `"`, malformed},
		"a numeric value":           {`{"DB_PASSWORD": 1234}`, malformed},
		"a nested object value":     {`{"DB_PASSWORD": {"value": "` + secret + `"}}`, malformed},
		"a value list":              {`{"DB_PASSWORD": ["` + secret + `"]}`, malformed},
		"trailing junk after close": {`{"DB_PASSWORD": "` + secret + `"} and then some`, malformed},
		"a null envelope":           {`null`, missing},
		"a null value":              {`{"DB_PASSWORD": null}`, missing},
		"an empty object":           {`{}`, missing},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			sink := fakeoutputsink.New(t)

			err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
				Names:        "DB_PASSWORD",
				EnvelopeJSON: tc.envelope,
				OutputDir:    dir,
			})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want wrapped ErrInvalidConfig", err)
			}

			msg := err.Error()
			for _, want := range []string{"REUSABLE_CI_BUILD_SECRETS_JSON", tc.want} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal does not mention %q, so it does not say what to fix:\n%s", want, msg)
				}
			}

			if strings.Contains(msg, secret) {
				t.Errorf("the refusal echoes the envelope's secret value:\n%s", msg)
			}

			// Nothing was written and nothing was published. A partially
			// materialised secret directory left behind by a refused call is
			// key material on the runner's disk that no later step removes.
			left, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatalf("read output dir: %v", readErr)
			}

			if len(left) != 0 {
				t.Errorf("a refused call left %d entr(y/ies) under the output directory", len(left))
			}

			if keys := sink.Keys(); len(keys) != 0 {
				t.Errorf("a refused call published %v", keys)
			}
		})
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
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)

			sink := fakeoutputsink.New(t)
			if err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
				Names:        input,
				EnvelopeJSON: `{"A":"a","B":"b","C":"c"}`,
				OutputDir:    fsys.Root,
			}); err != nil {
				t.Fatalf("input %q rejected: %v", input, err)
			}

			// All three, not merely "no error": a parser that yielded one
			// name would still be accepted, since that name is in the
			// envelope too.
			entries, err := os.ReadDir(sink.Single("secret-dir"))
			if err != nil {
				t.Fatal(err)
			}

			got := make([]string, 0, len(entries))
			for _, entry := range entries {
				got = append(got, entry.Name())
			}

			slices.Sort(got)

			if want := []string{"a", "b", "c"}; !slices.Equal(got, want) {
				t.Errorf("materialized %v, want %v", got, want)
			}
		})
	}
}

func TestMaterializeBuildSecrets_MultipleSecretsUseScalarOutputOnRealSink(t *testing.T) {
	for _, tc := range []struct {
		name       string
		names      string
		envelope   string
		wantMounts int
	}{
		{
			name:       "one secret",
			names:      "A",
			envelope:   `{"A":"a"}`,
			wantMounts: 1,
		},
		{
			name:       "two secrets",
			names:      "A\nB",
			envelope:   `{"A":"a","B":"b"}`,
			wantMounts: 2,
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
				OutputDir:    dir,
			})
			if err != nil {
				t.Fatal(err)
			}

			body, readErr := os.ReadFile(outPath)
			if readErr != nil {
				t.Fatal(readErr)
			}

			lines := strings.Split(strings.TrimSpace(string(body)), "\n")
			line := ""

			const prefix = "secret-mounts="
			for _, candidate := range lines {
				if strings.HasPrefix(candidate, prefix) {
					line = candidate
				}
			}

			if line == "" {
				t.Fatalf("output = %q, want %q prefix", body, prefix)
			}

			var mounts []string
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &mounts); err != nil {
				t.Fatalf("decode output %q: %v", line, err)
			}

			if len(mounts) != tc.wantMounts {
				t.Errorf("mounts = %v, want %d entries", mounts, tc.wantMounts)
			}
		})
	}
}

func TestMaterializeBuildSecrets_OutputFailureRemovesWrittenFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	sink := fakeoutputsink.New(t)
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "A B",
		EnvelopeJSON: `{"A":"a","B":"b"}`,
		OutputDir:    dir,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want the sink failure surfaced", err)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if len(entries) != 0 {
		t.Errorf("failed materialization left files behind: %v", entries)
	}
}

func TestMaterializeBuildSecrets_RejectsSymlinkedTempParent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	link := filepath.Join(base, "linked-temp")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}

	err := appcontainer.MaterializeBuildSecrets(context.Background(), fakeoutputsink.New(t), io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "A",
		EnvelopeJSON: `{"A":"value"}`,
		OutputDir:    link,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("symlinked temp parent error = %v, want ErrValidation", err)
	}
}

// secretMountsFailingSink records outputs but refuses secret-mounts, the last
// write, which happens after every secret file is already on disk.
type secretMountsFailingSink struct{ *fakeoutputsink.Sink }

var errMountsRefused = errors.New("output file closed")

func (s secretMountsFailingSink) Set(ctx context.Context, key, value string) error {
	if key == "secret-mounts" {
		return errMountsRefused
	}

	return s.Sink.Set(ctx, key, value)
}

// TestMaterializeBuildSecrets_LateFailureRemovesEverySecret fails the
// invocation at its last step, after both secrets are written and secret-dir
// has been published. The existing output-failure test closes the sink, which
// refuses secret-dir before any file exists, so it never had anything to
// clean up. Here the directory the output named must be gone, and the parent
// left empty, so the workflow's cleanup has nothing to miss.
func TestMaterializeBuildSecrets_LateFailureRemovesEverySecret(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	sink := secretMountsFailingSink{fakeoutputsink.New(t)}

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "A B",
		EnvelopeJSON: `{"A":"synthetic-a","B":"synthetic-b"}`,
		OutputDir:    parent,
	})
	if !errors.Is(err, errMountsRefused) {
		t.Fatalf("err = %v, want the sink failure", err)
	}

	secretDir := sink.Single("secret-dir")
	if filepath.Dir(secretDir) != parent {
		t.Fatalf("secret-dir = %q, want a directory under %s", secretDir, parent)
	}

	if _, statErr := os.Stat(secretDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("secret directory %s survived the failure (stat err %v)", secretDir, statErr)
	}

	if entries, readErr := os.ReadDir(parent); readErr != nil || len(entries) != 0 {
		t.Errorf("parent holds %v (err %v), want nothing", entries, readErr)
	}
}

// TestMaterializeBuildSecrets_RefusesARepeatedName covers a name declared
// twice. Both declarations would map to one mount file; it is refused as
// configuration before anything is created, rather than failing on the
// second file after the first secret was written.
func TestMaterializeBuildSecrets_RefusesARepeatedName(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	sink := fakeoutputsink.New(t)

	err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, io.Discard, appcontainer.MaterializeBuildSecretsInput{
		Names:        "A,B\nA",
		EnvelopeJSON: `{"A":"synthetic-a","B":"synthetic-b"}`,
		OutputDir:    parent,
	})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), `"A" is declared more than once`) {
		t.Errorf("err = %v, want ErrInvalidConfig naming A", err)
	}

	if entries, readErr := os.ReadDir(parent); readErr != nil || len(entries) != 0 || len(sink.Keys()) != 0 {
		t.Errorf("parent = %v (err %v), outputs = %v; want nothing created or emitted", entries, readErr, sink.Keys())
	}
}
