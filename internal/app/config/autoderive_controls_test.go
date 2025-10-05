// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// TestAutoDerive_ManifestControls covers the auto-derived name and the
// refusals: both Gradle settings dialects and their precedence, a settings
// file without a name, a malformed pom, a nameless package.json, a directory
// that merely carries a manifest's name, and a manifest that exists only below
// the root. Each refusal keeps its class, so "write artifacts.yml" and "fix
// this file" stay distinguishable.
func TestAutoDerive_ManifestControls(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		files    fstest.MapFS
		wantName string
		wantType projecttype.Type
		wantErr  error
	}{
		"groovy settings": {
			files:    fstest.MapFS{"build.gradle": {Data: []byte("plugins {}")}, "settings.gradle": {Data: []byte("rootProject.name = 'groovy-app'\n")}},
			wantName: "groovy-app", wantType: projecttype.Gradle,
		},
		"kotlin settings": {
			files:    fstest.MapFS{"build.gradle.kts": {Data: []byte("plugins {}")}, "settings.gradle.kts": {Data: []byte("rootProject.name = \"kotlin-app\"\n")}},
			wantName: "kotlin-app", wantType: projecttype.Gradle,
		},
		"groovy settings win over kotlin": {
			files: fstest.MapFS{
				"build.gradle": {Data: []byte("plugins {}")}, "settings.gradle": {Data: []byte("rootProject.name = 'first'\n")},
				"settings.gradle.kts": {Data: []byte("rootProject.name = \"second\"\n")},
			},
			wantName: "first", wantType: projecttype.Gradle,
		},
		"kotlin names when groovy does not": {
			files: fstest.MapFS{
				"build.gradle": {Data: []byte("plugins {}")}, "settings.gradle": {Data: []byte("include ':app'\n")},
				"settings.gradle.kts": {Data: []byte("rootProject.name = \"named\"\n")},
			},
			wantName: "named", wantType: projecttype.Gradle,
		},
		"malformed pom":          {files: fstest.MapFS{"pom.xml": {Data: []byte("<project><artifactId>x</project>")}}, wantErr: errs.ErrInvalidConfig},
		"nameless package":       {files: fstest.MapFS{"package.json": {Data: []byte(`{"version":"1.0.0"}`)}}, wantErr: errs.ErrInvalidConfig},
		"directory named go.mod": {files: fstest.MapFS{"go.mod/readme": {Data: []byte("not a manifest")}}, wantErr: errs.ErrMissingInput},
		"manifest below root":    {files: fstest.MapFS{"services/api/go.mod": {Data: []byte("module example.com/api\n")}}, wantErr: errs.ErrMissingInput},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := appconfig.AutoDeriveConfig(tc.files, ".")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || cfg != nil {
					t.Fatalf("cfg = %+v, err = %v, want %v", cfg, err, tc.wantErr)
				}

				return
			}

			if err != nil || len(cfg.Artifacts) != 1 || cfg.Artifacts[0].Name != tc.wantName || cfg.Artifacts[0].ProjectType != tc.wantType {
				t.Fatalf("cfg = %+v, err = %v, want %s %s", cfg, err, tc.wantType, tc.wantName)
			}
		})
	}
}

// TestAutoDerive_GradleNameFallsBackOnlyWhenNoSettingsNameIt: with no
// settings name the root directory's basename is used, while a settings file
// that exists but cannot be read is an error rather than a silent rename.
func TestAutoDerive_GradleNameFallsBackOnlyWhenNoSettingsNameIt(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "directory-name")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "build.gradle"), []byte("plugins {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := appconfig.AutoDeriveConfig(nil, root)
	if err != nil || cfg.Artifacts[0].Name != "directory-name" {
		t.Fatalf("cfg = %+v, err = %v, want the directory name", cfg, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("file permissions do not restrict root")
	}

	settings := filepath.Join(root, "settings.gradle")
	if err := os.WriteFile(settings, []byte("rootProject.name = 'real-name'\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	if cfg, err := appconfig.AutoDeriveConfig(nil, root); !errors.Is(err, os.ErrPermission) || cfg != nil {
		t.Fatalf("unreadable settings: cfg = %+v, err = %v, want a permission error", cfg, err)
	}
}
