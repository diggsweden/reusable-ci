// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	domainconfig "github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type failingExpansionWriter struct{}

func (failingExpansionWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSBOMExpansionBoundary_FormatsWritersAndRefusal(t *testing.T) { //nolint:gocognit // both CI formats independently cover empty/full layers, serializers and writer failures.
	t.Parallel()

	for _, mode := range []output.Format{output.FormatGitHub, output.FormatGitLab} {
		for _, value := range []string{"all", "none"} {
			for _, format := range []appconfig.ExpandSBOMsFormat{appconfig.ExpandSBOMsFormatJSON, appconfig.ExpandSBOMsFormatComma, "xml"} {
				sink := fakeoutputsink.New(t)
				if err := sink.Set(t.Context(), "prior", "keep"); err != nil {
					t.Fatal(err)
				}

				in := appconfig.ExpandSBOMsInput{Value: value, Format: format, Output: mode, Sink: sink}

				var out bytes.Buffer

				err := appconfig.ExpandSBOMs(t.Context(), &out, in)
				if format == "xml" {
					if !errors.Is(err, errs.ErrUsage) || len(sink.Keys()) != 1 || out.Len() != 0 {
						t.Fatalf("invalid format had effects: %v %v %s", err, sink.Keys(), &out)
					}

					continue
				}

				if err != nil {
					t.Fatal(err)
				}

				want := "build,analyzed-artifact,analyzed-container\n"
				if format == appconfig.ExpandSBOMsFormatJSON {
					want = "[\"build\",\"analyzed-artifact\",\"analyzed-container\"]\n"
				}

				if value == "none" {
					want = "\n"
					if format == appconfig.ExpandSBOMsFormatJSON {
						want = "[]\n"
					}
				}

				if out.String() != want {
					t.Fatalf("output=%q want=%q", out.String(), want)
				}

				in.Sink = fakeoutputsink.New(t)
				if err := appconfig.ExpandSBOMs(t.Context(), failingExpansionWriter{}, in); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("writer error lost: %v", err)
				}
			}
		}
	}
}

func TestArtifactValidationBoundary_BuildTypeWireMatchesSchema(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)
	for _, tc := range []struct {
		name       string
		fields     string
		valid      bool
		buildTypes []string
	}{
		{"omitted", "", true, []string{""}},
		{"application", "    build-type: application\n", true, []string{"application"}},
		{"library", "    build-type: library\n", true, []string{"library"}},
		{"quoted_application", "    build-type: 'application'\n", true, []string{"application"}},
		{"quoted_library", "    build-type: \"library\"\n", true, []string{"library"}},
		{"empty_double_quotes", "    build-type: \"\"\n", false, nil},
		{"empty_single_quotes", "    build-type: ''\n", false, nil},
		{"null", "    build-type: null\n", false, nil},
		{"tilde", "    build-type: ~\n", false, nil},
		{"bare_key", "    build-type:\n", false, nil},
		{"quoted_null", "    build-type: 'null'\n", false, nil},
		{"whitespace", "    build-type: ' '\n", false, nil},
		{"padded", "    build-type: ' application '\n", false, nil},
		{"case_application", "    build-type: Application\n", false, nil},
		{"case_library", "    build-type: LIBRARY\n", false, nil},
		{"short_app", "    build-type: app\n", false, nil},
		{"short_lib", "    build-type: lib\n", false, nil},
		{"unknown", "    build-type: banana\n", false, nil},
		{"boolean", "    build-type: true\n", false, nil},
		{"integer", "    build-type: 42\n", false, nil},
		{"float", "    build-type: 1.5\n", false, nil},
		{"sequence", "    build-type: [application]\n", false, nil},
		{"mapping", "    build-type: {kind: library}\n", false, nil},
		{"alias_library", "    build-type: &kind library\n  - name: later\n    project-type: maven\n    build-type: *kind\n", true, []string{"library", "library"}},
		{"alias_empty", "    config: {maven-profile: &kind ''}\n  - name: later\n    project-type: maven\n    build-type: *kind\n", false, nil},
		{"alias_null", "    <<: {build-type: &kind null}\n    build-type: *kind\n", false, nil},
		{"alias_boolean", "    require-authorization: &kind true\n    build-type: *kind\n", false, nil},
		{"merged_library", "    <<: {build-type: library}\n", true, []string{"library"}},
		{"merged_empty", "    <<: {build-type: ''}\n", false, nil},
		{"merged_null", "    <<: {build-type: null}\n", false, nil},
		{"override_merged_null", "    <<: {build-type: null}\n    build-type: application\n", true, []string{"application"}},
		{"override_before_merge", "    build-type: library\n    <<: {build-type: ''}\n", true, []string{"library"}},
		{"null_overrides_merge", "    <<: {build-type: library}\n    build-type: null\n", false, nil},
		{"empty_overrides_merge", "    build-type: ''\n    <<: {build-type: application}\n", false, nil},
		{"merge_sequence_first_wins", "    <<: [{build-type: library}, {build-type: null}]\n", true, []string{"library"}},
		{"merge_sequence_null_first", "    <<: [{build-type: null}, {build-type: library}]\n", false, nil},
		{"later_empty", "    build-type: library\n  - name: later\n    project-type: maven\n    build-type: ''\n", false, nil},
		{"later_null", "    build-type: application\n  - name: later\n    project-type: maven\n    build-type: ~\n", false, nil},
		{"later_bad_enum", "  - name: later\n    project-type: maven\n    build-type: banana\n", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := []byte("artifacts:\n  - name: fixture\n    project-type: maven\n" + tc.fields)
			// The schema sees the actual YAML instance, not the typed config:
			// decoding to Artifact would erase precisely the presence under test.
			schemaErr := schema.Validate(yamlToInstance(t, body))

			cfg, runtimeErr := domainconfig.Parse(body)
			if runtimeErr == nil {
				runtimeErr = domainconfig.Validate(cfg)
			}

			if !tc.valid {
				require.Error(t, schemaErr, "schema must reject this wire value")
				require.ErrorIs(t, runtimeErr, errs.ErrInvalidConfig, "runtime must reject this wire value")
				require.Contains(t, runtimeErr.Error(), "build-type")

				return
			}

			require.NoError(t, schemaErr, "schema must accept this wire value")
			require.NoError(t, runtimeErr, "runtime must accept this wire value")
			require.Len(t, cfg.Artifacts, len(tc.buildTypes))

			for i, want := range tc.buildTypes {
				require.Equal(t, want, string(cfg.Artifacts[i].BuildType))
			}
		})
	}
}

