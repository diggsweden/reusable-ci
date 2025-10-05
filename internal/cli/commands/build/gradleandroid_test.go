// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"testing"

	buildcmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestGradleAndroidVersionInfoCmd_WritesOutputs(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("gradle.properties", []byte("versionName=2.5.0\nversionCode=42\n"))

	cmd := buildcmd.New()
	if err := cmd.Run(context.Background(), []string{"build", "gradle-android", "metadata"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("version"); got != "2.5.0" {
		t.Errorf("version = %q", got)
	}

	if got := env.Output("version-code"); got != "42" {
		t.Errorf("version-code = %q", got)
	}
}
