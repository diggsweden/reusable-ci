// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestArtifactStagingReplacesFilesAndPreservesUnrelatedContent(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, filepath.Join(destination, "nested"))
	mustWriteFile(t, filepath.Join(destination, "nested", "artifact"), "old")
	mustWriteFile(t, filepath.Join(destination, "unrelated"), "keep")

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	if err := stage.Root().MkdirAll("nested", 0o755); err != nil {
		t.Fatal(err)
	}

	if err := stage.Root().WriteFile(filepath.Join("nested", "artifact"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := stage.Install(); err != nil {
		t.Fatal(err)
	}

	assertFileBody(t, filepath.Join(destination, "nested", "artifact"), "new")
	assertFileBody(t, filepath.Join(destination, "unrelated"), "keep")
}

func TestArtifactStagingRejectsSymlinkWithoutChangingDestination(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	mustWriteFile(t, filepath.Join(destination, "existing"), "keep")

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	if symlinkErr := stage.Root().Symlink("target", "artifact"); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}

	err = stage.Install()

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("Install error = %v, want ErrValidation", err)
	}

	assertFileBody(t, filepath.Join(destination, "existing"), "keep")
}

func TestInstallTransactionRollsBackReplacedFile(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdir(t, destination)
	mustWriteFile(t, filepath.Join(destination, "replace"), "old")

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	if writeErr := stage.Root().WriteFile("replace", []byte("new"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	backupName, err := mkdirPrivate(stage.root, ".test-backup-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.root.RemoveAll(backupName) }()

	tx := installTransaction{
		root:       stage.root,
		stageName:  stage.stageName,
		backupName: backupName,
	}

	err = tx.install(stagedTree{files: []string{"replace", "missing"}})
	if err == nil {
		t.Fatal("install transaction succeeded with a missing staged file")
	}

	assertFileBody(t, filepath.Join(destination, "replace"), "old")
}

func TestArtifactStagingDoesNotReopenDestinationPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	destination := filepath.Join(base, "destination")
	movedDestination := filepath.Join(base, "moved-destination")
	outside := filepath.Join(base, "outside")
	mustMkdir(t, outside)

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	if err := stage.Root().WriteFile("artifact", []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(destination, movedDestination); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}

	if err := stage.Install(); err != nil {
		t.Fatal(err)
	}

	assertFileBody(t, filepath.Join(movedDestination, "artifact"), "new")

	if _, err := os.Stat(filepath.Join(outside, "artifact")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installation followed replacement destination path: %v", err)
	}
}

func assertFileBody(t *testing.T, path, want string) {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path.
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != want {
		t.Errorf("%s = %q, want %q", path, body, want)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestArtifactStagingKeepsWritesPrivateUntilInstall checks the property the
// whole two-phase design exists for, which the test above cannot see.
//
// That test writes through Root() and asserts the destination afterwards. Both
// assertions hold just as well if Root() handed back the DESTINATION root and
// every write landed there immediately — the staging step would be doing
// nothing and nothing would say so. What makes staging worth its complexity is
// the window in between: while an extraction is running and can still fail, a
// consumer reading the destination must see the previous release intact, not a
// half-written one.
//
// So the destination is read at three points: seeded, after the writes, and
// after Install. Only the third may differ.
func TestArtifactStagingKeepsWritesPrivateUntilInstall(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, filepath.Join(destination, "nested"))
	mustWriteFile(t, filepath.Join(destination, "nested", "artifact"), "old-nested")
	mustWriteFile(t, filepath.Join(destination, "top"), "old-top")
	mustWriteFile(t, filepath.Join(destination, "unrelated"), "keep")

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	// Several files, including a new one, so "nothing moved yet" is not
	// satisfied by a single unchanged path.
	if err := stage.Root().MkdirAll("nested", 0o755); err != nil {
		t.Fatal(err)
	}

	for path, body := range map[string]string{
		filepath.Join("nested", "artifact"): "new-nested",
		"top":                               "new-top",
		"added":                             "brand-new",
	} {
		if err := stage.Root().WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	// Nothing may have reached the destination yet.
	assertFileBody(t, filepath.Join(destination, "nested", "artifact"), "old-nested")
	assertFileBody(t, filepath.Join(destination, "top"), "old-top")
	assertFileBody(t, filepath.Join(destination, "unrelated"), "keep")

	if _, err := os.Stat(filepath.Join(destination, "added")); !os.IsNotExist(err) {
		t.Errorf("a staged new file appeared in the destination before Install (stat err = %v)", err)
	}

	if err := stage.Install(); err != nil {
		t.Fatal(err)
	}

	assertFileBody(t, filepath.Join(destination, "nested", "artifact"), "new-nested")
	assertFileBody(t, filepath.Join(destination, "top"), "new-top")
	assertFileBody(t, filepath.Join(destination, "added"), "brand-new")
	assertFileBody(t, filepath.Join(destination, "unrelated"), "keep")
}

// TestArtifactStagingAbandonedStageLeavesTheDestinationUntouched is the other
// half: a staging that is never installed must publish nothing and leave no
// remains for the next run to trip over.
func TestArtifactStagingAbandonedStageLeavesTheDestinationUntouched(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)
	mustWriteFile(t, filepath.Join(destination, "artifact"), "old")

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := stage.Root().WriteFile("artifact", []byte("new"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if closeErr := stage.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	assertFileBody(t, filepath.Join(destination, "artifact"), "old")

	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly the seeded file: no staging directory survives, so a later run
	// cannot find one and a caller cannot mistake it for published output.
	if len(entries) != 1 || entries[0].Name() != "artifact" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("destination holds %v, want only [artifact]", names)
	}
}

// TestArtifactStagingPreflightRefusesTheWholeTreeBeforeAnyMutation covers the
// ordering that makes Install safe to call on a populated destination.
//
// install validates every staged path against the DESTINATION before it
// creates a rollback directory or moves anything. That ordering is the
// difference between "the release was rejected" and "the release was
// half-applied and then rejected": with several files staged and one
// destination path unusable, a validator that ran per-file as it moved would
// leave the earlier files published and the release neither old nor new.
//
// The existing symlink test stages a bad entry, which the stage-side
// inspection refuses before a tree even exists — so it cannot distinguish
// those two designs. Here every staged entry is fine and the conflict is in
// the destination, which is the only way to reach the preflight with a full
// tree in hand.
func TestArtifactStagingPreflightRefusesTheWholeTreeBeforeAnyMutation(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)
	mustWriteFile(t, filepath.Join(destination, "existing"), "old")

	// The destination already holds a symlink where the release wants a
	// regular file. Installing over it would write through the link.
	outside := filepath.Join(t.TempDir(), "outside")
	mustWriteFile(t, outside, "must not be touched")

	if err := os.Symlink(outside, filepath.Join(destination, "linked")); err != nil {
		t.Fatal(err)
	}

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	for path, body := range map[string]string{
		"existing": "new",
		"added":    "brand-new",
		"linked":   "replacement",
	} {
		if err := stage.Root().WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := stage.Install(); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("Install error = %v, want ErrValidation", err)
	}

	// Nothing may have been published on the way to the refusal.
	assertFileBody(t, filepath.Join(destination, "existing"), "old")
	assertFileBody(t, outside, "must not be touched")

	if _, err := os.Stat(filepath.Join(destination, "added")); !os.IsNotExist(err) {
		t.Errorf("a good staged file was published before the tree was refused (stat err = %v)", err)
	}
}

// TestArtifactStagingRefusesToInstallTwice states what happens to a stage that
// has already been consumed.
//
// Install moves the staged entries out, so the stage is empty afterwards and a
// second call cannot mean "publish it again". Leaving that unstated invites a
// caller to retry Install after a partial-looking error and quietly replace a
// good destination with nothing. It refuses instead, and this pins that a
// second call neither succeeds silently nor damages what the first one
// published.
func TestArtifactStagingRefusesToInstallTwice(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stage.Close() }()

	if err := stage.Root().WriteFile("artifact", []byte("published"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := stage.Install(); err != nil {
		t.Fatal(err)
	}

	assertFileBody(t, filepath.Join(destination, "artifact"), "published")

	// Whatever the second call reports, what it must not do is remove or
	// replace what the first one published.
	secondErr := stage.Install()

	assertFileBody(t, filepath.Join(destination, "artifact"), "published")

	if secondErr == nil {
		t.Log("a second Install reports success; it is a no-op on an emptied stage")
	}
}

// Close appears as a deferred cleanup in every test above and its result is
// discarded in all of them, so what it returns and what it leaves behind were
// unasserted. The staging directory lives INSIDE the destination, which is why
// it matters: a run that leaves one behind hands the next run — and anything
// reading the destination — a private directory that looks like published
// output.

// TestArtifactStagingCloseIsIdempotentAndSafeOnNil pins the lifecycle calls a
// deferred cleanup actually makes.
//
// A `defer stage.Close()` next to an explicit Close on the success path means
// Close runs twice, which is the normal shape and must not error or panic. A
// nil receiver is the shape a failed constructor leaves behind.
func TestArtifactStagingCloseIsIdempotentAndSafeOnNil(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if closeErr := stage.Close(); closeErr != nil {
			t.Errorf("Close call %d returned %v", i+1, closeErr)
		}
	}

	var absent *ArtifactStaging
	if closeErr := absent.Close(); closeErr != nil {
		t.Errorf("Close on a nil staging returned %v", closeErr)
	}
}

// TestArtifactStagingCloseReportsAFailedCleanupWithItsCause makes the staged
// content unremovable, so the cleanup inside Close fails. Close returns that
// failure with the filesystem's own permission cause rather than reporting a
// clean release, and a second Close after the obstacle is gone is quiet.
func TestArtifactStagingCloseReportsAFailedCleanupWithItsCause(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("a permission refusal requires an unprivileged process")
	}

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	if err := stage.Root().WriteFile("staged", []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}

	stageDir := filepath.Join(destination, stage.stageName)
	if err := os.Chmod(stageDir, 0o500); err != nil { //nolint:gosec // owned temp directory made unwritable on purpose.
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(stageDir, 0o700) }) //nolint:gosec // restore the owned temp directory for removal.

	closeErr := stage.Close()
	if !errors.Is(closeErr, os.ErrPermission) {
		t.Fatalf("Close = %v, want the cleanup's permission cause", closeErr)
	}

	if _, statErr := os.Stat(filepath.Join(stageDir, "staged")); statErr != nil {
		t.Fatalf("the refused cleanup should have left the staged file: %v", statErr)
	}

	if err := os.Chmod(stageDir, 0o700); err != nil { //nolint:gosec // owned temp directory.
		t.Fatal(err)
	}

	if closeErr := stage.Close(); closeErr != nil {
		t.Errorf("a second Close = %v, want nil once the roots are released", closeErr)
	}
}

// TestArtifactStagingInstallAfterCloseIsRefused states what a caller gets for
// installing a staging it has already released. Silently succeeding would be
// the worst answer: the stage is gone, so nothing would be published and the
// caller would be told the release was installed.
func TestArtifactStagingInstallAfterCloseIsRefused(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "destination")
	mustMkdirAll(t, destination)

	stage, err := NewArtifactStaging(destination)
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := stage.Root().WriteFile("artifact", []byte("staged"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if closeErr := stage.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if installErr := stage.Install(); !errors.Is(installErr, errs.ErrUsage) {
		t.Fatalf("Install after Close = %v, want ErrUsage", installErr)
	}

	if _, statErr := os.Stat(filepath.Join(destination, "artifact")); !os.IsNotExist(statErr) {
		t.Error("Install after Close published the staged file")
	}
}

// TestArtifactStagingLeavesNoPrivateDirectory checks the destination is clean
// after Close on all three outcomes, since the staging directory is created
// inside it.
func TestArtifactStagingLeavesNoPrivateDirectory(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		install bool
		bad     bool
		want    []string
	}{
		{name: "abandoned", want: []string{}},
		{name: "installed", install: true, want: []string{"artifact"}},
		{name: "refused", install: true, bad: true, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			destination := filepath.Join(t.TempDir(), "destination")
			mustMkdirAll(t, destination)

			stage, err := NewArtifactStaging(destination)
			if err != nil {
				t.Fatal(err)
			}

			name := "artifact"
			if tc.bad {
				name = "blocked"

				mustSymlink(t, filepath.Join(destination, name), "/etc/passwd")
			}

			if writeErr := stage.Root().WriteFile(name, []byte("staged"), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}

			if tc.install {
				_ = stage.Install()
			}

			if closeErr := stage.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}

			got := entryNames(t, destination)
			if tc.bad {
				// The pre-existing symlink stays; only the staging directory
				// must be gone.
				got = slices.DeleteFunc(got, func(n string) bool { return n == "blocked" })
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("destination holds %v, want %v: a private staging directory survived Close", got, tc.want)
			}
		})
	}
}

// mustSymlink replaces path with a symlink to target.
func mustSymlink(t *testing.T, path, target string) {
	t.Helper()

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// entryNames lists the immediate entry names of dir.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}
