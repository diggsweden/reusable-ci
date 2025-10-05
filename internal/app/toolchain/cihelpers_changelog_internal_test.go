// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The renderer binary names and the four mise subdirectory names appear
// throughout. goconst counts a string across the whole package, test
// files included, so leaving these as literals here would push the
// product file over the threshold without that file changing.
const (
	gitCliffBin  = "git-cliff"
	gitChglogBin = "git-chglog"
	cacheSubdir  = "cache"
	dataSubdir   = "data"
	configSubdir = "config"
	stateSubdir  = "state"
)

// TestChangelogRendererSelector_MapsOnlyAllowlistedPinnedBackends pins the backend allowlist. The selector
// returned here is handed to `mise install` verbatim, so the closed set is
// what stops a caller-supplied backend from naming an arbitrary aqua
// package to install.
func TestChangelogRendererSelector_MapsOnlyAllowlistedPinnedBackends(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		in              InstallChangelogRendererInput
		wantSelector    string
		wantBin         string
		wantVersion     string
		wantErrSentinel error
	}{
		{
			name:         gitChglogBin,
			in:           InstallChangelogRendererInput{Backend: gitChglogBin, GitChglogVersion: "0.15.4"},
			wantSelector: "aqua:git-chglog/git-chglog",
			wantBin:      gitChglogBin,
			wantVersion:  "0.15.4",
		},
		{
			name:         gitCliffBin,
			in:           InstallChangelogRendererInput{Backend: gitCliffBin, GitCliffVersion: "2.6.1"},
			wantSelector: "aqua:orhun/git-cliff",
			wantBin:      gitCliffBin,
			wantVersion:  "2.6.1",
		},
		{
			// The version is not defaulted: an unpinned renderer would
			// resolve to whatever aqua serves that day.
			name:            "git-cliff without a version",
			in:              InstallChangelogRendererInput{Backend: gitCliffBin},
			wantErrSentinel: errs.ErrUsage,
		},
		{
			name:            "git-chglog without a version",
			in:              InstallChangelogRendererInput{Backend: gitChglogBin, GitChglogVersion: "   "},
			wantErrSentinel: errs.ErrUsage,
		},
		{
			name:            "unknown backend",
			in:              InstallChangelogRendererInput{Backend: "changelogger"},
			wantErrSentinel: errs.ErrValidation,
		},
		{
			// Not a backend name but a selector: the allowlist is what
			// keeps it from being installed as written.
			name:            "a selector passed as the backend",
			in:              InstallChangelogRendererInput{Backend: "aqua:attacker/tool", GitCliffVersion: "2.6.1"},
			wantErrSentinel: errs.ErrValidation,
		},
		{
			name:            "empty backend",
			in:              InstallChangelogRendererInput{},
			wantErrSentinel: errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			selector, bin, version, err := changelogRendererSelector(tc.in)

			if tc.wantErrSentinel != nil {
				if !errors.Is(err, tc.wantErrSentinel) {
					t.Fatalf("err = %v, want %v", err, tc.wantErrSentinel)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if selector != tc.wantSelector || bin != tc.wantBin || version != tc.wantVersion {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					selector, bin, version, tc.wantSelector, tc.wantBin, tc.wantVersion)
			}
		})
	}
}

func TestValidateChangelogRendererInput_RequiresARunnerAndAPathFile(t *testing.T) {
	t.Parallel()

	if err := validateChangelogRendererInput(nil, InstallChangelogRendererInput{PathFile: "p"}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("nil runner: err = %v, want ErrUsage", err)
	}

	if err := validateChangelogRendererInput(plainRunner{}, InstallChangelogRendererInput{}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("no path file: err = %v, want ErrUsage", err)
	}

	if err := validateChangelogRendererInput(plainRunner{}, InstallChangelogRendererInput{PathFile: "p"}); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
}

