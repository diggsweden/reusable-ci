// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build unix

package manifest_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestSink_FilesystemBoundaryRefusesLinksAndNonregularTargets(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			for _, boundary := range []string{"root-link", "ancestor-link", "root-file", "jobs-link", "leaf-link", "confined-leaf-link", "dangling-leaf-link", "leaf-directory", "leaf-fifo"} {
				if boundary == "jobs-link" && kind != "WriteJob" {
					continue
				}

				t.Run(boundary, func(t *testing.T) {
					outer := t.TempDir()
					results := filepath.Join(outer, "results")
					outside := filepath.Join(outer, "outside")
					require.NoError(t, os.Mkdir(outside, 0o700))
					canary := filepath.Join(outside, filepath.Base(resultFile(results, kind, "build")))
					require.NoError(t, os.WriteFile(canary, []byte("canary\n"), 0o600))

					switch boundary {
					case "root-link", "ancestor-link":
						require.NoError(t, os.Symlink(outside, results))

						if boundary == "ancestor-link" {
							results = filepath.Join(results, "nested")
						}
					case "root-file":
						require.NoError(t, os.WriteFile(results, []byte("not a directory"), 0o600))
					case "jobs-link":
						require.NoError(t, os.Mkdir(results, 0o700))
						require.NoError(t, os.Symlink(outside, filepath.Join(results, "jobs")))
					default:
						path := resultFile(results, kind, "build")
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

						switch boundary {
						case "leaf-link":
							require.NoError(t, os.Symlink(canary, path))
						case "confined-leaf-link":
							require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "keep"), []byte("keep"), 0o600))
							require.NoError(t, os.Symlink("keep", path))
						case "dangling-leaf-link":
							require.NoError(t, os.Symlink(filepath.Join(outside, "missing"), path))
						case "leaf-directory":
							require.NoError(t, os.Mkdir(path, 0o700))
						case "leaf-fifo":
							require.NoError(t, syscall.Mkfifo(path, 0o600))
						}
					}

					before := resultTree(t, outer)
					err := writeBoundaryResult(t, manifest.New(results), kind, "build", jsonBody(`{"ok":true}`))
					require.ErrorIs(t, err, errs.ErrValidation)
					require.NotErrorIs(t, err, errs.ErrUsage)
					assertResultTree(t, outer, before)
				})
			}
		})
	}
}

func TestSink_FilesystemBoundaryReplacementDoesNotTruncateHardlink(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			for _, hardlink := range []bool{false, true} {
				outer := t.TempDir()
				results := filepath.Join(outer, "results")
				canary := filepath.Join(outer, "canary")
				require.NoError(t, os.WriteFile(canary, []byte("original\n"), 0o600))
				require.NoError(t, os.Chmod(canary, 0o600))
				before, err := os.Lstat(canary)
				require.NoError(t, err)

				path := resultFile(results, kind, "build")
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

				if hardlink {
					require.NoError(t, os.Link(canary, path))
				} else {
					require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o600))
					require.NoError(t, os.Chmod(path, 0o600))
				}

				linked, err := os.Lstat(path)
				require.NoError(t, err)
				require.Equal(t, hardlink, os.SameFile(before, linked))

				sink := manifest.New(results)
				require.NoError(t, writeBoundaryResult(t, sink, kind, "build", jsonBody(`{"ok":true}`)))

				body, err := os.ReadFile(canary)
				require.NoError(t, err)
				require.Equal(t, "original\n", string(body))

				after, err := os.Lstat(canary)
				require.NoError(t, err)
				require.Equal(t, before.Mode(), after.Mode())
				require.True(t, os.SameFile(before, after))

				replacement, err := os.Lstat(path)
				require.NoError(t, err)
				require.True(t, replacement.Mode().IsRegular())
				require.Equal(t, os.FileMode(0o600), replacement.Mode().Perm(), "replacement must preserve existing permissions")
				require.False(t, os.SameFile(after, replacement))

				body, err = os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, resultBody(kind), string(body))

				entries, err := os.ReadDir(filepath.Dir(path))
				require.NoError(t, err)
				require.Len(t, entries, 1, "no staging leftovers")
			}
		})
	}
}

