// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestTrivyTransforms_DestinationPolicy states the path policy of the three
// report transforms for a valid input. The destination is a caller-chosen
// path, not confined to a workspace: an absent file in an existing directory is
// created, and an existing regular file is rewritten in place (a read-only one
// is refused, see TestTrivyToGitLab_ReadOnlyOutputPreservesCauseAndBytes). A
// linked, directory, parent-missing or separator-terminated destination, and an
// output that is the input by name or by hard link, are refused before anything
// is written, with the input, any link target and any seeded bytes unchanged.
func TestTrivyTransforms_DestinationPolicy(t *testing.T) {
	transforms := map[string]func(appsecurity.TransformInput) error{
		"dependency": func(in appsecurity.TransformInput) error {
			_, err := appsecurity.TrivyToGitLabDep(in)

			return err
		},
		"container": func(in appsecurity.TransformInput) error {
			_, err := appsecurity.TrivyToGitLabContainer(in)

			return err
		},
		"sarif": appsecurity.TrivyToSARIF,
	}

	for name, transform := range transforms {
		for _, tc := range []struct {
			shape string
			want  error
		}{
			{"absent", nil},
			{"existing file", nil},
			{"symlink", errs.ErrValidation},
			{"directory", errs.ErrValidation},
			{"missing parent", errs.ErrMissingInput},
			{"trailing separator", errs.ErrUsage},
			{"same as input", errs.ErrUsage},
			{"hard link to input", errs.ErrUsage},
		} {
			t.Run(name+"/"+tc.shape, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				input := fsys.WriteFile("trivy.json", []byte(sampleTrivyJSON))
				target := fsys.WriteFile("elsewhere.json", []byte("link target"))
				out := fsys.Path("report.json")

				switch tc.shape {
				case "existing file":
					fsys.WriteFile("report.json", []byte("previous report"))
				case "symlink":
					require.NoError(t, os.Symlink(target, out))
				case "directory":
					fsys.MkdirAll("report.json")
				case "missing parent":
					out = fsys.Path("absent", "report.json")
				case "trailing separator":
					out = fsys.Path("reports") + string(filepath.Separator)
				case "same as input":
					out = input
				case "hard link to input":
					require.NoError(t, os.Link(input, out))
				}

				err := transform(appsecurity.TransformInput{InputPath: input, OutputPath: out})

				require.True(t, bytes.Equal([]byte(sampleTrivyJSON), fsys.ReadFile("trivy.json")), "input changed")
				require.Equal(t, "link target", string(fsys.ReadFile("elsewhere.json")), "link target changed")

				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)

					if tc.shape == "existing file" {
						require.Equal(t, "previous report", string(fsys.ReadFile("report.json")))
					}

					return
				}

				require.NoError(t, err)

				var report map[string]any
				require.NoError(t, json.Unmarshal(fsys.ReadFile("report.json"), &report), "destination is not the rendered report")
				require.NotEmpty(t, report)
			})
		}
	}
}
