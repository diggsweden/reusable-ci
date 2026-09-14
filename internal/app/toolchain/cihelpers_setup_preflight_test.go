// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	setupCacheOn   = "true"
	setupCacheOff  = "false"
	runnerPathName = "PATH"
	miseCacheKey   = "MISE_CACHE_DIR"
	miseConfigKey  = "MISE_CONFIG_DIR"
	miseDataKey    = "MISE_DATA_DIR"
	miseStateKey   = "MISE_STATE_DIR"
)

func TestSetupMiseEnv_LocalRefusalHasNoEffects(t *testing.T) { //nolint:gocognit // each obstacle shares a complete owned-tree snapshot.
	// Sequential: HOME and cwd are process-wide.
	for _, scenario := range []string{
		"ENV directory", "ENV missing parent", "ENV symlink", "PATH symlink", "linked parent",
		"same path", "normalized alias", "hardlink alias", "same planned file", "PATH ancestor", "ENV planned directory",
		"bin separator", "raw bin separator", "home separator", "temp newline", "temp NUL",
		"state obstacle", "linked state", "bin obstacle", "data home obstacle",
	} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogFixtureRoot(t)
			t.Chdir(root)
			home := filepath.Join(root, "home")
			t.Setenv("HOME", home)

			in := SetupMiseEnvInput{Cache: setupCacheOff, BinHome: filepath.Join(root, "new", "bin"),
				PathFile: filepath.Join(root, runnerPathName), EnvFile: filepath.Join(root, "ENV"), RunnerTemp: filepath.Join(root, "temp")}
			writeChangelogCanary(t, in.PathFile, "prior PATH\n", 0o640)
			writeChangelogCanary(t, in.EnvFile, "PRIOR=raw:value\n", 0o600)

			wantErr, reason := errs.ErrValidation, "invalid mise local path"

			switch scenario {
			case "ENV directory":
				in.EnvFile = filepath.Join(root, "env-directory")
				if err := os.Mkdir(in.EnvFile, 0o700); err != nil {
					t.Fatal(err)
				}

				reason = "nonlinked regular file"
			case "ENV missing parent":
				in.EnvFile = filepath.Join(root, "unplanned", "ENV")
				wantErr, reason = os.ErrNotExist, "open runner/install parent"
			case "ENV symlink", "PATH symlink", "linked parent":
				reason = "nonlinked regular file"
				alias := filepath.Join(root, "alias")

				target := in.EnvFile
				if scenario == "linked parent" {
					target = root
				}

				changelogSymlink(t, target, alias)
				in.EnvFile = alias

				switch scenario {
				case "PATH symlink":
					in.PathFile, in.EnvFile = alias, target
				case "linked parent":
					in.EnvFile = filepath.Join(alias, "ENV")
					reason = "open runner/install parent"
				}
			case "same path":
				in.EnvFile, reason = in.PathFile, "files alias"
			case "normalized alias":
				in.EnvFile, reason = "./PATH", "files alias"
			case "hardlink alias":
				reason = "files alias"

				in.EnvFile = filepath.Join(root, "hardlink")
				if err := os.Link(in.PathFile, in.EnvFile); err != nil {
					t.Fatal(err)
				}
			case "same planned file":
				in.PathFile = filepath.Join(in.BinHome, "exports")
				in.EnvFile, reason = in.PathFile, "files alias"
			case "PATH ancestor":
				in.BinHome = filepath.Join(in.PathFile, "bin")
				reason = "inspect planned mise directory"
			case "ENV planned directory":
				in.EnvFile = filepath.Join(in.RunnerTemp, "setup-toolchain-mise-data", "state")
				reason = "overlaps planned directory"
			case "bin separator":
				in.BinHome += ":other"
			case "raw bin separator":
				in.BinHome = root + "/bad:part/../bin"
			case "home separator":
				t.Setenv("HOME", home+":other")
			case "temp newline":
				in.RunnerTemp += "\nINJECTED=value"
			case "temp NUL":
				in.RunnerTemp += "\x00"
			case "state obstacle", "linked state":
				reason = "inspect planned mise directory"
				state := filepath.Join(in.RunnerTemp, "setup-toolchain-mise-data", "state")
				writeChangelogCanary(t, filepath.Join(filepath.Dir(state), "prior"), "keep\n", 0o600)

				if scenario == "linked state" {
					changelogSymlink(t, root, state)
				} else {
					writeChangelogCanary(t, state, "state obstacle\n", 0o600)
				}
			case "bin obstacle":
				reason = "inspect planned mise directory"

				writeChangelogCanary(t, in.BinHome, "not a directory\n", 0o600)
			case "data home obstacle":
				reason = "inspect planned mise directory"

				writeChangelogCanary(t, filepath.Join(home, ".local", "share"), "not a directory\n", 0o600)
			}

			unchanged := changelogUnchanged(t, root)

			err := SetupMiseEnv(in)
			if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), reason) {
				t.Errorf("setup refusal = %v, want %v containing %q", err, wantErr, reason)
			}

			unchanged()
		})
	}
}

