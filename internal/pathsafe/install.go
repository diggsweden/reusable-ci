// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pathsafe

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ArtifactStaging owns descriptor-backed roots for a destination and its
// private staging directory. Callers write only through Root and then invoke
// Install; no validated directory is reopened by pathname.
type ArtifactStaging struct {
	root      *os.Root
	stage     *os.Root
	stageName string
}

// NewArtifactStaging opens or creates destination and creates a private
// staging directory inside it. Keeping the transaction below one os.Root lets
// install and rollback use descriptor-rooted atomic renames.
func NewArtifactStaging(destination string) (*ArtifactStaging, error) {
	root, err := MkdirRoot(destination, 0o755)
	if err != nil {
		return nil, fmt.Errorf("open artifact destination %q: %w", destination, err)
	}

	stageName, err := mkdirPrivate(root, ".reusable-ci-artifact-stage-")
	if err != nil {
		_ = root.Close()

		return nil, fmt.Errorf("create artifact staging directory: %w", err)
	}

	stage, err := root.OpenRoot(stageName)
	if err != nil {
		_ = root.RemoveAll(stageName)
		_ = root.Close()

		return nil, fmt.Errorf("open artifact staging directory: %w", err)
	}

	return &ArtifactStaging{root: root, stage: stage, stageName: stageName}, nil
}

// Root returns the staging root used by archive and artifact extractors. The
// handle is owned by ArtifactStaging and must not be closed by the caller.
func (s *ArtifactStaging) Root() *os.Root { return s.stage }

// Close removes any remaining staging data and closes both roots.
func (s *ArtifactStaging) Close() error {
	if s == nil {
		return nil
	}

	var closeErr error
	if s.stage != nil {
		closeErr = errors.Join(closeErr, s.stage.Close())
		s.stage = nil
	}

	if s.root != nil {
		closeErr = errors.Join(closeErr, s.root.RemoveAll(s.stageName), s.root.Close())
		s.root = nil
	}

	return closeErr
}

type stagedTree struct {
	dirs  []string
	files []string
	links []string
}

func inspectStagedTree(root *os.Root, allowRelativeSymlinks bool) (stagedTree, error) { //nolint:cyclop // path, type and optional confined-link checks precede every installation.
	var tree stagedTree

	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == "." {
			return nil
		}

		if !Relative(filepath.FromSlash(path)) {
			return fmt.Errorf("resolve staged artifact path %q: %w", path, errs.ErrValidation)
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			if !allowRelativeSymlinks {
				return fmt.Errorf("staged artifact path %q is a symlink: %w", path, errs.ErrValidation)
			}

			target, err := root.Readlink(path)
			if err != nil {
				return err
			}

			if filepath.IsAbs(target) || !Relative(filepath.Join(filepath.Dir(path), target)) {
				return fmt.Errorf("staged symlink %q escapes the destination: %w", path, errs.ErrValidation)
			}

			tree.links = append(tree.links, filepath.FromSlash(path))
			tree.files = append(tree.files, filepath.FromSlash(path))

			return nil
		}

		if entry.IsDir() {
			tree.dirs = append(tree.dirs, filepath.FromSlash(path))

			return nil
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("staged artifact path %q is not a regular file: %w", path, errs.ErrValidation)
		}

		tree.files = append(tree.files, filepath.FromSlash(path))

		return nil
	})
	if err != nil {
		return stagedTree{}, fmt.Errorf("inspect staged artifact: %w", err)
	}

	return tree, nil
}

func validateInstallTarget(root *os.Root, rel string, directory bool) error {
	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect artifact destination %q: %w", rel, err)
	}

	if info.Mode()&fs.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("artifact destination %q has an incompatible file type: %w", rel, errs.ErrValidation)
	}

	return nil
}

func validateInstallTree(root *os.Root, tree stagedTree) error {
	for _, rel := range tree.dirs {
		if err := validateInstallTarget(root, rel, true); err != nil {
			return err
		}
	}

	for _, rel := range tree.files {
		if err := validateInstallTarget(root, rel, false); err != nil {
			return err
		}
	}

	return nil
}

type installTransaction struct {
	root        *os.Root
	stageName   string
	backupName  string
	createdDirs []string
	installed   []string
	backedUp    []string
}

func (tx *installTransaction) stagePath(rel string) string {
	return filepath.Join(tx.stageName, rel)
}