// TestPrepareChangelogMiseEnv_RedirectsEveryMiseDirAndLeadsPATH covers the isolation the prepare tree exists
// for: the renderer install must not read or write the ambient mise trees,
// and must not inherit a MISE_* pointing at them.
func TestPrepareChangelogMiseEnv_RedirectsEveryMiseDirAndLeadsPATH(t *testing.T) {
	// No t.Parallel(): mutates the environment via t.Setenv.
	shared := t.TempDir()
	t.Setenv("MISE_DATA_DIR", shared)
	t.Setenv("MISE_CACHE_DIR", shared)
	t.Setenv("MISE_CONFIG_DIR", shared)
	t.Setenv("MISE_STATE_DIR", shared)

	runnerTemp := t.TempDir()
	binHome := filepath.Join(t.TempDir(), "bin")

	env, err := prepareChangelogMiseEnv(filepath.Join(runnerTemp, "reusable-ci-prepare-mise-42"), binHome)
	if err != nil {
		t.Fatal(err)
	}

	prepareDir := filepath.Join(runnerTemp, "reusable-ci-prepare-mise-42")

	for name, sub := range map[string]string{
		"MISE_CACHE_DIR":  cacheSubdir,
		"MISE_CONFIG_DIR": configSubdir,
		"MISE_DATA_DIR":   dataSubdir,
		"MISE_STATE_DIR":  stateSubdir,
	} {
		want := filepath.Join(prepareDir, sub)

		// Exactly one entry: an ambient value left in place alongside
		// the override would be the one a child process picks up,
		// depending on which the runtime reads last.
		if got := envEntryCount(env, name); got != 1 {
			t.Errorf("%s appears %d times in the env, want 1", name, got)
		}

		if got := envLookup(env, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}

		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Errorf("%s was not created as a directory: %v", want, err)
		}
	}

	// The freshly linked binary has to win over any same-named tool
	// already on PATH.
	if path := envLookup(env, "PATH"); !strings.HasPrefix(path, binHome+string(os.PathListSeparator)) {
		t.Errorf("PATH = %q, want it to start with the bin home %q", path, binHome)
	}
}

// TestPrepareChangelogMiseEnv_ClearsAPriorRunsTree covers the RemoveAll:
// a self-hosted runner reuses its temp dir, so a previous run's half
// installed tool tree must not be treated as this run's install.
func TestPrepareChangelogMiseEnv_ClearsAPriorRunsTree(t *testing.T) {
	// No t.Parallel(): reads the ambient environment.
	runnerTemp := t.TempDir()
	prepareDir := filepath.Join(runnerTemp, "reusable-ci-prepare-mise-42")
	stale := filepath.Join(prepareDir, dataSubdir, "stale-tool")

	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareChangelogMiseEnv(prepareDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a prior run's tree survived: stat %s -> %v", stale, err)
	}
}

func TestChangelogPrepareDir_RejectsUnsafeRunIDBeforeRemovingAnything(t *testing.T) {
	runnerTemp := t.TempDir()

	marker := filepath.Join(runnerTemp, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := changelogPrepareDir("../../..", runnerTemp)
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("unsafe run ID error = %v, want ErrUsage", err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unsafe run ID removed unrelated content: %v", err)
	}
}

// TestDefaultRunID_FallsBackToSomethingProcessSpecific keeps two concurrent runs on the same self-hosted
// runner out of each other's prepare tree when the forge supplies no run id.
func TestDefaultRunID_FallsBackToSomethingProcessSpecific(t *testing.T) {
	t.Parallel()

	if got := defaultRunID("77"); got != "77" {
		t.Errorf("defaultRunID(77) = %q", got)
	}

	if got := defaultRunID("  "); got == "" || got == "  " {
		t.Errorf("blank run id must fall back to something process-specific, got %q", got)
	}
}

func TestEnsureBinHome_CreatesTheDirectory(t *testing.T) {
	t.Parallel()

	want := filepath.Join(t.TempDir(), "nested", "bin")

	got, err := ensureBinHome(want)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Errorf("bin home = %q, want %q", got, want)
	}

	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("bin home was not created: %v", err)
	}
}

