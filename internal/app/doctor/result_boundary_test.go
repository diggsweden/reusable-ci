// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/doctor"
)

func TestDoctorResultsBoundary_ConfigIndependentChecks(t *testing.T) { //nolint:gocognit // four real config outcomes assert their complete ordered result sets.
	t.Parallel()

	for _, kind := range []string{"valid", "missing", "malformed", "derived"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".github/workflows"), 0o700); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(root, ".github/workflows/release.yml"), []byte("jobs:\n  release:\n    uses: org/engine/.github/workflows/release.yml@main\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			in := doctor.Input{Root: root, RepoSlug: "org/engine"}
			want := []string{"artifacts.yml present", "artifacts.yml parses", "workflows pin reusable-ci to a tag"}

			switch kind {
			case "missing":
				in.ArtifactsPath = filepath.Join(root, "missing.yml")
			case "valid", "malformed":
				in.ArtifactsPath = filepath.Join(root, "artifacts.yml")

				body := "artifacts:\n  - name: fixture\n    project-type: meta\n"
				if kind == "malformed" {
					body = "artifacts: ["
				}

				if err := os.WriteFile(in.ArtifactsPath, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			case "derived":
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/org/fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if kind == "valid" {
				want = []string{"artifacts.yml present", "artifacts.yml parses + validates", "workflows pin reusable-ci to a tag", "sign block valid", "release-authorization allowlist (n/a)", "workflow id-token permission (n/a)"}
			}

			if kind == "derived" {
				want = []string{"artifacts.yml present", "workflows pin reusable-ci to a tag", "sign block valid", "release-authorization allowlist (n/a)", "workflow id-token permission (n/a)"}
			}

			checks, err := doctor.Run(in)
			if err != nil {
				t.Fatal(err)
			}

			names := make([]string, 0, len(checks))
			for _, check := range checks {
				names = append(names, check.Name)
			}

			if !reflect.DeepEqual(names, want) {
				t.Fatalf("names=%v want=%v", names, want)
			}

			pin := findCheck(checks, "workflows pin reusable-ci to a tag")
			if pin == nil || pin.Severity != doctor.SeverityWarn {
				t.Fatalf("independent pin check=%+v", pin)
			}
		})
	}
}
