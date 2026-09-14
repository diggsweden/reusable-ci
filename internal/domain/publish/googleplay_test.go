// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

func TestRenderGooglePlayUploadSummary_RendersMetadataLiterally(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, text, aab, aabCode string }{
		{"heading injection", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED ", "demo\n\n## B8-INJECTED\n", "<code>bundle-demo  &#35;&#35; B8-INJECTED </code>"},
		{"inline syntax", "[link](https://evil.invalid) <b>&amp;</b> \\| `` *_~\t\r\n", "&#91;link&#93;(https&#58;//evil.invalid) &#60;b&#62;&#38;amp;&#60;/b&#62; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   ", "[link](target) <b>& \\| `` *_~\t\r\n", "<code>bundle-&#91;link&#93;(target) &#60;b&#62;&#38; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   </code>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
				AABFile: "private-parent-canary/bundle-" + tc.aab, PackageName: "package-" + tc.raw,
				Track: "track-" + tc.raw, Status: "status-" + tc.raw, ReleaseName: "release-" + tc.raw,
			}, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

			want := fmt.Sprintf("## Google Play Upload Summary\n\n### Upload Details\n| Property | Value |\n|----------|-------|\n| **AAB File** | %s |\n| **Package** | <code>package-%s</code> |\n| **Track** | track-%s |\n| **Status** | status-%s |\n| **Release Name** | release-%s |\n| **Upload Status** | Uploaded |\n\n### Next Steps\n1. Check [Google Play Console](https://play.google.com/console) for upload status\n\n*Upload completed at 2026-05-10 14:00:00 UTC*\n", tc.aabCode, tc.text, tc.text, tc.text, tc.text)
			if got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestRenderGooglePlayUploadSummary_RawBranchAndReleaseNameDecisions(t *testing.T) {
	t.Parallel()

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "build/Demo.aab", PackageName: "se.digg.demo",
		Track: "production\r\n", Status: "draft\t", ReleaseName: "\t\r\n",
	}, time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC))

	want := "## Google Play Upload Summary\n\n### Upload Details\n| Property | Value |\n|----------|-------|\n" +
		"| **AAB File** | `Demo.aab` |\n| **Package** | `se.digg.demo` |\n" +
		"| **Track** | production   |\n| **Status** | draft  |\n| **Release Name** |     |\n" +
		"| **Upload Status** | Uploaded |\n\n### Next Steps\n" +
		"1. Check [Google Play Console](https://play.google.com/console) for upload status\n\n" +
		"*Upload completed at 2026-05-10 14:00:00 UTC*\n"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestRenderGooglePlayUploadSummary_ProductionWithStagedRollout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile:         "build/release/Demo.aab",
		PackageName:     "se.digg.demo",
		Track:           "production",
		Status:          "completed", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseName:     "1.2.3 (42)",
		UserFraction:    0.1,
		UserFractionSet: true,
		Priority:        3,
	}, now)
	for _, want := range []string{
		"## Google Play Upload Summary",
		"| **AAB File** | `Demo.aab` |",
		"| **Package** | `se.digg.demo` |",
		"| **Track** | production |",
		"| **Status** | completed |",
		"| **Release Name** | 1.2.3 (42) |",
		"| **Staged Rollout** | 10% |",
		"| **Update Priority** | 3 |",
		"| **Upload Status** | Uploaded |",
		"2. Staged rollout to 10% of users will begin after review",
		"*Upload completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderGooglePlayUploadSummary_DraftAddsManualStep3(t *testing.T) {
	t.Parallel()

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "internal", Status: "draft", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "3. Release is saved as draft - manually publish from Play Console when ready") {
		t.Errorf("missing draft step:\n%s", got)
	}

	if !strings.Contains(got, "2. Build will be available to internal testers within minutes") {
		t.Errorf("missing internal next-step:\n%s", got)
	}
}

func TestRenderGooglePlayUploadSummary_AlphaBetaShowTrackName(t *testing.T) {
	t.Parallel()

	for _, track := range []string{"alpha", "beta"} {
		got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
			AABFile: "Demo.aab", PackageName: "p", Track: track, Status: "completed",
		}, time.Now())

		want := "2. Build will be available to " + track + " testers after review"
		if !strings.Contains(got, want) {
			t.Errorf("track %s: missing %q in:\n%s", track, want, got)
		}
	}
}

func TestRenderGooglePlayUploadSummary_ProductionFullReleaseWhenNoFraction(t *testing.T) {
	t.Parallel()

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "production", Status: "completed",
	}, time.Now())
	if !strings.Contains(got, "2. Full production release will begin after review") {
		t.Errorf("missing full-release step:\n%s", got)
	}

	if strings.Contains(got, "Staged Rollout") {
		t.Errorf("did not expect Staged Rollout row when fraction unset:\n%s", got)
	}
}

