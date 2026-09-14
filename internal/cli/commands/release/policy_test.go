// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	releasecmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestPublishCmd_StrategiesPreserveReleasePolicy(t *testing.T) {
	for _, strategy := range []string{"reconcile", "recreate"} {
		for _, tag := range []string{"v1.2.3", "v1.2.3-rc.1"} {
			for _, draft := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/draft=%t", strategy, tag, draft), func(t *testing.T) {
					testenv.New(t)
					fsys := testfs.NewReal(t)
					fsys.Chdir()
					fsys.WriteFile("dist/release-notes.md", []byte("notes"))
					fsys.WriteFile("dist/app.tgz", []byte("asset"))

					log, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
					if err != nil {
						t.Fatal(err)
					}

					previous := os.Stderr
					os.Stderr = log

					t.Cleanup(func() { os.Stderr = previous; _ = log.Close() })

					args := []string{"release", "publish", "--dry-run", "--strategy", strategy, "--tag", tag, "--repository", "fixture/repo", fmt.Sprintf("--draft=%t", draft), "--release-dir", "dist", "--asset", "dist/app.tgz"}
					if runErr := releasecmd.New().Run(t.Context(), args); runErr != nil {
						t.Fatal(runErr)
					}

					body, err := os.ReadFile(log.Name())
					if err != nil {
						t.Fatal(err)
					}

					want := fmt.Sprintf("draft=%t, prerelease=%t", draft, tag == "v1.2.3-rc.1")
					if !strings.Contains(string(body), want) {
						t.Errorf("missing %q in provider preview: %s", want, body)
					}
				})
			}
		}
	}
}
