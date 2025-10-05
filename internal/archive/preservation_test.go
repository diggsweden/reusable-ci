// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"github.com/diggsweden/reusable-ci/v3/internal/archive"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTarTransaction_RefusalsPreserveOriginalBytes(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"traversal", "later-hardlink", "truncated", "composed-link-escape", "success"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			require.NoError(t, os.Mkdir(dest, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dest, "original"), []byte("old"), 0o600))
			archivePath := filepath.Join(root, "input.tgz")
			entries := []tarEntry{{hdr: tar.Header{Name: "package/original", Typeflag: tar.TypeReg, Mode: 0o600}, body: "new"}}

			switch kind {
			case "traversal":
				entries = append(entries, tarEntry{hdr: tar.Header{Name: "package/../outside", Typeflag: tar.TypeReg}, body: "bad"})
			case "later-hardlink":
				entries = append(entries, tarEntry{hdr: tar.Header{Name: "package/link", Typeflag: tar.TypeLink, Linkname: "original"}})
			case "composed-link-escape":
				require.NoError(t, os.Symlink(root, filepath.Join(dest, "existing")))

				entries = append(entries, tarEntry{hdr: tar.Header{Name: "package/new-link", Typeflag: tar.TypeSymlink, Linkname: "existing/input.tgz"}})
			}

			if kind == "truncated" {
				var raw, compressed bytes.Buffer

				tw := tar.NewWriter(&raw)
				require.NoError(t, tw.WriteHeader(&tar.Header{Name: "package/original", Typeflag: tar.TypeReg, Size: 9, Mode: 0o600}))
				_, err := tw.Write([]byte("xx"))
				require.NoError(t, err)
				require.Error(t, tw.Close())

				gz := gzip.NewWriter(&compressed)
				_, err = gz.Write(raw.Bytes())
				require.NoError(t, err)
				require.NoError(t, gz.Close())
				require.NoError(t, os.WriteFile(archivePath, compressed.Bytes(), 0o600))
			} else {
				writeTarball(t, archivePath, entries)
			}

			err := archive.UntarStripOne(archivePath, dest)
			want := "old"

			switch kind {
			case "success":
				require.NoError(t, err)

				want = "new"
			case "truncated":
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			default:
				require.ErrorIs(t, err, errs.ErrValidation)
			}

			body, err := os.ReadFile(filepath.Join(dest, "original"))
			require.NoError(t, err)
			require.Equal(t, want, string(body))

			files, err := os.ReadDir(dest)
			require.NoError(t, err)

			count := 1
			if kind == "composed-link-escape" {
				count = 2
			}

			require.Len(t, files, count)
		})
	}
}
