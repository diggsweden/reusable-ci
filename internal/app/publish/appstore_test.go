// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestEmitAppStoreUploadResult_EmitsRequestID(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("upload-result.json", []byte(`{"product-errors":[{"requestId":"req-123"}]}`))
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := apppublish.EmitAppStoreUploadResult(context.Background(), sink, &out, apppublish.AppStoreUploadResultInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("request-id"); got != "req-123" {
		t.Errorf("request-id = %q", got)
	}

	if !strings.Contains(out.String(), "Upload Request ID: req-123") {
		t.Errorf("out = %s", out.String())
	}
}

func TestEmitAppStoreUploadResult_MissingFileNoops(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	if err := apppublish.EmitAppStoreUploadResult(context.Background(), sink, &bytes.Buffer{}, apppublish.AppStoreUploadResultInput{Path: fsys.Path("missing.json")}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("request-id"); got != "" {
		t.Errorf("request-id = %q", got)
	}
}

func TestEmitAppStoreUploadResult_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("upload-result.json", []byte(`not json`))
	sink := fakeoutputsink.New(t)

	err := apppublish.EmitAppStoreUploadResult(context.Background(), sink, &bytes.Buffer{}, apppublish.AppStoreUploadResultInput{Path: path})
	if err == nil || !strings.Contains(err.Error(), "parse App Store upload result") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareAppStoreCredentials(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	private := []byte("-----BEGIN PRIVATE KEY-----\nbody\n-----END PRIVATE KEY-----\n")

	if err := apppublish.PrepareAppStoreCredentials(context.Background(), &bytes.Buffer{}, apppublish.AppStorePrepareCredentialsInput{
		Dir:           dir,
		KeyID:         "ABCD1234EF",
		PrivateKeyB64: base64.StdEncoding.EncodeToString(private),
	}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "AuthKey_ABCD1234EF.p8")

	got, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, private) {
		t.Errorf("decoded key mismatch:\n got: %q\nwant: %q", got, private)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestPrepareAppStoreCredentials_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	validKey := base64.StdEncoding.EncodeToString([]byte("private-key-body"))
	for _, tc := range []struct {
		name  string
		keyID string
		b64   string
		want  string
	}{
		{name: "missing key id", b64: validKey, want: "app store API key ID is required"},
		{name: "key id with path traversal", keyID: "../etc/passwd", b64: validKey, want: "not a valid"},
		{name: "key id with dot", keyID: "ABCD.EF", b64: validKey, want: "not a valid"},
		{name: "missing base64", keyID: "ABCDEF1234", want: "app store API private key is required"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "invalid base64", keyID: "ABCDEF1234", b64: "!!!not-base64!!!", want: "decode App Store API private key"},
		{name: "empty decoded", keyID: "ABCDEF1234", b64: "", want: "app store API private key is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := apppublish.PrepareAppStoreCredentials(context.Background(), &bytes.Buffer{}, apppublish.AppStorePrepareCredentialsInput{
				Dir:           t.TempDir(),
				KeyID:         tc.keyID,
				PrivateKeyB64: tc.b64,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}
