// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestUploadBoundary_RedactsSuppliedTokenAndPreservesCategory(t *testing.T) {
	t.Parallel()

	const token = "INVENTED_BOUNDARY_TOKEN"

	fsys := testfs.NewReal(t)
	body := `{"version":"2.1.0","runs":[{"automationDetails":{"id":"scan/1"},"results":[{"ruleId":"FIRST"}]},{"results":[{"ruleId":"SECOND"}]}]}`
	path := fsys.WriteFile("report.sarif", []byte(body))

	for _, fail := range []bool{false, true} {
		prov := fakeprovider.New(t)
		if fail {
			prov.WithUploadSARIFError(fmt.Errorf("safe context with %s: %w", token, errs.ErrPermissionDenied))
		}

		var out, diagnostics bytes.Buffer

		err := appsecurity.UploadSARIF(t.Context(), prov, &out, output.NewAnnotator(&diagnostics, output.FormatText), appsecurity.UploadSARIFInput{SARIFFile: path, Token: token, Repository: "owner/repository", SHA: strings.Repeat("a", 40), Ref: "refs/heads/main", Category: "scan"})
		if fail && !errors.Is(err, errs.ErrPermissionDenied) || !fail && err != nil {
			t.Fatalf("err=%v", err)
		}

		text := out.String() + diagnostics.String()
		if err != nil {
			text += err.Error()
		}

		if strings.Contains(text, token) || fail && !strings.Contains(text, "safe context") {
			t.Fatalf("unsafe/unhelpful diagnostic=%s", text)
		}

		calls := prov.UploadSARIFCalls()
		if len(calls) != 1 || calls[0].Token != token || calls[0].Repository != "owner/repository" {
			t.Fatal("request lost identity or credential")
		}

		var doc struct {
			Runs []struct {
				Automation struct {
					ID string `json:"id"`
				} `json:"automationDetails"`
				Results []struct {
					RuleID string `json:"ruleId"`
				} `json:"results"`
			} `json:"runs"`
		}
		if err := json.Unmarshal(calls[0].SARIF, &doc); err != nil {
			t.Fatal(err)
		}

		if len(doc.Runs) != 2 || doc.Runs[0].Automation.ID != "scan/1" || doc.Runs[1].Automation.ID != "scan/1/1" || doc.Runs[0].Results[0].RuleID != "FIRST" || doc.Runs[1].Results[0].RuleID != "SECOND" {
			t.Fatalf("analysis identity=%+v", doc)
		}

		if string(fsys.ReadFile("report.sarif")) != body {
			t.Fatal("upload changed disk input")
		}
	}
}

func TestUploadBoundary_OnlyMissingFilesSkip(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	parent := fsys.WriteFile("regular-parent", []byte("canary"))
	prov := fakeprovider.New(t)

	var out bytes.Buffer

	err := appsecurity.UploadSARIF(t.Context(), prov, &out, output.NewAnnotator(&out, output.FormatText), appsecurity.UploadSARIFInput{SARIFFile: filepath.Join(parent, "scan.sarif"), Token: "invented", Repository: "owner/repo", SHA: strings.Repeat("a", 40), Ref: "refs/heads/main"})

	var pathErr *os.PathError
	if !errors.Is(err, errs.ErrValidation) || !errors.As(err, &pathErr) || len(prov.UploadSARIFCalls()) != 0 || out.Len() != 0 {
		t.Fatalf("err=%v output=%s", err, &out)
	}
}

func TestTrivyTransformBoundary_RefusalPreservesDestinations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		transform func(appsecurity.TransformInput) (int, error)
	}{
		{"dependency", appsecurity.TrivyToGitLabDep},
		{"container", appsecurity.TrivyToGitLabContainer},
		{"sarif", func(in appsecurity.TransformInput) (int, error) { return 0, appsecurity.TrivyToSARIF(in) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, body := range []string{"null", "{", `{"Results":42}`, "{}", `{"unexpected":true}`, "missing"} {
				for _, seeded := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/seeded=%t", body, seeded), func(t *testing.T) {
						assertTrivyRefusal(t, tc.transform, body, seeded)
					})
				}
			}
		})
	}
}

func assertTrivyRefusal(t *testing.T, transform func(appsecurity.TransformInput) (int, error), body string, seeded bool) {
	t.Helper()
	fsys := testfs.NewReal(t)

	path := fsys.Path("input.json")
	if body != "missing" {
		fsys.WriteFile("input.json", []byte(body))
	}

	out := fsys.Path("output.json")
	if seeded {
		fsys.WriteFile("output.json", []byte("previous output"))
		require.NoError(t, os.Chmod(out, 0o600))
	}

	count, err := transform(appsecurity.TransformInput{InputPath: path, OutputPath: out})
	require.Zero(t, count)

	want, wantCode := errs.ErrMalformedInput, 65
	if body == "missing" {
		want, wantCode = errs.ErrMissingInput, 66

		require.ErrorIs(t, err, os.ErrNotExist)

		_, statErr := os.Stat(path)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	} else {
		require.Equal(t, []byte(body), fsys.ReadFile("input.json"))
	}

	require.ErrorIs(t, err, want)
	require.Equal(t, wantCode, int(errs.ExitCodeFromError(err)))

	if body == "{" {
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
	}

	if body == `{"Results":42}` {
		var cause *json.UnmarshalTypeError
		require.ErrorAs(t, err, &cause)
	}

	info, statErr := os.Stat(out)
	if seeded {
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		require.Equal(t, "previous output", string(fsys.ReadFile("output.json")))
	} else {
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
}
