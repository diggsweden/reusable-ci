// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"encoding/json"
	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGoogleCredentialBoundary_ParsesKeysWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"type", "client_email", "private_key"} {
		for _, value := range []any{"SECRET-CANARY", map[string]string{"SECRET-CANARY": "value"}} {
			fields := map[string]any{"type": "service_account", "client_email": "EMAIL-CANARY", "private_key": "KEY-CANARY"}
			fields[field] = value
			body, err := json.Marshal(fields)
			require.NoError(t, err)

			var log bytes.Buffer

			err = apppublish.GooglePlayCheckCredentials(t.Context(), &log, apppublish.GooglePlayCheckCredentialsInput{ServiceAccountJSON: string(body)})
			require.ErrorIs(t, err, errs.ErrValidation)

			for _, secret := range []string{"SECRET-CANARY", "EMAIL-CANARY", "KEY-CANARY"} {
				require.NotContains(t, err.Error(), secret)
				require.NotContains(t, log.String(), secret)
			}
		}
	}
}

func TestNPMTarballBoundary_RefusalPreservesCallerInput(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"missing-entrypoint", "stale-entrypoint"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(dir, "dist"), 0o700))

			wantFiles := map[string]string{"package.json": "original-package"}
			if kind == "stale-entrypoint" {
				wantFiles["dist/cli.js"] = "stale-entrypoint"
			}

			for name, body := range wantFiles {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
			}

			makeNPMTarball(t, dir, "app.tgz", map[string]string{"package.json": "replacement", "dist/index.js": "wrong entrypoint"})
			before, err := os.ReadFile(filepath.Join(dir, "app.tgz"))
			require.NoError(t, err)

			wantFiles["app.tgz"] = string(before)
			err = apppublish.NPMValidateTarball(t.Context(), io.Discard, io.Discard, output.Annotator{}, apppublish.NPMValidateTarballInput{Dir: dir})
			assert.ErrorIs(t, err, errs.ErrValidation) //nolint:testifylint // wrong classification must not suppress the independent preservation checks.

			for name, want := range wantFiles {
				body, readErr := os.ReadFile(filepath.Join(dir, name))
				if assert.NoError(t, readErr) {
					assert.Equal(t, want, string(body))
				}
			}

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 3)
		})
	}
}

type npmVersionEvidence struct {
	args []string
	body string
}

func (f *npmVersionEvidence) Run(_ context.Context, _ string, args ...string) (string, string, error) {
	f.args = append([]string(nil), args...)

	return f.body, "", nil
}

func TestNPMVersionBoundary_RequiresExactEvidence(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", `"1.2.3"`, `{"name":"other","version":"1.2.3"}`, `{"name":"@org/app","version":"1.2.4"}`, `{"name":"@org/app","version":"1.2.3"}`} {
		npm := &npmVersionEvidence{body: body}
		sink := fakeoutputsink.New(t)
		err := apppublish.NPMCheckVersion(t.Context(), npm, sink, io.Discard, output.Annotator{}, apppublish.NPMCheckVersionInput{Dir: t.TempDir(), Name: "@org/app", Version: "1.2.3", Registry: "https://registry.example"})
		require.Equal(t, []string{"view", "@org/app@1.2.3", "name", "version", "--json", "--registry", "https://registry.example"}, npm.args)

		if body == `{"name":"@org/app","version":"1.2.3"}` {
			require.NoError(t, err)
			require.Equal(t, "true", sink.Single("already-published"))
		} else {
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Empty(t, sink.Keys())
		}
	}
}
