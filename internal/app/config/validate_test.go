// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestValidate_HappyPath(t *testing.T) {
	t.Parallel()

	path := testfs.NewReal(t).WriteFile("artifacts.yml", []byte(`
artifacts:
  - name: my-app
    project-type: maven
`))

	var warnings bytes.Buffer
	if err := appconfig.Validate(path, &warnings); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if warnings.Len() != 0 {
		t.Errorf("unexpected warnings: %q", warnings.String())
	}
}

func TestValidate_FileNotFound(t *testing.T) {
	t.Parallel()

	err := appconfig.Validate("/does/not/exist.yml", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate_PropagatesValidationErrors(t *testing.T) {
	t.Parallel()

	path := testfs.NewReal(t).WriteFile("artifacts.yml", []byte(`
artifacts:
  - name: bad
    project-type: rust
`))
	err := appconfig.Validate(path, nil)

	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("err = %v, want ValidationError", err)
	}
}

func TestValidate_EmitsWarnings(t *testing.T) {
	t.Parallel()

	path := testfs.NewReal(t).WriteFile("artifacts.yml", []byte(`
artifacts:
  - name: app
    project-type: maven
    build-type: application
    publish-to: [forge-packages]
`))

	var warnings bytes.Buffer
	if err := appconfig.Validate(path, &warnings); err != nil {
		t.Errorf("Validate: %v", err)
	}

	if !strings.Contains(warnings.String(), "Maven application") {
		t.Errorf("expected maven-app warning, got: %q", warnings.String())
	}
}
