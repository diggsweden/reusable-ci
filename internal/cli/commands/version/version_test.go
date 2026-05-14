// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version_test

import (
	"context"
	"strings"
	"testing"

	versioncmd "github.com/diggsweden/reusable-ci/internal/cli/commands/version"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestBumpCmd_RequiresProjectTypeAndVersion(t *testing.T) {
	t.Parallel()
	cmd := versioncmd.New()
	err := cmd.Run(context.Background(), []string{"version", "bump"})
	if err == nil || !strings.Contains(err.Error(), "Usage: bump") {
		t.Errorf("err = %v", err)
	}
}

func TestBumpCmd_XcodePositionalVersionFileIsIgnoredLikeBash(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("custom.xcconfig", []byte("MARKETING_VERSION = 0.5.0\n"))

	cmd := versioncmd.New()
	err := cmd.Run(context.Background(), []string{"version", "bump", "xcode-ios", "1.0.0", dir, "custom.xcconfig"})
	if err != nil {
		t.Fatal(err)
	}
	body := fsys.ReadFile("versions.xcconfig")
	if string(body) != "MARKETING_VERSION = 1.0.0\n" {
		t.Errorf("versions.xcconfig = %q", body)
	}
	customBody := fsys.ReadFile("custom.xcconfig")
	if !strings.Contains(string(customBody), "0.5.0") {
		t.Errorf("custom.xcconfig should be unchanged, got %q", customBody)
	}
}

func TestBumpCmd_GradlePositionalVersionFileIsUsed(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("custom.properties", []byte("version=0.1.0\n"))

	cmd := versioncmd.New()
	err := cmd.Run(context.Background(), []string{"version", "bump", "gradle", "2.0.0", dir, "custom.properties"})
	if err != nil {
		t.Fatal(err)
	}
	body := fsys.ReadFile("custom.properties")
	if !strings.Contains(string(body), "version=2.0.0") {
		t.Errorf("custom.properties = %q", body)
	}
}