func TestSetupMiseEnv_ExactExportsAndAppendOnlyReruns(t *testing.T) { //nolint:gocognit // all modes and planned parents share exact byte/mode/identity assertions.
	for _, cache := range []string{"", setupCacheOn, setupCacheOff} {
		for _, planned := range []bool{false, true} {
			t.Run(cache+"/planned="+map[bool]string{false: setupCacheOff, true: setupCacheOn}[planned], func(t *testing.T) {
				root := changelogFixtureRoot(t)
				home := filepath.Join(root, "home space \u00e5")
				t.Setenv("HOME", home)

				in := SetupMiseEnvInput{Cache: cache, BinHome: filepath.Join(root, "bin space \u00e5"),
					PathFile: filepath.Join(root, runnerPathName), EnvFile: filepath.Join(root, "ENV"), RunnerTemp: filepath.Join(root, "temp:raw=value \u00e5")}
				config := filepath.Join(in.RunnerTemp, "setup-toolchain-mise-config")
				prefixPath, prefixEnv := "prior PATH\n", "PRIOR=raw:value\n"
				pathMode, envMode := os.FileMode(0o640), os.FileMode(0o600)

				if planned {
					in.PathFile, in.EnvFile = filepath.Join(in.BinHome, runnerPathName), filepath.Join(config, "ENV")
					prefixPath, prefixEnv, pathMode, envMode = "", "", 0o644, 0o644
				} else {
					writeChangelogCanary(t, in.PathFile, prefixPath, pathMode)
					writeChangelogCanary(t, in.EnvFile, prefixEnv, envMode)
				}

				pathLines := in.BinHome + "\n" + filepath.Join(home, ".cargo", "bin") + "\n"

				var envLines strings.Builder
				envLines.WriteString("MISE_CONFIG_DIR=" + config + "\n")

				dirs := []string{in.BinHome, filepath.Join(home, ".local", "share", "mise"), config}
				if cache == setupCacheOff {
					base := filepath.Join(in.RunnerTemp, "setup-toolchain-mise-data")
					for _, pair := range [][2]string{{miseDataKey, dataSubdir}, {miseCacheKey, cacheSubdir}, {miseStateKey, stateSubdir}} {
						dir := filepath.Join(base, pair[1])
						dirs = append(dirs, dir)
						envLines.WriteString(pair[0] + "=" + dir + "\n")
					}
				}

				var priorPath, priorEnv os.FileInfo

				for count := 1; count <= 2; count++ {
					if err := SetupMiseEnv(in); err != nil {
						t.Fatal(err)
					}

					assertChangelogFile(t, in.PathFile, prefixPath+strings.Repeat(pathLines, count), pathMode)
					assertChangelogFile(t, in.EnvFile, prefixEnv+strings.Repeat(envLines.String(), count), envMode)
					currentPath, _ := os.Stat(in.PathFile)

					currentEnv, _ := os.Stat(in.EnvFile)
					if priorPath != nil && (!os.SameFile(priorPath, currentPath) || !os.SameFile(priorEnv, currentEnv)) {
						t.Error("rerun replaced a runner file")
					}

					priorPath, priorEnv = currentPath, currentEnv
				}

				for _, dir := range dirs {
					if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
						t.Errorf("planned directory %s: %v", dir, err)
					}
				}

				if cache != setupCacheOff {
					assertChangelogAbsent(t, filepath.Join(in.RunnerTemp, "setup-toolchain-mise-data"))
				}
			})
		}
	}
}

func assertChangelogFile(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil || string(body) != want {
		t.Errorf("file %s = %q, want %q: %v", path, body, want, err)
	}

	if info, statErr := os.Lstat(path); statErr != nil || info.Mode() != mode {
		t.Errorf("file %s mode, want %v: %v", path, mode, statErr)
	}
}

func TestSetupMiseEnv_UsesCanonicalOSTempFallback(t *testing.T) {
	for _, scenario := range []string{"direct", "alias"} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogFixtureRoot(t)

			temp := filepath.Join(root, "os-temp")
			if scenario == "alias" {
				alias := filepath.Join(root, "temp-alias")
				changelogSymlink(t, temp, alias)

				for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
					t.Setenv(key, alias)
				}
			}

			in := SetupMiseEnvInput{Cache: setupCacheOff, PathFile: filepath.Join(root, runnerPathName), EnvFile: filepath.Join(root, "ENV")}
			if err := SetupMiseEnv(in); err != nil {
				t.Fatalf("OS temp fallback setup failed: %v", err)
			}

			config := filepath.Join(temp, "setup-toolchain-mise-config")
			base := filepath.Join(temp, "setup-toolchain-mise-data")
			assertChangelogFile(t, in.EnvFile, "MISE_CONFIG_DIR="+config+"\nMISE_DATA_DIR="+filepath.Join(base, dataSubdir)+
				"\nMISE_CACHE_DIR="+filepath.Join(base, cacheSubdir)+"\nMISE_STATE_DIR="+filepath.Join(base, stateSubdir)+"\n", 0o644)

			for _, dir := range []string{config, filepath.Join(base, dataSubdir), filepath.Join(base, cacheSubdir), filepath.Join(base, stateSubdir)} {
				if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
					t.Errorf("OS temp fallback directory %s: %v", dir, err)
				}
			}
		})
	}
}

func assertChangelogAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected state at %s: %v", path, err)
	}
}