// TestFindMiseToolBinary_FindsTheExecutableNotASameNamedDirectory pins what counts as the installed binary. mise
// lays the tool out either directly in the install dir or one level down
// (bin/, or a versioned dir), so the search is two shapes wide -- but it
// must still land on a real executable file.
func TestFindMiseToolBinary_FindsTheExecutableNotASameNamedDirectory(t *testing.T) {
	t.Parallel()

	t.Run("directly in the install dir", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		want := writeExecutable(t, dir, gitCliffBin)

		got, err := findMiseToolBinary(dir, gitCliffBin)
		if err != nil {
			t.Fatal(err)
		}

		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("one level down", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		want := writeExecutable(t, filepath.Join(dir, "bin"), gitCliffBin)

		got, err := findMiseToolBinary(dir, gitCliffBin)
		if err != nil {
			t.Fatal(err)
		}

		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a directory of the right name is not the binary", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, gitCliffBin), 0o755); err != nil {
			t.Fatal(err)
		}

		// The real binary sits one level down; the same-named directory
		// at the top must not shadow it.
		want := writeExecutable(t, filepath.Join(dir, "v2.6.1"), gitCliffBin)

		got, err := findMiseToolBinary(dir, gitCliffBin)
		if err != nil {
			t.Fatal(err)
		}

		if got != want {
			t.Errorf("got %q, want the real binary %q", got, want)
		}
	})

	t.Run("a non-executable file is not the binary", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, gitCliffBin), []byte("# readme"), 0o600); err != nil {
			t.Fatal(err)
		}

		if got, err := findMiseToolBinary(dir, gitCliffBin); !errors.Is(err, errs.ErrMissingInput) {
			t.Errorf("got (%q, %v), want ErrMissingInput", got, err)
		}
	})

	t.Run("nothing installed", func(t *testing.T) {
		t.Parallel()

		if _, err := findMiseToolBinary(t.TempDir(), gitCliffBin); !errors.Is(err, errs.ErrMissingInput) {
			t.Errorf("err = %v, want ErrMissingInput", err)
		}
	})
}

func TestChangelogPublication_RefusesOldLinkOnSecondRun(t *testing.T) {
	t.Parallel()

	root := changelogTempDir(t)
	binHome, pathFile := filepath.Join(root, "bin"), filepath.Join(root, "runner-path")
	old, current := filepath.Join(root, "old"), filepath.Join(root, "current")
	writeChangelogCanary(t, old, "old inert executable\n", 0o751)
	writeChangelogCanary(t, current, "current inert executable\n", 0o711)

	link, err := publishChangelogBinary(old, binHome, gitCliffBin, pathFile)
	if err != nil {
		t.Fatal(err)
	}

	if link != filepath.Join(binHome, gitCliffBin) {
		t.Errorf("link = %q", link)
	}

	unchanged := changelogUnchanged(t, root)
	if got, publishErr := publishChangelogBinary(current, binHome, gitCliffBin, pathFile); got != "" || !errors.Is(publishErr, errs.ErrValidation) || !strings.Contains(publishErr.Error(), "occupied") {
		t.Errorf("old-link refusal = (%q, %v), want occupied validation error", got, publishErr)
	}

	unchanged()

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}

	if target != old {
		t.Errorf("old link points at %q, want preserved target %q", target, old)
	}
}

