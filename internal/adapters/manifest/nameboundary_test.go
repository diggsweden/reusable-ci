// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package manifest_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestSink_NameBoundaryRefusesBeforeMarshalOrFilesystem(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			for _, name := range []string{
				"", "   ", ".", "..", "../canary", "../../canary", "nested/job", "sub/../canary",
				`..\canary`, `nested\job`, "absolute",
				"job\x00name", "job\tname", "job\nname", "job\rname", "job\x1bname",
				"job\x7fname", "job\u0085name", "job\xffname", "job\n",
			} {
				t.Run(name, func(t *testing.T) {
					for _, existing := range []bool{false, true} {
						outer := t.TempDir()
						root := filepath.Join(outer, "R")
						results := filepath.Join(root, "results")
						require.NoError(t, os.Mkdir(root, 0o700))

						if existing {
							require.NoError(t, os.MkdirAll(filepath.Join(results, "jobs"), 0o700))
							require.NoError(t, os.WriteFile(filepath.Join(results, "jobs", "keep.json"), []byte("keep"), 0o600))
						}

						for _, leaf := range []string{"canary.json", "canary-result.json"} {
							require.NoError(t, os.WriteFile(filepath.Join(outer, leaf), []byte("canary\n"), 0o600))
							require.NoError(t, os.WriteFile(filepath.Join(root, leaf), []byte("canary\n"), 0o600))
						}

						input := name
						if name == "absolute" {
							input = filepath.Join(outer, "canary")
						}

						before := resultTree(t, outer)
						body := &boundaryMarshaler{}
						err := writeBoundaryResult(t, manifest.New(results), kind, input, body)
						require.ErrorIs(t, err, errs.ErrUsage)
						require.NotErrorIs(t, err, errs.ErrValidation)
						require.Zero(t, body.calls, "invalid name invoked MarshalJSON")
						assertResultTree(t, outer, before)
					}
				})
			}
		})
	}
}

func TestSink_NameBoundaryPreservesDistinctFlatNamesAndReplacement(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			reference := filepath.Join(root, "reference")
			require.NoError(t, os.WriteFile(reference, nil, 0o644)) //nolint:gosec // owned reference measures permissions after the inherited umask.
			referenceInfo, referenceErr := os.Lstat(reference)
			require.NoError(t, referenceErr)

			results := filepath.Join(root, "results")
			sink := manifest.New(results)

			names := []string{"build", "build linux", "build-linux", "build_linux", "matrix (go=1.26, os=linux) [a+b]@v1:ok", "R\u00e4ksm\u00f6rg\u00e5s \u65e5\u672c", "v1..2", ".hidden", "-", " outer space "}
			for _, name := range names {
				for _, body := range []jsonBody{`{"old":true}`, `{"ok":true}`} {
					require.NoError(t, writeBoundaryResult(t, sink, kind, name, body))
				}

				path := resultFile(results, kind, name)
				got, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, resultBody(kind), string(got))

				info, err := os.Lstat(path)
				require.NoError(t, err)
				require.True(t, info.Mode().IsRegular())
				require.Equal(t, referenceInfo.Mode().Perm(), info.Mode().Perm(), "new-file permissions must honor umask")
			}

			dir := filepath.Dir(resultFile(results, kind, "build"))
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, len(names), "no name collisions or leftover staging files")

			if kind == "WriteJob" {
				docs, err := sink.CollectJobs(t.Context())
				require.NoError(t, err)
				require.Len(t, docs, len(names), "same content at different paths is not a collision")

				for _, doc := range docs {
					require.Equal(t, resultBody(kind), string(doc))
				}
			}
		})
	}
}

func TestSink_NameBoundaryPreservesCaseInSeparateRoots(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			for _, name := range []string{"Build", "build"} {
				// Separate roots check spelling without assuming case-sensitive storage.
				root := t.TempDir()
				require.NoError(t, writeBoundaryResult(t, manifest.New(root), kind, name, jsonBody(`{"ok":true}`)))
				path := resultFile(root, kind, name)
				entries, err := os.ReadDir(filepath.Dir(path))
				require.NoError(t, err)
				require.Len(t, entries, 1)
				require.Equal(t, filepath.Base(path), entries[0].Name())
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, resultBody(kind), string(body))
			}
		})
	}
}