func (tx *installTransaction) backupPath(rel string) string {
	return filepath.Join(tx.backupName, rel)
}

func (tx *installTransaction) rollback() {
	for i := len(tx.installed) - 1; i >= 0; i-- {
		_ = tx.root.Remove(tx.installed[i])
	}

	for i := len(tx.backedUp) - 1; i >= 0; i-- {
		rel := tx.backedUp[i]
		_ = tx.root.Rename(tx.backupPath(rel), rel)
	}

	for i := len(tx.createdDirs) - 1; i >= 0; i-- {
		_ = tx.root.Remove(tx.createdDirs[i])
	}
}

func (tx *installTransaction) createDir(rel string) error {
	_, err := tx.root.Lstat(rel)
	if err == nil {
		return nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect artifact destination directory %q: %w", rel, err)
	}

	if err := tx.root.MkdirAll(rel, 0o755); err != nil {
		return fmt.Errorf("create artifact destination directory %q: %w", rel, err)
	}

	tx.createdDirs = append(tx.createdDirs, rel)

	return nil
}

func (tx *installTransaction) installFile(rel string) error {
	if err := tx.root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return fmt.Errorf("create artifact destination parent for %q: %w", rel, err)
	}

	_, statErr := tx.root.Lstat(rel)
	switch {
	case statErr == nil:
		backupPath := tx.backupPath(rel)
		if err := tx.root.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
			return fmt.Errorf("create artifact rollback parent for %q: %w", rel, err)
		}

		if err := tx.root.Rename(rel, backupPath); err != nil {
			return fmt.Errorf("back up artifact destination %q: %w", rel, err)
		}

		tx.backedUp = append(tx.backedUp, rel)
	case errors.Is(statErr, fs.ErrNotExist):
	default:
		return fmt.Errorf("inspect artifact destination %q before install: %w", rel, statErr)
	}

	if err := tx.root.Rename(tx.stagePath(rel), rel); err != nil {
		return fmt.Errorf("install artifact path %q: %w", rel, err)
	}

	tx.installed = append(tx.installed, rel)

	return nil
}

func (tx *installTransaction) install(tree stagedTree) error {
	for _, rel := range tree.dirs {
		if err := tx.createDir(rel); err != nil {
			tx.rollback()

			return err
		}
	}

	for _, rel := range tree.files {
		if err := tx.installFile(rel); err != nil {
			tx.rollback()

			return err
		}
	}
	// Check the composed tree, including preexisting target chains, through the
	// destination descriptor. Dangling links are allowed; escaping/cyclic ones are not.
	for _, rel := range tree.links {
		if _, err := tx.root.Stat(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
			tx.rollback()

			return fmt.Errorf("installed symlink %q is not confined: %w: %w", rel, err, errs.ErrValidation)
		}
	}

	return nil
}

// Install validates the complete staged tree and atomically renames each file
// into the destination. Existing files are restored if any install fails.
func (s *ArtifactStaging) Install() error {
	return s.install(false)
}

// InstallWithRelativeSymlinks retains tar's confined relative-link support.
// Other artifact consumers keep Install's regular-file-only policy.
func (s *ArtifactStaging) InstallWithRelativeSymlinks() error {
	return s.install(true)
}

func (s *ArtifactStaging) install(allowRelativeSymlinks bool) error {
	if s == nil || s.root == nil || s.stage == nil {
		return fmt.Errorf("artifact staging is closed: %w", errs.ErrUsage)
	}

	tree, err := inspectStagedTree(s.stage, allowRelativeSymlinks)
	if err != nil {
		return err
	}

	if validationErr := validateInstallTree(s.root, tree); validationErr != nil {
		return validationErr
	}

	backupName, err := mkdirPrivate(s.root, ".reusable-ci-artifact-backup-")
	if err != nil {
		return fmt.Errorf("create artifact rollback directory: %w", err)
	}

	defer func() { _ = s.root.RemoveAll(backupName) }()

	tx := installTransaction{root: s.root, stageName: s.stageName, backupName: backupName}

	return tx.install(tree)
}

func mkdirPrivate(root *os.Root, prefix string) (string, error) {
	for range 100 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}

		name := prefix + hex.EncodeToString(random[:])
		if err := root.Mkdir(name, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}

			return "", err
		}

		return name, nil
	}

	return "", fmt.Errorf("could not allocate private directory: %w", errs.ErrDependencyUnavailable)
}