func TestChangelogPublication_SelectedFileAndRepeat(t *testing.T) { //nolint:gocognit,gocyclo // positives share exact source, destination identity and append-only PATH checks.
	t.Parallel()

	for _, scenario := range []string{"fresh git-cliff", "fresh git-chglog", "same file", "hardlink", "existing link", "source link"} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogTempDir(t)
			fixtures := filepath.Join(root, "fixtures")
			binHome, pathFile := filepath.Join(root, "bin"), filepath.Join(root, "runner-path")

			bin := gitCliffBin
			if scenario == "fresh git-chglog" {
				bin = gitChglogBin
			}

			source := filepath.Join(fixtures, bin)
			writeChangelogCanary(t, source, "selected inert executable\n", 0o751)
			writeChangelogCanary(t, filepath.Join(fixtures, "unselected"), "sibling executable canary\n", 0o711)
			changelogSymlink(t, "missing", filepath.Join(fixtures, "dangling-sibling"))

			if err := os.Mkdir(binHome, 0o700); err != nil {
				t.Fatal(err)
			}

			candidate, wantTarget := source, source

			switch scenario {
			case "fresh git-cliff":
				binHome = filepath.Join(root, "new", "bin")
			case "same file":
				binHome = fixtures
			case "hardlink":
				if err := os.Link(source, filepath.Join(binHome, bin)); err != nil {
					t.Fatal(err)
				}
			case "existing link":
				wantTarget = "../fixtures/" + bin
				changelogSymlink(t, wantTarget, filepath.Join(binHome, bin))
			case "source link":
				changelogSymlink(t, bin, filepath.Join(fixtures, "hop"))
				candidate = filepath.Join(fixtures, "selected-link")
				changelogSymlink(t, filepath.Join(fixtures, "hop"), candidate)
			}

			prefix, pathMode := "prior PATH bytes\n", os.FileMode(0o640)
			if scenario == "fresh git-cliff" {
				prefix, pathMode = "", 0o644
			} else {
				writeChangelogCanary(t, pathFile, prefix, pathMode)
			}

			unchanged := changelogUnchanged(t, fixtures)

			sourceInfo, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}

			destination := filepath.Join(binHome, bin)
			previous, _ := os.Lstat(destination)

			previousPath, _ := os.Lstat(pathFile)
			for count := 1; count <= 2; count++ {
				got, err := publishChangelogBinary(candidate, binHome, bin, pathFile)
				if err != nil || got != destination {
					t.Fatalf("publication = (%q, %v), want %q", got, err, destination)
				}

				unchanged()

				current, err := os.Lstat(destination)
				if err != nil {
					t.Fatalf("published destination missing: %v", err)
				}

				if previous != nil && (!os.SameFile(previous, current) || previous.Mode() != current.Mode()) {
					t.Error("same-file destination identity or mode changed")
				}

				previous = current
				if current.Mode()&os.ModeSymlink != 0 {
					if target, readErr := os.Readlink(destination); readErr != nil || target != wantTarget {
						t.Errorf("destination link spelling = %q, want %q: %v", target, wantTarget, readErr)
					}
				}

				resolved, err := os.Stat(destination)
				if err != nil || !os.SameFile(sourceInfo, resolved) || resolved.Mode() != sourceInfo.Mode() {
					t.Errorf("destination does not resolve to selected executable: %v", err)
				}

				if body, readErr := os.ReadFile(destination); readErr != nil || string(body) != "selected inert executable\n" {
					t.Errorf("exposed bytes = %q: %v", body, readErr)
				}

				if body, readErr := os.ReadFile(pathFile); readErr != nil || string(body) != prefix+strings.Repeat(binHome+"\n", count) {
					t.Errorf("append-only BinHome PATH = %q: %v", body, readErr)
				}

				currentPath, err := os.Lstat(pathFile)
				if err != nil || currentPath.Mode() != pathMode || (previousPath != nil && !os.SameFile(previousPath, currentPath)) {
					t.Errorf("PATH identity or mode changed: %v", err)
				}

				previousPath = currentPath

				if binHome != fixtures {
					entries, err := os.ReadDir(binHome)
					if err != nil || len(entries) != 1 || entries[0].Name() != bin {
						t.Errorf("publication exposed siblings: %v: %v", entries, err)
					}
				}
			}
		})
	}
}

func TestChangelogPublication_RelativeBinHome(t *testing.T) { //nolint:gocognit // both cwd-relative forms share append and identity checks across reruns.
	// No t.Parallel(): cwd is process-wide; t.Chdir restores it after each case.
	for _, binHome := range []string{".", "relative bin"} {
		t.Run(binHome, func(t *testing.T) {
			root := changelogTempDir(t)

			cwd := filepath.Join(root, "work")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}

			t.Chdir(cwd)

			fixtures := filepath.Join(root, "fixtures")
			source, pathFile := filepath.Join(fixtures, "source"), filepath.Join(root, "runner-path")
			writeChangelogCanary(t, source, "inert selected executable\n", 0o751)
			writeChangelogCanary(t, pathFile, "prior PATH\n", 0o640)
			unchanged := changelogUnchanged(t, fixtures)

			pathInfo, err := os.Stat(pathFile)
			if err != nil {
				t.Fatal(err)
			}

			wantHome := filepath.Join(cwd, binHome)
			wantLink := filepath.Join(wantHome, gitCliffBin)

			var previous os.FileInfo

			for count := 1; count <= 2; count++ {
				got, err := publishChangelogBinary(source, binHome, gitCliffBin, pathFile)
				if err != nil || got != wantLink || !filepath.IsAbs(got) {
					t.Fatalf("relative BinHome publication = (%q, %v), want absolute %q", got, err, wantLink)
				}

				if target, readErr := os.Readlink(got); readErr != nil || target != source {
					t.Errorf("relative BinHome link = %q, want %q: %v", target, source, readErr)
				}

				current, err := os.Lstat(got)
				if err != nil || (previous != nil && (!os.SameFile(previous, current) || previous.Mode() != current.Mode())) {
					t.Fatalf("relative BinHome rerun changed destination identity or mode: %v", err)
				}

				previous = current

				if body, readErr := os.ReadFile(pathFile); readErr != nil || string(body) != "prior PATH\n"+strings.Repeat(wantHome+"\n", count) {
					t.Errorf("relative BinHome exported nonabsolute PATH: %q: %v", body, readErr)
				}

				currentPath, err := os.Stat(pathFile)
				if err != nil || !os.SameFile(pathInfo, currentPath) || currentPath.Mode() != 0o640 {
					t.Errorf("relative BinHome changed PATH identity or mode: %v", err)
				}

				unchanged()
			}
		})
	}
}