func TestArtifactValidationBoundary_RejectsInvalidSBOMs(t *testing.T) {
	t.Parallel()

	for _, sboms := range []string{"banana", "all,build", "build,,analyzed-artifact"} {
		path := writeYAML(t, fmt.Sprintf("artifacts:\n  - name: fixture\n    project-type: maven\n    sboms: %q\n", sboms))
		if err := appconfig.Validate(path, nil); !errors.Is(err, errs.ErrInvalidConfig) {
			t.Fatalf("sboms=%s err=%v", sboms, err)
		}
	}
}

func TestArtifactValidationBoundary_BuildTypeRefusedBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, value := range []string{`""`, "''", "null", "~", "", "banana", "app", "lib", "' library '", "Library", "true", "42", "[application]", "{kind: library}"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			// The valid first artifact would warn if validation ran effects early.
			body := "artifacts:\n  - name: first\n    project-type: maven\n    build-type: application\n    publish-to: [forge-packages]\n  - name: later\n    project-type: maven\n    build-type: " + value + "\n"
			path := writeYAML(t, body)
			before, err := os.Stat(path)
			require.NoError(t, err)

			warnings := bytes.NewBufferString("prior warnings\n")
			err = appconfig.Validate(path, warnings)
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), `"later"`)
			require.Contains(t, err.Error(), "build-type")
			require.Equal(t, "prior warnings\n", warnings.String())

			sink := fakeoutputsink.New(t)
			require.NoError(t, sink.Set(t.Context(), "config-plan-json", "prior plan"))

			summary := &fakeSummary{}
			require.NoError(t, summary.Append(t.Context(), "prior summary\n"))

			stderr := bytes.NewBufferString("prior stderr\n")
			err = appconfig.EmitConfigPlan(t.Context(), sink, summary, stderr,
				output.NewAnnotator(stderr, output.FormatGitHub), appconfig.EmitConfigPlanInput{Path: path})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), `"later"`)
			require.Contains(t, err.Error(), "build-type")
			require.Equal(t, []string{"config-plan-json"}, sink.Keys())
			require.Equal(t, map[string]string{"config-plan-json": "prior plan"}, sink.AllScalar())
			require.Nil(t, sink.Multiline("config-plan-json"))
			require.Zero(t, sink.CloseCount())
			require.Equal(t, "prior summary\n", summary.buf.String())
			require.Equal(t, "prior stderr\n", stderr.String())

			afterBody, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, body, string(afterBody))

			after, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, before.Mode(), after.Mode())

			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1, "refusal must not create derived files")
		})
	}
}

func TestArtifactValidationBoundary_BuildTypeDefaultsAndWarnings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		field    string
		wantWarn bool
	}{
		{"omitted", "", false},
		{"application", "    build-type: application\n", true},
		{"library", "    build-type: library\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeYAML(t, "artifacts:\n  - name: fixture\n    project-type: maven\n    publish-to: [forge-packages]\n"+tc.field)

			var warnings bytes.Buffer
			require.NoError(t, appconfig.Validate(path, &warnings))

			if tc.wantWarn {
				require.Contains(t, warnings.String(), "Maven application")
			} else {
				require.Empty(t, warnings.String())
			}
		})
	}
}
