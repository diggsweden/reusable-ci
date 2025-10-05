// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/archive"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestTarStreamBoundary_IntegrityBeforeInstall(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"valid", "crc", "truncated-trailer", "padding-at-cap", "padding-over-cap"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			require.NoError(t, os.Mkdir(dest, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dest, "value"), []byte("old"), 0o600))

			var raw, compressed bytes.Buffer

			tw := tar.NewWriter(&raw)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "package/value", Typeflag: tar.TypeReg, Mode: 0o600, Size: 3}))
			_, err := tw.Write([]byte("new"))
			require.NoError(t, err)
			require.NoError(t, tw.Close())

			gz := gzip.NewWriter(&compressed)
			_, err = gz.Write(raw.Bytes())
			require.NoError(t, err)

			padding := 0
			if kind == "padding-at-cap" {
				padding = 1 << 20
			}

			if kind == "padding-over-cap" {
				padding = 1<<20 + 1
			}

			_, err = gz.Write(make([]byte, padding))
			require.NoError(t, err)
			require.NoError(t, gz.Close())

			body := compressed.Bytes()

			var wantErr error

			switch kind {
			case "crc":
				body[len(body)-8] ^= 1
				wantErr = gzip.ErrChecksum
			case "truncated-trailer":
				body = body[:len(body)-4]
				wantErr = io.ErrUnexpectedEOF
			case "padding-over-cap":
				wantErr = errs.ErrValidation
			}

			input := filepath.Join(root, "input.tgz")
			require.NoError(t, os.WriteFile(input, body, 0o600))

			err = archive.UntarStripOne(input, dest)
			if !errors.Is(err, wantErr) {
				t.Fatalf("err=%v want=%v", err, wantErr)
			}

			want := "new"
			if wantErr != nil {
				want = "old"
			}

			got, err := os.ReadFile(filepath.Join(dest, "value"))
			require.NoError(t, err)
			require.Equal(t, want, string(got))

			files, err := os.ReadDir(dest)
			require.NoError(t, err)
			require.Len(t, files, 1)
		})
	}
}

func TestTarLinkBoundary_ForwardRelativeAndExistingBridge(t *testing.T) {
	t.Parallel()

	for _, escape := range []bool{false, true} {
		root := t.TempDir()
		dest := filepath.Join(root, "dest")
		require.NoError(t, os.Mkdir(dest, 0o700))

		outside := filepath.Join(root, "outside")
		require.NoError(t, os.Mkdir(outside, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(outside, "value"), []byte("outside-canary"), 0o600))

		entries := []tarEntry{
			{hdr: tar.Header{Name: "package/alias", Typeflag: tar.TypeSymlink, Linkname: "bridge/value"}},
		}

		if escape {
			require.NoError(t, os.Symlink(outside, filepath.Join(dest, "bridge")))
		} else {
			entries = append(entries, tarEntry{hdr: tar.Header{Name: "package/bridge/", Typeflag: tar.TypeDir, Mode: 0o700}}, tarEntry{hdr: tar.Header{Name: "package/bridge/value", Typeflag: tar.TypeReg, Mode: 0o600}, body: "inside"}, tarEntry{hdr: tar.Header{Name: "package/bridge/back", Typeflag: tar.TypeSymlink, Linkname: "../alias"}})
		}

		input := filepath.Join(root, "input.tgz")
		writeTarball(t, input, entries)

		err := archive.UntarStripOne(input, dest)
		if escape {
			require.ErrorIs(t, err, errs.ErrValidation)

			_, statErr := os.Lstat(filepath.Join(dest, "alias"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		} else {
			require.NoError(t, err)

			for _, name := range []string{"alias", "bridge/back"} {
				body, readErr := os.ReadFile(filepath.Join(dest, name))
				require.NoError(t, readErr)
				require.Equal(t, "inside", string(body))
			}
		}

		body, err := os.ReadFile(filepath.Join(outside, "value"))
		require.NoError(t, err)
		require.Equal(t, "outside-canary", string(body))
	}
}
