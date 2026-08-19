// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The two renderer binary names appear throughout; named so the package's
// goconst budget is not spent on a test fixture.
const (
	gitCliffBin  = "git-cliff"
	gitChglogBin = "git-chglog"
	cacheSubdir  = "cache"
)

// TestChangelogRendererSelector pins the backend allowlist. The selector
// returned here is handed to `mise install` verbatim, so the closed set is
// what stops a caller-supplied backend from naming an arbitrary aqua
// package to install.
func TestChangelogRendererSelector(t *testing.T) {
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

func TestValidateChangelogRendererInput(t *testing.T) {
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

// TestPrepareChangelogMiseEnv covers the isolation the prepare tree exists
// for: the renderer install must not read or write the ambient mise trees,
// and must not inherit a MISE_* pointing at them.
func TestPrepareChangelogMiseEnv(t *testing.T) {
	// No t.Parallel(): mutates the environment via t.Setenv.
	shared := t.TempDir()
	t.Setenv("MISE_DATA_DIR", shared)
	t.Setenv("MISE_CACHE_DIR", shared)
	t.Setenv("MISE_CONFIG_DIR", shared)
	t.Setenv("MISE_STATE_DIR", shared)

	runnerTemp := t.TempDir()
	binHome := filepath.Join(t.TempDir(), "bin")

	env, err := prepareChangelogMiseEnv(InstallChangelogRendererInput{
		RunnerTemp: runnerTemp,
		RunID:      "42",
	}, binHome)
	if err != nil {
		t.Fatal(err)
	}

	prepareDir := filepath.Join(runnerTemp, "reusable-ci-prepare-mise-42")

	for name, sub := range map[string]string{
		"MISE_CACHE_DIR":  cacheSubdir,
		"MISE_CONFIG_DIR": "config",
		"MISE_DATA_DIR":   "data",
		"MISE_STATE_DIR":  "state",
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
	stale := filepath.Join(prepareDir, "data", "stale-tool")

	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareChangelogMiseEnv(InstallChangelogRendererInput{
		RunnerTemp: runnerTemp, RunID: "42",
	}, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a prior run's tree survived: stat %s -> %v", stale, err)
	}
}

// TestDefaultRunID keeps two concurrent runs on the same self-hosted
// runner out of each other's prepare tree when the forge supplies no run id.
func TestDefaultRunID(t *testing.T) {
	t.Parallel()

	if got := defaultRunID("77"); got != "77" {
		t.Errorf("defaultRunID(77) = %q", got)
	}

	if got := defaultRunID("  "); got == "" || got == "  " {
		t.Errorf("blank run id must fall back to something process-specific, got %q", got)
	}
}

func TestEnsureBinHome(t *testing.T) {
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

// TestFindMiseToolBinary pins what counts as the installed binary. mise
// lays the tool out either directly in the install dir or one level down
// (bin/, or a versioned dir), so the search is two shapes wide -- but it
// must still land on a real executable file.
func TestFindMiseToolBinary(t *testing.T) {
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

// TestLinkChangelogBinary covers the replace-not-add behaviour: a
// self-hosted runner's ~/.local/bin keeps the previous run's symlink, and
// leaving it would run the previous version under this run's name.
func TestLinkChangelogBinary(t *testing.T) {
	t.Parallel()

	binHome := t.TempDir()
	old := writeExecutable(t, t.TempDir(), gitCliffBin)
	current := writeExecutable(t, t.TempDir(), gitCliffBin)

	link, err := linkChangelogBinary(old, binHome, gitCliffBin)
	if err != nil {
		t.Fatal(err)
	}

	if link != filepath.Join(binHome, gitCliffBin) {
		t.Errorf("link = %q", link)
	}

	// Second run of the same verb, pointing somewhere else.
	if _, err = linkChangelogBinary(current, binHome, gitCliffBin); err != nil {
		t.Fatal(err)
	}

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}

	if target != current {
		t.Errorf("link points at %q, want the binary from this run %q", target, current)
	}
}

// TestResolveChangelogBinary covers the two ways `mise where` fails to
// name an install dir. Neither may be read as "installed at the empty
// path", which would resolve the tool against the process working dir.
func TestResolveChangelogBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := writeExecutable(t, dir, gitCliffBin)

	got, err := resolveChangelogBinary(t.Context(), whereRunner{out: dir + "\n"}, nil, "aqua:orhun/git-cliff@2.6.1", gitCliffBin)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if _, err := resolveChangelogBinary(t.Context(), whereRunner{out: "  \n"}, nil, "t", gitCliffBin); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("empty `mise where` output: err = %v, want ErrValidation", err)
	}

	if _, err := resolveChangelogBinary(t.Context(), whereRunner{err: errs.ErrUnsupported}, nil, "t", gitCliffBin); err == nil {
		t.Error("a failing `mise where` must not yield an install dir")
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