func TestSink_NameBoundaryMarshalFailurePreservesCauseAndState(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Write", "WriteJSON", "WriteJob"} {
		t.Run(kind, func(t *testing.T) {
			for _, existing := range []bool{false, true} {
				root := t.TempDir()

				results := filepath.Join(root, "results")
				if existing {
					path := resultFile(results, kind, "build")
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
					require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
				}

				before := resultTree(t, root)
				cause := &os.PathError{Op: "marshal-fixture", Path: filepath.Join(root, "owned"), Err: os.ErrPermission}
				body := &boundaryMarshaler{err: cause}
				err := writeBoundaryResult(t, manifest.New(results), kind, "build", body)
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, os.ErrPermission)
				require.NotErrorIs(t, err, errs.ErrUsage)
				require.NotErrorIs(t, err, errs.ErrValidation)

				var pathErr *os.PathError
				require.ErrorAs(t, err, &pathErr)
				require.Same(t, cause, pathErr)

				if kind == "Write" {
					var marshalErr *json.MarshalerError
					require.ErrorAs(t, err, &marshalErr)
				}

				require.Equal(t, 1, body.calls)
				assertResultTree(t, root, before)
			}
		})
	}
}

type boundaryMarshaler struct {
	calls int
	err   error
}

func (b *boundaryMarshaler) MarshalJSON() ([]byte, error) {
	b.calls++

	return []byte(`{"ok":true}`), b.err
}

func writeBoundaryResult(t *testing.T, sink *manifest.Sink, kind, name string, body json.Marshaler) error {
	t.Helper()

	switch kind {
	case "Write":
		return sink.Write(t.Context(), name, map[string]any{"body": body})
	case "WriteJSON":
		return sink.WriteJSON(t.Context(), name, body)
	case "WriteJob":
		return sink.WriteJob(t.Context(), name, body)
	default:
		t.Fatalf("unknown writer %q", kind)

		return nil
	}
}

func resultFile(dir, kind, name string) string {
	if kind == "WriteJob" {
		return filepath.Join(dir, "jobs", name+".json")
	}

	return filepath.Join(dir, name+"-result.json")
}

func resultBody(kind string) string {
	if kind == "Write" {
		return "{\"body\":{\"ok\":true}}\n"
	}

	return "{\"ok\":true}\n"
}

type resultFileState struct {
	info os.FileInfo
	body string
	link string
}

func resultTree(t *testing.T, root string) map[string]resultFileState {
	t.Helper()

	handle, err := os.OpenRoot(root)
	require.NoError(t, err)

	defer func() { _ = handle.Close() }()

	tree := make(map[string]resultFileState)

	require.NoError(t, fs.WalkDir(handle.FS(), ".", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := handle.Lstat(path)
		require.NoError(t, err)

		state := resultFileState{info: info}
		if info.Mode().IsRegular() {
			body, readErr := handle.ReadFile(path)
			require.NoError(t, readErr)

			state.body = string(body)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			state.link, err = handle.Readlink(path)
			require.NoError(t, err)
		}

		tree[path] = state

		return nil
	}))

	return tree
}

func assertResultTree(t *testing.T, root string, before map[string]resultFileState) {
	t.Helper()
	after := resultTree(t, root)
	require.Len(t, after, len(before), "refused operation changed directory entries")

	for path, want := range before {
		got, ok := after[path]
		require.True(t, ok, "missing %s", path)
		require.Equal(t, want.body, got.body, path)
		require.Equal(t, want.link, got.link, path)
		require.Equal(t, want.info.Mode(), got.info.Mode(), path)
		require.True(t, os.SameFile(want.info, got.info), "identity changed: %s", path)
	}
}