func TestChangelogPublication_RefusesUnsafeCWD(t *testing.T) {
	// No t.Parallel(): exercise the absolute export after resolving against cwd.
	for _, binHome := range []string{".", "relative bin"} {
		t.Run(binHome, func(t *testing.T) {
			root := changelogTempDir(t)

			cwd := filepath.Join(root, "work"+string(os.PathListSeparator)+"other")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}

			t.Chdir(cwd)

			source, pathFile := filepath.Join(root, "source"), filepath.Join(root, "runner-path")
			writeChangelogCanary(t, source, "inert selected executable\n", 0o751)
			writeChangelogCanary(t, pathFile, "prior PATH\n", 0o640)
			unchanged := changelogUnchanged(t, root)

			got, err := publishChangelogBinary(source, binHome, gitCliffBin, pathFile)
			if got != "" || !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "PATH entry") {
				t.Errorf("unsafe cwd publication = (%q, %v), want PATH validation refusal", got, err)
			}

			unchanged()
		})
	}
}

func TestChangelogPublication_RefusalPreservesState(t *testing.T) { //nolint:gocognit,gocyclo // focused changelog wiring cases; the full shared publisher matrix remains in miseexpose_boundary_test.go.
	t.Parallel()

	for _, scenario := range []string{
		"occupied file", "occupied directory", "dangling destination",
		"PATH planned destination", "PATH existing destination", "PATH source", "PATH hardlink",
		"PATH symlink", "PATH directory", "PATH missing parent", "PATH linked parent", "linked bin home",
		"PATH CR", "PATH LF", "PATH colon", "PATH tab", "PATH NUL", "PATH spelling before normalization",
		"relative source", "source CR", "source LF", "source tab", "source colon", "source NUL",
		"source spelling before resolution", "resolved source LF", "source missing", "source directory", "source not executable", "unallowlisted name", "empty PATH",
	} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogTempDir(t)
			binHome, pathFile := filepath.Join(root, "bin"), filepath.Join(root, "runner-path")
			source := filepath.Join(root, "source")
			writeChangelogCanary(t, source, "selected source canary\n", 0o751)
			writeChangelogCanary(t, pathFile, "prior PATH canary\n", 0o640)

			canary := filepath.Join(root, "canary")
			writeChangelogCanary(t, canary, "unrelated canary\n", 0o711)

			bin, wantReason, wantErr := gitCliffBin, "PATH ", errs.ErrValidation
			destination := filepath.Join(binHome, bin)

			switch scenario {
			case "occupied file":
				writeChangelogCanary(t, destination, "occupied executable\n", 0o711)

				wantReason = "occupied"
			case "occupied directory":
				writeChangelogCanary(t, filepath.Join(destination, "child"), "directory child\n", 0o600)

				wantReason = "occupied"
			case "dangling destination", "PATH existing destination":
				if err := os.Mkdir(binHome, 0o700); err != nil {
					t.Fatal(err)
				}

				target := "missing"

				wantReason = "occupied"
				if scenario == "PATH existing destination" {
					target, pathFile, wantReason = source, destination, "PATH "
				}

				changelogSymlink(t, target, destination)
			case "PATH planned destination":
				pathFile = destination
			case "PATH source":
				pathFile = source
			case "PATH hardlink":
				pathFile = filepath.Join(root, "hardlink-PATH")
				if err := os.Link(source, pathFile); err != nil {
					t.Fatal(err)
				}
			case "PATH symlink":
				pathFile = filepath.Join(root, "linked-PATH")
				changelogSymlink(t, filepath.Base(canary), pathFile)
			case "PATH directory":
				pathFile = filepath.Join(root, "path-directory")
				if err := os.Mkdir(pathFile, 0o700); err != nil {
					t.Fatal(err)
				}
			case "PATH missing parent":
				pathFile, wantErr = filepath.Join(root, "missing", "runner-path"), os.ErrNotExist
			case "PATH linked parent":
				changelogSymlink(t, root, filepath.Join(root, "alias"))
				pathFile = filepath.Join(root, "alias", "runner-path")
			case "linked bin home":
				changelogSymlink(t, root, binHome)

				wantReason = "bin home"
			case "PATH CR":
				binHome += "\rinjected"
			case "PATH LF":
				binHome += "\ninjected"
			case "PATH colon":
				binHome = filepath.Join(root, "new", "tools"+string(os.PathListSeparator)+"other")
			case "PATH tab":
				binHome += "\tinjected"
			case "PATH NUL":
				binHome += "\x00injected"
			case "PATH spelling before normalization":
				// Cleaning must not erase the separator before spelling validation.
				binHome = root + "/unsafe" + string(os.PathListSeparator) + "component/../new/bin"
			case "relative source":
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}

				source, err = filepath.Rel(cwd, source)
				if err != nil {
					t.Fatal(err)
				}

				wantReason = "invalid changelog executable path"
			case "source CR", "source LF", "source tab", "source colon", "source NUL", "resolved source LF":
				suffix := map[string]string{"source CR": "\r", "source LF": "\n", "source tab": "\t", "source colon": ":", "source NUL": "\x00", "resolved source LF": "\n"}[scenario]

				unsafe := source + suffix
				if scenario != "source NUL" {
					writeChangelogCanary(t, unsafe, "unsafe spelling canary\n", 0o751)
				}

				source, wantReason = unsafe, "invalid changelog executable path"
				if scenario == "resolved source LF" {
					source, wantReason = filepath.Join(root, "source-link"), "invalid resolved changelog executable path"
					changelogSymlink(t, unsafe, source)
				}
			case "source missing":
				source, wantReason, wantErr = filepath.Join(root, "missing"), "resolve changelog executable", os.ErrNotExist
			case "source spelling before resolution":
				unsafeDir := filepath.Join(root, "unsafe\ncomponent")
				if err := os.Mkdir(unsafeDir, 0o700); err != nil {
					t.Fatal(err)
				}
				// Resolution would erase the unsafe component of this spelling.
				source, wantReason = unsafeDir+"/../source", "invalid changelog executable path"
			case "source directory":
				source, wantReason = root, "executable regular file"
			case "source not executable":
				source, wantReason = pathFile, "executable regular file"
			case "unallowlisted name":
				bin, wantReason = "other-renderer", "unsupported changelog binary"
			case "empty PATH":
				pathFile, wantReason, wantErr = "", "path-file is required", errs.ErrUsage
			}

			unchanged := changelogUnchanged(t, root)

			got, err := publishChangelogBinary(source, binHome, bin, pathFile)
			if got != "" || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), wantReason) {
				t.Errorf("refusal = (%q, %v), want %v containing %q", got, err, wantErr, wantReason)
			}

			unchanged()
		})
	}
}

