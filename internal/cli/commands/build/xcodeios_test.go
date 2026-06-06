// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"strings"
	"testing"

	buildcmd "github.com/diggsweden/reusable-ci/internal/cli/commands/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestXcodeIOSVersionInfoCmd_WritesOutputs(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	project := fsys.WriteFile("MyApp.xcodeproj/project.pbxproj", []byte("MARKETING_VERSION = 2.1.0;\nCURRENT_PROJECT_VERSION = 15;\n"))

	cmd := buildcmd.New()
	if err := cmd.Run(context.Background(), []string{"build", "xcode-ios", "version-info", strings.TrimSuffix(project, "/project.pbxproj")}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("version"); got != "2.1.0" {
		t.Errorf("version = %q", got)
	}

	if got := env.Output("build"); got != "15" {
		t.Errorf("build = %q", got)
	}
}

func TestXcodeIOSExportIPACmd_ReportsCustomMissingVarName(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("EXPORT_OPTIONS_BASE64", "")
	env.Setenv("EXPORT_OPTIONS_VAR", "IOS_EXPORT_OPTIONS")

	cmd := buildcmd.New()

	err := cmd.Run(context.Background(), []string{"build", "xcode-ios", "export-ipa"})
	if err == nil || !strings.Contains(err.Error(), "export options not found in variable IOS_EXPORT_OPTIONS") {
		t.Errorf("err = %v", err)
	}
}
