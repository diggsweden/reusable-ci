// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestCacheDiscriminator covers the cache-key discriminator, which had no
// test. It decides whether two setup-toolchain runs share a cache, so a
// collision serves one run's tool set to another.
func TestCacheDiscriminator(t *testing.T) {
	t.Parallel()

	base := toolchain.CacheDiscriminatorInput{Tools: "go,node", InstallDevTools: "true", ExtraCachePaths: "~/.cargo"}

	got := toolchain.CacheDiscriminator(base)
	if len(got) != 16 {
		t.Errorf("discriminator = %q, want 16 characters", got)
	}

	// Stable across calls: the key has to be the same on the restore run
	// as it was on the save run.
	if again := toolchain.CacheDiscriminator(base); again != got {
		t.Errorf("not stable: %q then %q", got, again)
	}

	// Every input participates. If one did not, two runs differing only
	// in that field would share a cache.
	for _, tc := range []struct {
		name   string
		mutate func(*toolchain.CacheDiscriminatorInput)
	}{
		{name: "tools", mutate: func(in *toolchain.CacheDiscriminatorInput) { in.Tools = "go" }},
		{name: "dev tools", mutate: func(in *toolchain.CacheDiscriminatorInput) { in.InstallDevTools = "false" }},
		{name: "extra paths", mutate: func(in *toolchain.CacheDiscriminatorInput) { in.ExtraCachePaths = "~/.m2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := base
			tc.mutate(&in)

			if toolchain.CacheDiscriminator(in) == got {
				t.Errorf("changing %s did not change the discriminator", tc.name)
			}
		})
	}
}

// TestSetupMiseEnv_IsolatesMutableTreesWhenCacheIsOff covers the reason
// the cache flag exists. With caching off, mise's data, cache and state
// trees are redirected under the runner temp directory so a run does not
// read or write the shared trees under $HOME.
//
// The test sets HOME itself, because the code under test creates
// ~/.local/share/mise and would otherwise touch the developer's own.
func TestSetupMiseEnv_IsolatesMutableTreesWhenCacheIsOff(t *testing.T) {
	// No t.Parallel(): mutates HOME via t.Setenv.
	home := t.TempDir()
	t.Setenv("HOME", home)

	runnerTemp := t.TempDir()
	dir := t.TempDir()
	pathFile := filepath.Join(dir, "path")
	envFile := filepath.Join(dir, "env")

	if err := toolchain.SetupMiseEnv(toolchain.SetupMiseEnvInput{
		Cache:      "false",
		BinHome:    filepath.Join(dir, "bin"),
		PathFile:   pathFile,
		EnvFile:    envFile,
		RunnerTemp: runnerTemp,
	}); err != nil {
		t.Fatal(err)
	}

	env := string(readFileOrFail(t, envFile))

	// All three mutable trees point inside the runner temp dir.
	for _, key := range []string{"MISE_DATA_DIR", "MISE_CACHE_DIR", "MISE_STATE_DIR"} {
		value := envFileValue(t, env, key)
		if value == "" {
			t.Errorf("%s not written to the env file:\n%s", key, env)

			continue
		}

		if !strings.HasPrefix(value, runnerTemp) {
			t.Errorf("%s = %q, want it under the runner temp dir %q", key, value, runnerTemp)
		}

		if _, err := os.Stat(value); err != nil {
			t.Errorf("%s directory was not created: %v", key, err)
		}
	}

	// The config dir is isolated in both modes.
	if cfg := envFileValue(t, env, "MISE_CONFIG_DIR"); !strings.HasPrefix(cfg, runnerTemp) {
		t.Errorf("MISE_CONFIG_DIR = %q, want it under the runner temp dir", cfg)
	}
}

// TestSetupMiseEnv_CachedModeLeavesTheSharedTreesAlone is the other side:
// with caching on, the mutable trees are deliberately NOT redirected, so
// the runner's cache is what mise reads and writes.
func TestSetupMiseEnv_CachedModeLeavesTheSharedTreesAlone(t *testing.T) {
	// No t.Parallel(): mutates HOME via t.Setenv.
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")

	if err := toolchain.SetupMiseEnv(toolchain.SetupMiseEnvInput{
		Cache:      "true",
		BinHome:    filepath.Join(dir, "bin"),
		PathFile:   filepath.Join(dir, "path"),
		EnvFile:    envFile,
		RunnerTemp: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	env := string(readFileOrFail(t, envFile))
	for _, key := range []string{"MISE_DATA_DIR", "MISE_CACHE_DIR", "MISE_STATE_DIR"} {
		if strings.Contains(env, key) {
			t.Errorf("%s was redirected despite caching being enabled:\n%s", key, env)
		}
	}
}

func TestSetupMiseEnv_Refusals(t *testing.T) {
	// No t.Parallel(): mutates HOME via t.Setenv.
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	valid := toolchain.SetupMiseEnvInput{
		Cache: "true", BinHome: filepath.Join(dir, "bin"),
		PathFile: filepath.Join(dir, "path"), EnvFile: filepath.Join(dir, "env"),
		RunnerTemp: dir,
	}

	for _, tc := range []struct {
		name   string
		mutate func(*toolchain.SetupMiseEnvInput)
	}{
		// Neither "" nor a recognised value: anything else is refused
		// rather than guessed, because guessing wrong either leaks a
		// shared cache into an isolated run or discards a wanted one.
		{name: "unknown cache value", mutate: func(in *toolchain.SetupMiseEnvInput) { in.Cache = "False" }},
		{name: "numeric cache value", mutate: func(in *toolchain.SetupMiseEnvInput) { in.Cache = "0" }},
		{name: "no path file", mutate: func(in *toolchain.SetupMiseEnvInput) { in.PathFile = "" }},
		{name: "no env file", mutate: func(in *toolchain.SetupMiseEnvInput) { in.EnvFile = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mutate(&in)

			if err := toolchain.SetupMiseEnv(in); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage", err)
			}
		})
	}
}

func readFileOrFail(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test-owned path.
	if err != nil {
		t.Fatal(err)
	}

	return body
}

// envFileValue returns the value of key from a runner env *file* body.
// The existing envValue helper takes an already-split []string.
func envFileValue(t *testing.T, body, key string) string {
	t.Helper()

	for _, line := range strings.Split(body, "\n") {
		if after, ok := strings.CutPrefix(line, key+"="); ok {
			return after
		}
	}

	return ""
}