func writeChangelogCanary(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), mode); err != nil { //nolint:gosec // owned inert canaries, never executed.
		t.Fatal(err)
	}
}

func changelogSymlink(t *testing.T, target, path string) {
	t.Helper()

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// Capture every owned entry without following links, including leaf identities.
func changelogUnchanged(t *testing.T, root string) func() {
	t.Helper()

	type entryState struct {
		info os.FileInfo
		body string
	}

	snapshot := func() map[string]entryState {
		state := make(map[string]entryState)

		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}

			var body string

			switch {
			case info.Mode().IsRegular():
				var data []byte

				data, err = os.ReadFile(path) //nolint:gosec // test-owned tree is not concurrently modified.
				body = string(data)
			case info.Mode()&os.ModeSymlink != 0:
				body, err = os.Readlink(path)
			}

			state[path] = entryState{info: info, body: body}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}

		return state
	}
	before := snapshot()

	return func() {
		t.Helper()

		after := snapshot()
		if len(after) != len(before) {
			t.Errorf("publication changed owned entry count: got %d, want %d", len(after), len(before))
		}

		for path, old := range before {
			current, ok := after[path]
			if !ok || !os.SameFile(old.info, current.info) || old.info.Mode() != current.info.Mode() || old.body != current.body {
				t.Errorf("publication changed owned bytes, mode, identity or link spelling: %s", path)
			}
		}
	}
}

