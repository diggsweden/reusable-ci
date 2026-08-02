// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

func TestParseExternalParametersJSON(t *testing.T) {
	t.Parallel()

	t.Run("empty means no extras", func(t *testing.T) {
		t.Parallel()

		for _, raw := range []string{"", "   ", "\n"} {
			got, err := provenance.ParseExternalParametersJSON(raw)
			if err != nil || got != nil {
				t.Errorf("ParseExternalParametersJSON(%q) = %v, %v; want nil, nil", raw, got, err)
			}
		}
	})

	t.Run("object parses", func(t *testing.T) {
		t.Parallel()

		got, err := provenance.ParseExternalParametersJSON(`{"base_input_set":"abc123","build_group":"core"}`)
		if err != nil {
			t.Fatal(err)
		}

		want := map[string]any{"base_input_set": "abc123", "build_group": "core"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("invalid JSON and non-objects are malformed input", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]string{
			"truncated": `{"a":`,
			"array":     `[1,2]`,
			"string":    `"x"`,
			"number":    `42`,
			"null":      `null`,
		} {
			if _, err := provenance.ParseExternalParametersJSON(raw); !errors.Is(err, errs.ErrMalformedInput) {
				t.Errorf("%s: err = %v, want ErrMalformedInput", name, err)
			}
		}
	})
}

func TestMergeExternalParameters(t *testing.T) {
	t.Parallel()

	t.Run("extras land without touching computed keys", func(t *testing.T) {
		t.Parallel()

		ext := map[string]any{"source": "git+https://example.org/o/r"}
		if err := provenance.MergeExternalParameters(ext, map[string]any{"build_group": "core"}); err != nil {
			t.Fatal(err)
		}

		want := map[string]any{"source": "git+https://example.org/o/r", "build_group": "core"}
		if !reflect.DeepEqual(ext, want) {
			t.Errorf("ext = %#v, want %#v", ext, want)
		}
	})

	t.Run("already-present keys are reserved", func(t *testing.T) {
		t.Parallel()

		ext := map[string]any{"source": "computed"}

		err := provenance.MergeExternalParameters(ext, map[string]any{"source": "shadowed"})
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}

		if ext["source"] != "computed" {
			t.Errorf("computed value was overridden: %#v", ext)
		}
	})
}

// TestBuild_ExternalParametersExtras pins the generic extras path on the
// statement builder: declared parameters land in externalParameters, and
// a key the engine computes (source, ref, image, …) is reserved.
func TestBuild_ExternalParametersExtras(t *testing.T) {
	t.Parallel()

	base := provenance.Input{
		Subjects:     []provenance.Subject{{Name: "dist/app.tar.gz", SHA256: "abc"}},
		BuildType:    provenance.ReleaseBuildType,
		BuilderID:    "https://example.org/o/r/release.yml@v1",
		SourceURI:    "git+https://example.org/o/r",
		Ref:          "v1.2.3",
		InvocationID: "https://example.org/o/r/actions/runs/1",
	}

	t.Run("extras land in externalParameters", func(t *testing.T) {
		t.Parallel()

		in := base
		in.ExternalParameters = map[string]any{"base_input_set": "abc123", "build_group": "core"}

		stmt, err := provenance.Build(in)
		if err != nil {
			t.Fatal(err)
		}

		body, err := stmt.JSON()
		if err != nil {
			t.Fatal(err)
		}

		var decoded struct {
			Predicate struct {
				BuildDefinition struct {
					ExternalParameters map[string]any `json:"externalParameters"`
				} `json:"buildDefinition"`
			} `json:"predicate"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}

		ext := decoded.Predicate.BuildDefinition.ExternalParameters
		if ext["base_input_set"] != "abc123" || ext["build_group"] != "core" {
			t.Errorf("extras missing from externalParameters: %#v", ext)
		}

		if ext["source"] != "git+https://example.org/o/r" || ext["ref"] != "v1.2.3" {
			t.Errorf("computed parameters disturbed: %#v", ext)
		}
	})

	t.Run("computed-key collision fails", func(t *testing.T) {
		t.Parallel()

		in := base
		in.ExternalParameters = map[string]any{"source": "shadowed"}

		if _, err := provenance.Build(in); !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation for reserved-key collision", err)
		}
	})

	t.Run("predicate path enforces the same rule", func(t *testing.T) {
		t.Parallel()

		in := base
		in.Subjects = nil
		in.ImageName = "ghcr.io/o/app"
		in.ExternalParameters = map[string]any{"image": "shadowed"}

		if _, err := provenance.Predicate(in); !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation for reserved-key collision", err)
		}
	})
}
