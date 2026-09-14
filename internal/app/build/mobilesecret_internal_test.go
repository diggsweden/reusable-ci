// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestDecodeMobileSecret_Contract pins the decoder every Android and Xcode
// secret goes through. Standard base64 wrapped with spaces, tabs, LF or CRLF
// decodes to the same bytes, up to exactly 16 MiB decoded. Empty decoded
// material, one byte over the limit, an encoded value over twice the limit
// (even a valid secret padded with whitespace, refused before decoding), the URL-safe alphabet and
// missing padding are malformed input with no bytes returned.
func TestDecodeMobileSecret_Contract(t *testing.T) {
	t.Parallel()

	secret := []byte("\x00keystore\xff/+ bytes")
	encoded := base64.StdEncoding.EncodeToString(secret)
	limit := bytes.Repeat([]byte{0xfb}, maxMobileSecretBytes)

	for _, tc := range []struct {
		name    string
		encoded string
		want    []byte
	}{
		{"single line", encoded, secret},
		{"wrapped with LF, CRLF, tabs and spaces", " " + encoded[:4] + "\n" + encoded[4:10] + "\r\n\t" + encoded[10:] + "  \n", secret},
		{"exactly the decoded limit", base64.StdEncoding.EncodeToString(limit), limit},
		{"decodes to nothing", " \r\n\t", nil},
		{"one byte over the decoded limit", base64.StdEncoding.EncodeToString(append(limit, 1)), nil},
		{"valid secret padded past twice the limit", encoded + strings.Repeat(" ", 2*maxMobileSecretBytes), nil},
		{"url-safe alphabet", base64.URLEncoding.EncodeToString(secret), nil},
		{"missing padding", base64.RawStdEncoding.EncodeToString([]byte("ab")), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeMobileSecret(tc.encoded)
			if tc.want == nil {
				if !errors.Is(err, errs.ErrMalformedInput) || got != nil {
					t.Fatalf("got %d bytes, err = %v, want malformed input and no bytes", len(got), err)
				}

				return
			}

			if err != nil || !bytes.Equal(got, tc.want) {
				t.Fatalf("got %d bytes, err = %v, want the %d original bytes", len(got), err, len(tc.want))
			}
		})
	}
}