// TestResolveChangelogBinary_RejectsAnEmptyOrFailingMiseWhere covers the two ways `mise where` fails to
// name an install dir. Neither may be read as "installed at the empty
// path", which would resolve the tool against the process working dir.
func TestResolveChangelogBinary_RejectsAnEmptyOrFailingMiseWhere(t *testing.T) {
	t.Parallel()

	installRoot := changelogInstallTree(t)
	dir := filepath.Join(installRoot, "installs", "renderer")
	want := writeExecutable(t, dir, gitCliffBin)

	got, err := resolveChangelogBinary(t.Context(), whereRunner{out: dir + "\n"}, nil, "aqua:orhun/git-cliff@2.6.1", gitCliffBin, installRoot)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, err := resolveChangelogBinary(t.Context(), whereRunner{out: "  \n"}, nil, "t", gitCliffBin, installRoot); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("empty `mise where` output: err = %v, want ErrValidation", err)
	}

	// The cause has to survive the wrap: a bare "must not yield an install
	// dir" would also pass on an error that lost why mise refused.
	if _, err := resolveChangelogBinary(t.Context(), whereRunner{err: errs.ErrUnsupported}, nil, "t", gitCliffBin, installRoot); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("failing `mise where`: err = %v, want it to wrap ErrUnsupported", err)
	}
}

// changelogInstallTree builds one run's isolated prepare tree and returns the
// resolved installation root a renderer may be selected from.
func changelogInstallTree(t *testing.T) string {
	t.Helper()

	prepare := filepath.Join(changelogTempDir(t), "reusable-ci-prepare-mise-42")
	if err := os.MkdirAll(filepath.Join(prepare, dataSubdir), 0o755); err != nil {
		t.Fatal(err)
	}

	root, err := changelogInstallRoot(prepare)
	if err != nil {
		t.Fatal(err)
	}

	return root
}

