// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
)

// Digest computes a canonical, reproducible content digest over the regular
// files under root. It backs `reusable-ci artifact digest`.
//
// This is NOT the build -> sign hand-off digest, despite being the obvious
// candidate for it. That role belongs to release.DistDigest
// (`release dist-digest` / `release validate-dist`), which uses a different,
// shell-compatible scheme and is the one actually wired into a cross-job
// tamper-evidence channel: the producing job emits the digest as a job output,
// the caller passes it to the signing workflow as an input, and the signer
// re-derives and compares. The expected value travels the forge control plane
// rather than the artifact store, which is what makes it evidence rather than
// a checksum. See docs/flows.md.
//
// Reach for release.DistDigest when binding a hand-off. This function is the
// richer scheme (it commits to the execute bit and the size, which the
// sha256sum-compatible one cannot), available to adopters who want a strict
// content digest of a directory in their own flow.
//
// The scheme is a manifest hash, deliberately independent of any tar/filesystem
// quirk so the value is identical on every runner and OS:
//
//	for each regular file, sorted by its root-relative slash path P:
//	    write  "<P>\x00<mode>\x00<size>\x00<sha256(content)>\n"
//	digest = sha256(concatenation), rendered as 64 lowercase hex chars.
//
// <mode> is "0755" when any execute bit is set, else "0644" (so an executable
// release binary digests differently from a non-executable file of identical
// content). Directories, symlinks and other non-regular entries are excluded.
func Digest(fsys fs.FS, root string) (string, error) {
	files, err := regularFilesSorted(fsys, root)
	if err != nil {
		return "", err
	}

	manifest := sha256.New()
	prefix := strings.TrimSuffix(root, "/") + "/"

	for _, path := range files {
		info, err := fs.Stat(fsys, path)
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", path, err)
		}

		content, err := fileSHA256(fsys, path)
		if err != nil {
			return "", err
		}

		mode := "0644"
		if info.Mode()&0o111 != 0 {
			mode = "0755"
		}

		rel := strings.TrimPrefix(path, prefix)
		if root == "." {
			rel = path
		}

		_, _ = fmt.Fprintf(manifest, "%s\x00%s\x00%d\x00%s\n", rel, mode, info.Size(), content)
	}

	return hex.EncodeToString(manifest.Sum(nil)), nil
}

func regularFilesSorted(fsys fs.FS, root string) ([]string, error) {
	var files []string

	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		files = append(files, path)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	sort.Strings(files)

	return files, nil
}

func fileSHA256(fsys fs.FS, path string) (string, error) {
	file, err := fsys.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
