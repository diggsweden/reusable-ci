// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
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
		t.Fatalf("err = %v, want the parse failure", err)
	}

	// Asserted by message rather than sentinel because
	// domain/publish.ParseAppStoreUploadRequestID wraps only encoding/json's
	// error: this refusal carries no errs.* class today, so it exits 70
	// ("file a bug") for what is really malformed tool output.

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q for an unparsable result file", got)
	}
}

func TestPrepareAppStoreCredentials_WritesTheDecodedKeyOwnerOnly(t *testing.T) {
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

	// The two classes are not interchangeable: an absent secret is
	// something the operator adds to the forge (ErrMissingInput, exit 66),
	// while a secret that is present but unusable is bad data (exit 65).
	for _, tc := range []struct {
		name    string
		keyID   string
		b64     string
		want    string
		wantErr error
	}{
		{name: "missing key id", b64: validKey, want: "app store API key ID is required", wantErr: errs.ErrMissingInput},
		{name: "key id with path traversal", keyID: "../etc/passwd", b64: validKey, want: "not a valid", wantErr: errs.ErrValidation},
		{name: "key id with dot", keyID: "ABCD.EF", b64: validKey, want: "not a valid", wantErr: errs.ErrValidation},
		{name: "missing base64", keyID: "ABCDEF1234", want: "app store API private key is required", wantErr: errs.ErrMissingInput}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "invalid base64", keyID: "ABCDEF1234", b64: "!!!not-base64!!!", want: "decode App Store API private key", wantErr: errs.ErrValidation},
		{name: "empty decoded", keyID: "ABCDEF1234", b64: "\n", want: "decoded", wantErr: errs.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			err := apppublish.PrepareAppStoreCredentials(context.Background(), &bytes.Buffer{}, apppublish.AppStorePrepareCredentialsInput{
				Dir:           dir,
				KeyID:         tc.keyID,
				PrivateKeyB64: tc.b64,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}

			// A refused credential must leave nothing on disk: the key is
			// written owner-only, and a partial write is still a key.
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Errorf("a refused run left %v behind", entries)
			}
		})
	}
}

// TestPrepareAppStoreCredentials_NeverEchoesTheKeyMaterial covers what this
// step says while it writes an App Store Connect signing key to disk.
//
// The existing test passes a bytes.Buffer as the writer and never reads it, so
// what the step prints is unasserted on every path. The private key is the
// credential that signs and uploads builds to Apple, and the issuer and key ID
// identify the account it belongs to. A base64 decode failure is the likeliest
// place for the encoded key to be quoted back, because the error comes from
// encoding/base64 with the input in hand.
//
// The file itself is excluded: writing the key there is the whole point. What
// must never carry it is stdout or the returned error.
func TestPrepareAppStoreCredentials_NeverEchoesTheKeyMaterial(t *testing.T) {
	t.Parallel()

	const canaryKeyBody = "CANARY-APPSTORE-PRIVATE-KEY-8b41f7c2"

	private := []byte("-----BEGIN PRIVATE KEY-----\n" + canaryKeyBody + "\n-----END PRIVATE KEY-----\n")
	encoded := base64.StdEncoding.EncodeToString(private)

	for _, tc := range []struct {
		name    string
		in      apppublish.AppStorePrepareCredentialsInput
		wantErr bool
	}{
		{
			name: "a successful preparation",
			in: apppublish.AppStorePrepareCredentialsInput{
				KeyID: "ABCD1234EF", PrivateKeyB64: encoded,
			},
		},
		{
			name: "the key is not valid base64",
			in: apppublish.AppStorePrepareCredentialsInput{
				KeyID:         "ABCD1234EF",
				PrivateKeyB64: "not-base64-" + canaryKeyBody + "!!",
			},
			wantErr: true,
		},
		{
			name: "the key decodes to nothing",
			in: apppublish.AppStorePrepareCredentialsInput{
				KeyID: "ABCD1234EF", PrivateKeyB64: "",
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			in := tc.in
			in.Dir = t.TempDir()

			err := apppublish.PrepareAppStoreCredentials(context.Background(), &out, in)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			surfaces := map[string]string{"stdout": out.String()}
			if err != nil {
				surfaces["error"] = err.Error()
			}

			for name, body := range surfaces {
				for _, canary := range []string{canaryKeyBody, encoded} {
					if strings.Contains(body, canary) {
						t.Errorf("%s leaked App Store credential material:\n%s", name, body)
					}
				}
			}

			// The key ID is not secret — it names the file on disk and an
			// operator needs it to debug — so a blanket redaction cannot
			// satisfy the assertions above.
			if !tc.wantErr {
				if _, statErr := os.Stat(filepath.Join(in.Dir, "AuthKey_ABCD1234EF.p8")); statErr != nil {
					t.Errorf("the key file was not written: %v", statErr)
				}
			}
		})
	}
}