// TestChangelogInstallRoot_IsTheDirectoryMiseWasToldToInstallInto keeps the
// publication authority and the runner environment from drifting apart. A root
// naming some other directory would refuse every real installation, and one
// naming a parent would readmit the caller paths the prepare tree exists to
// exclude. A missing tree is an error, never an empty root that matches nothing.
func TestChangelogInstallRoot_IsTheDirectoryMiseWasToldToInstallInto(t *testing.T) {
	t.Parallel()

	base := changelogTempDir(t)
	prepare := filepath.Join(base, "reusable-ci-prepare-mise-42")

	if root, err := changelogInstallRoot(prepare); root != "" || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing prepare tree: got (%q, %v), want ErrNotExist", root, err)
	}

	env, err := prepareChangelogMiseEnv(prepare, filepath.Join(base, "bin"))
	if err != nil {
		t.Fatal(err)
	}

	root, err := changelogInstallRoot(prepare)
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(envLookup(env, "MISE_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}

	if root != want {
		t.Errorf("installation root = %q, want the runner's MISE_DATA_DIR %q", root, want)
	}
}

// TestResolveChangelogBinary_RequiresThisRunsInstallationRoot binds the selected
// renderer to the isolated tree this run just installed into. `mise where` output
// is subprocess text, not publication authority: without this check any absolute
// path it names -- a repository checkout, a preinstalled system directory, a
// leftover tree from an earlier job step -- would be linked into the bin home and
// executed as the renderer. Resolution happens before containment so a link
// cannot spell its way back inside.
func TestResolveChangelogBinary_RequiresThisRunsInstallationRoot(t *testing.T) {
	t.Parallel()

	// setup builds the fixture and returns the `mise where` output together with
	// the executable a successful selection must land on.
	for _, testCase := range []struct {
		name  string
		setup func(t *testing.T, root, outside string) (string, string)
		want  error
	}{
		{
			name: "installed directly in the run's own tree",
			setup: func(t *testing.T, root, _ string) (string, string) {
				t.Helper()

				dir := filepath.Join(root, "installs", "renderer")

				return dir, writeExecutable(t, dir, gitCliffBin)
			},
		},
		{
			name: "installed one level below the reported dir",
			setup: func(t *testing.T, root, _ string) (string, string) {
				t.Helper()

				dir := filepath.Join(root, "installs", "renderer")

				return dir, writeExecutable(t, filepath.Join(dir, "bin"), gitCliffBin)
			},
		},
		{
			// The shared publisher resolves legitimate source links; this claim
			// is about where the selection came from, not the bytes it names.
			name: "a leaf link inside the tree stays selectable",
			setup: func(t *testing.T, root, outside string) (string, string) {
				t.Helper()
				target := writeExecutable(t, outside, gitCliffBin)

				dir := filepath.Join(root, "installs", "renderer")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}

				link := filepath.Join(dir, gitCliffBin)
				changelogSymlink(t, target, link)

				return dir, link
			},
		},
		{
			name: "a directory outside the tree",
			setup: func(t *testing.T, _, outside string) (string, string) {
				t.Helper()
				writeExecutable(t, outside, gitCliffBin)

				return outside, ""
			},
			want: errs.ErrValidation,
		},
		{
			name: "a linked install dir leaving the tree",
			setup: func(t *testing.T, root, outside string) (string, string) {
				t.Helper()
				writeExecutable(t, outside, gitCliffBin)

				installs := filepath.Join(root, "installs")
				if err := os.MkdirAll(installs, 0o755); err != nil {
					t.Fatal(err)
				}

				link := filepath.Join(installs, "renderer")
				changelogSymlink(t, outside, link)

				return link, ""
			},
			want: errs.ErrValidation,
		},
		{
			name: "a linked child directory leaving the tree",
			setup: func(t *testing.T, root, outside string) (string, string) {
				t.Helper()
				writeExecutable(t, outside, gitCliffBin)

				dir := filepath.Join(root, "installs", "renderer")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}

				changelogSymlink(t, outside, filepath.Join(dir, "v2.6.1"))

				return dir, ""
			},
			want: errs.ErrValidation,
		},
		{
			name: "a relative report is not resolved against the working directory",
			setup: func(t *testing.T, root, _ string) (string, string) {
				t.Helper()
				writeExecutable(t, filepath.Join(root, "installs", "renderer"), gitCliffBin)

				return filepath.Join("installs", "renderer"), ""
			},
			want: errs.ErrValidation,
		},
		{
			name: "a PATH-separated report",
			setup: func(t *testing.T, root, _ string) (string, string) {
				t.Helper()

				dir := filepath.Join(root, "installs", "renderer")
				writeExecutable(t, dir, gitCliffBin)

				return dir + string(os.PathListSeparator) + dir, ""
			},
			want: errs.ErrValidation,
		},
		{
			name: "a report naming nothing installed",
			setup: func(t *testing.T, root, _ string) (string, string) {
				t.Helper()

				return filepath.Join(root, "installs", "never-installed"), ""
			},
			want: os.ErrNotExist,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base := changelogTempDir(t)

			prepare, outside := filepath.Join(base, "prepare"), filepath.Join(base, "outside install")
			if err := os.MkdirAll(filepath.Join(prepare, dataSubdir), 0o755); err != nil {
				t.Fatal(err)
			}

			installRoot, err := changelogInstallRoot(prepare)
			if err != nil {
				t.Fatal(err)
			}

			output, want := testCase.setup(t, installRoot, outside)
			unchanged := changelogUnchanged(t, base)

			got, err := resolveChangelogBinary(t.Context(), whereRunner{out: " \n" + output + "\n "}, nil, "aqua:orhun/git-cliff@2.6.1", gitCliffBin, installRoot)
			switch {
			case testCase.want != nil:
				if got != "" || !errors.Is(err, testCase.want) {
					t.Errorf("got (%q, %v), want (\"\", %v)", got, err, testCase.want)
				}
			case err != nil || got != want:
				t.Errorf("got (%q, %v), want %q", got, err, want)
			}
			// Selection reads; it never repairs, creates or removes anything.
			unchanged()
		})
	}
}

// whereRunner answers every mise invocation with a fixed result.
type whereRunner struct {
	out string
	err error
}

func (r whereRunner) Run(context.Context, []string, ...string) (string, error) { return r.out, r.err }

//nolint:unparam // the binary name is fixed today; the parameter keeps the helper readable at the call sites.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, name)
	//nolint:gosec // the point of the fixture is that the mode bit is set.
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	return path
}

// envLookup returns the last value for name, which is what a process
// started with this environment would see.
func envLookup(env []string, name string) string {
	value := ""

	for _, item := range env {
		if got, rest, ok := strings.Cut(item, "="); ok && got == name {
			value = rest
		}
	}

	return value
}

func envEntryCount(env []string, name string) int {
	count := 0

	for _, item := range env {
		if got, _, ok := strings.Cut(item, "="); ok && got == name {
			count++
		}
	}

	return count
}