func TestRenderGooglePlayUploadSummary_OmitsOptionalRows(t *testing.T) {
	t.Parallel()

	got := publish.RenderGooglePlayUploadSummary(publish.GooglePlayUploadInput{
		AABFile: "Demo.aab", PackageName: "p", Track: "internal", Status: "completed",
	}, time.Now())
	if strings.Contains(got, "Release Name") {
		t.Errorf("expected no Release Name row:\n%s", got)
	}

	if strings.Contains(got, "Update Priority") {
		t.Errorf("expected no Update Priority row when zero:\n%s", got)
	}
}

func TestValidateGooglePlayServiceAccount_Accepts(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]string{"type": "service_account", "client_email": "play-publisher@example.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))})
	if err != nil {
		t.Fatal(err)
	}

	if err := publish.ValidateGooglePlayServiceAccount(body); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGooglePlayServiceAccount_Rejects(t *testing.T) {
	t.Parallel()

	// Each row also pins its exit class, because the three are different
	// answers to the operator: nothing supplied at all is a broken invocation,
	// a key of the wrong kind is a domain-rule failure, and content that will
	// not parse or is missing a field is malformed data.
	for _, tc := range []struct {
		name    string
		body    string
		want    string
		wantErr error
	}{
		{name: "empty body", body: "", want: "is empty", wantErr: errs.ErrUsage},
		{name: "not json", body: "not json", want: "parse service account JSON", wantErr: errs.ErrMalformedInput},
		{name: "wrong type", body: `{"type":"user","client_email":"x@y","private_key":"k"}`, want: `want "service_account"`, wantErr: errs.ErrValidation},
		{name: "missing client_email", body: `{"type":"service_account","private_key":"k"}`, want: "missing client_email", wantErr: errs.ErrMalformedInput},
		{name: "missing private_key", body: `{"type":"service_account","client_email":"x@y"}`, want: "missing private_key", wantErr: errs.ErrMalformedInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := publish.ValidateGooglePlayServiceAccount([]byte(tc.body))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want substring %q", err, tc.want)
			}

			// A secret must never be echoed back in the refusal.
			if tc.body != "" && strings.Contains(err.Error(), tc.body) {
				t.Errorf("the rejection echoed the service account body: %v", err)
			}
		})
	}
}

// TestValidateGooglePlayServiceAccount_KeyShapes covers the private_key
// framing and the key kind with keys generated here. Trailing data after
// the PEM block, a PEM header, an EC key and an RSA key whose components do
// not agree are each malformed input, and a whitespace-only client_email is
// missing; the same RSA key framed properly is the control.
func TestValidateGooglePlayServiceAccount_KeyShapes(t *testing.T) {
	t.Parallel()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pkcs8 := func(key any) []byte {
		der, marshalErr := x509.MarshalPKCS8PrivateKey(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}

		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	}

	rsaDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}

	body := func(email, privateKey string) []byte {
		encoded, marshalErr := json.Marshal(map[string]string{"type": "service_account", "client_email": email, "private_key": privateKey})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}

		return encoded
	}

	const email = "play-publisher@example.iam.gserviceaccount.com"

	if err := publish.ValidateGooglePlayServiceAccount(body(email, string(pkcs8(rsaKey)))); err != nil {
		t.Fatalf("control key refused: %v", err)
	}

	for name, testCase := range map[string]struct {
		email string
		key   string
		want  string
	}{
		"trailing data":     {email: email, key: string(pkcs8(rsaKey)) + "trailing\n", want: "must be PKCS8 PEM"},
		"pem header":        {email: email, key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: rsaDER})), want: "must be PKCS8 PEM"},
		"ec key":            {email: email, key: string(pkcs8(ecKey)), want: "valid RSA private key"},
		"inconsistent rsa":  {email: email, key: string(inconsistentRSAPKCS8(t, rsaKey)), want: "private_key must contain a valid"},
		"blank client mail": {email: " \n\t", key: string(pkcs8(rsaKey)), want: "missing client_email"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := publish.ValidateGooglePlayServiceAccount(body(testCase.email, testCase.key))
			if !errors.Is(err, errs.ErrMalformedInput) || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want ErrMalformedInput naming %q", err, testCase.want)
			}
		})
	}
}

// inconsistentRSAPKCS8 frames rsaKey's modulus with a private exponent that
// does not invert the public one. Go's own marshaller refuses such a key, so
// the DER is assembled directly: the parser then fails rsa.Validate, which is
// the refusal the RSA check exists for.
func inconsistentRSAPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	pkcs1, err := asn1.Marshal(struct {
		Version                     int
		N, E, D, P, Q, Dp, Dq, Qinv *big.Int
	}{
		N: key.N, E: big.NewInt(int64(key.E)), D: new(big.Int).Add(key.D, big.NewInt(2)),
		P: key.Primes[0], Q: key.Primes[1], Dp: key.Precomputed.Dp, Dq: key.Precomputed.Dq, Qinv: key.Precomputed.Qinv,
	})
	if err != nil {
		t.Fatal(err)
	}

	der, err := asn1.Marshal(struct {
		Version    int
		Algorithm  pkix.AlgorithmIdentifier
		PrivateKey []byte
	}{
		Algorithm:  pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}, Parameters: asn1.NullRawValue},
		PrivateKey: pkcs1,
	})
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}