func TestSink_CollectionBoundaryRefusesSymlinksAndNonregularFiles(t *testing.T) {
	t.Parallel()

	for _, boundary := range []string{"root-file", "jobs-file", "root-link", "ancestor-link", "jobs-link", "leaf-link", "confined-leaf-link", "dangling-leaf-link", "leaf-fifo"} {
		t.Run(boundary, func(t *testing.T) {
			outer := t.TempDir()
			results := filepath.Join(outer, "results")
			outside := filepath.Join(outer, "outside")
			require.NoError(t, os.MkdirAll(filepath.Join(outside, "jobs"), 0o700))
			canary := filepath.Join(outside, "canary.json")
			require.NoError(t, os.WriteFile(canary, []byte("outside bytes"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(outside, "jobs", "canary.json"), []byte("outside job bytes"), 0o600))

			switch boundary {
			case "root-file":
				require.NoError(t, os.WriteFile(results, []byte("not a directory"), 0o600))
			case "jobs-file":
				require.NoError(t, os.Mkdir(results, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(results, "jobs"), []byte("not a directory"), 0o600))
			case "root-link", "ancestor-link":
				require.NoError(t, os.Symlink(outside, results))

				if boundary == "ancestor-link" {
					results = filepath.Join(results, "nested")
				}
			case "jobs-link":
				require.NoError(t, os.Mkdir(results, 0o700))
				require.NoError(t, os.Symlink(outside, filepath.Join(results, "jobs")))
			default:
				jobs := filepath.Join(results, "jobs")
				require.NoError(t, os.MkdirAll(jobs, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(jobs, "a.json"), []byte("valid first record"), 0o600))
				path := filepath.Join(jobs, "z.json")

				switch boundary {
				case "leaf-link":
					require.NoError(t, os.Symlink(canary, path))
				case "confined-leaf-link":
					require.NoError(t, os.Symlink("a.json", path))
				case "dangling-leaf-link":
					require.NoError(t, os.Symlink(filepath.Join(outside, "missing"), path))
				case "leaf-fifo":
					require.NoError(t, syscall.Mkfifo(path, 0o600))
				}
			}

			before := resultTree(t, outer)
			docs, err := manifest.New(results).CollectJobs(t.Context())
			require.ErrorIs(t, err, errs.ErrValidation)
			require.NotErrorIs(t, err, errs.ErrUsage)
			require.NotErrorIs(t, err, os.ErrNotExist)
			require.Nil(t, docs, "no partial collection on refusal")
			assertResultTree(t, outer, before)
		})
	}
}

func TestSink_CollectionBoundaryIsFlatRawAndMissingDirectoryIsNil(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results := filepath.Join(root, "results")
	sink := manifest.New(results)

	for range 2 {
		before := resultTree(t, root)
		docs, err := sink.CollectJobs(t.Context())
		require.NoError(t, err)
		require.Nil(t, docs)
		assertResultTree(t, root, before)
		require.NoError(t, os.MkdirAll(results, 0o700))
	}

	jobs := filepath.Join(results, "jobs")
	require.NoError(t, os.MkdirAll(filepath.Join(jobs, "nested.json"), 0o700))

	for name, body := range map[string]string{"z.json": "same raw bytes\n", "a.json": "same raw bytes\n", "ignore.txt": "ignore", "nested.json/hidden.json": "ignore nested"} {
		require.NoError(t, os.WriteFile(filepath.Join(jobs, name), []byte(body), 0o600))
	}

	before := resultTree(t, root)
	docs, err := sink.CollectJobs(t.Context())
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte("same raw bytes\n"), []byte("same raw bytes\n")}, docs)
	assertResultTree(t, root, before)
}

func TestSink_CollectionBoundaryBoundsEachRegularRead(t *testing.T) {
	t.Parallel()

	for _, size := range []int64{64 << 20, (64 << 20) + 1} {
		root := t.TempDir()
		jobs := filepath.Join(root, "jobs")
		require.NoError(t, os.Mkdir(jobs, 0o700))
		path := filepath.Join(jobs, "large.json")
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(size))
		_, err = file.WriteAt([]byte("Z"), size-1)
		require.NoError(t, err)
		require.NoError(t, file.Close())

		before, err := os.Lstat(path)
		require.NoError(t, err)

		docs, err := manifest.New(root).CollectJobs(t.Context())
		if size == 64<<20 {
			require.NoError(t, err)
			require.Len(t, docs, 1)
			require.Len(t, docs[0], 64<<20)
			require.Equal(t, byte('Z'), docs[0][len(docs[0])-1])
		} else {
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, docs)
		}

		after, err := os.Lstat(path)
		require.NoError(t, err)
		require.Equal(t, before.Size(), after.Size())
		require.Equal(t, before.Mode(), after.Mode())
		require.True(t, os.SameFile(before, after))
	}
}
