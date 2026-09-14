// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestXcodeIOSVersionInfoCmd_WritesOutputs(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	project := fsys.WriteFile("MyApp.xcodeproj/project.pbxproj", []byte("MARKETING_VERSION = 2.1.0;\nCURRENT_PROJECT_VERSION = 15;\n"))
	fsys.WriteFile("Other.xcodeproj/project.pbxproj", []byte("MARKETING_VERSION = 9.0.0;\nCURRENT_PROJECT_VERSION = 99;\n"))

	cmd := cli.New(cli.BuildInfo{Version: "test"})
	if err := cmd.Run(context.Background(), []string{"reusable-ci", "build", "xcode-ios", "metadata", "--project", strings.TrimSuffix(project, "/project.pbxproj")}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("version"); got != "2.1.0" {
		t.Errorf("version = %q", got)
	}

	if got := env.Output("build"); got != "15" {
		t.Errorf("build = %q", got)
	}

	err := cli.New(cli.BuildInfo{Version: "test"}).Run(t.Context(), []string{"reusable-ci", "build", "xcode-ios", "metadata", strings.TrimSuffix(project, "/project.pbxproj")})
	require.ErrorIs(t, err, errs.ErrUsage)
}
