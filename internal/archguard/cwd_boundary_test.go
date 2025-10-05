// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
)

func TestGuardWorkingDirectoryBoundary_CompiledTestsRunFromRoot(t *testing.T) {
	t.Parallel()

	binary, err := os.Executable()
	require.NoError(t, err)
	// Resolve the effective default before replacing HOME. getenv alone loses
	// the module cache when neither GOMODCACHE nor GOPATH was explicitly set.
	moduleCache := guardModuleCache(t, os.Environ())
	require.DirExists(t, moduleCache)
	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^Test(ExecAdaptersClassifyStartFailures|OnlyCompositionRootMintsOperatorCredentials|LayersDoNotNameRunContextVars|NoOutwardImports|ForgeAwareCommandsHaveALiveScenario)$") //nolint:gosec // self-owned test executable and fixed guard selection.
	root := t.TempDir()
	goRoot := runtime.GOROOT() //nolint:staticcheck // SA1019: use the build toolchain's Go, not a potentially different Go on PATH.
	cmd.Dir = reporoot.Path(t)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root,
		"GOROOT=" + goRoot, "GOMODCACHE=" + moduleCache,
		"GOCACHE=" + filepath.Join(root, "go-build"), "CGO_ENABLED=0", "GOENV=off",
		"GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local",
	}
	body, err := cmd.CombinedOutput()
	require.NoError(t, err, string(body))
	t.Logf("compiled guards passed from repository root with fresh HOME/GOCACHE and effective GOMODCACHE=%s", moduleCache)
}

func guardModuleCache(t *testing.T, env []string) string {
	t.Helper()

	goRoot := runtime.GOROOT()                                                                       //nolint:staticcheck // SA1019: use the build toolchain's Go, not a potentially different Go on PATH.
	cmd := exec.CommandContext(t.Context(), filepath.Join(goRoot, "bin", "go"), "env", "GOMODCACHE") //nolint:gosec // matching installed Go executable; fixed metadata-only command, not a product tool.
	cmd.Dir = reporoot.Path(t)
	cmd.Env = env
	cmd.Env = append(cmd.Env, "GOROOT="+goRoot, "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	body, err := cmd.CombinedOutput()
	require.NoError(t, err, string(body))
	cache := strings.TrimSpace(string(body))
	require.NotEmpty(t, cache)
	require.True(t, filepath.IsAbs(cache), "effective GOMODCACHE must be absolute: %q", cache)

	return cache
}

func TestGuardWorkingDirectoryBoundary_EffectiveModuleCache(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, tc := range []struct {
		name string
		env  []string
		want string
	}{
		{"default from HOME", nil, filepath.Join(root, "go", "pkg", "mod")},
		{"default from GOPATH", []string{"GOPATH=" + filepath.Join(root, "gopath")}, filepath.Join(root, "gopath", "pkg", "mod")},
		{"explicit module cache", []string{"GOMODCACHE=" + filepath.Join(root, "modules")}, filepath.Join(root, "modules")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No ambient GOMODCACHE, GOPATH, or persisted go env settings enter
			// these cases. go env derives paths without creating/downloading them.
			env := append([]string{"HOME=" + root, "TMPDIR=" + root, "GOENV=off"}, tc.env...)
			require.Equal(t, tc.want, guardModuleCache(t, env))
		})
	}
}
