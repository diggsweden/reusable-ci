// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Named so the propagation assertions below can use errors.Is rather than
// matching a message the fake happens to use.
var errMissingSBOMAttestation = errors.New("missing sbom attestation")

// errSignatureMismatch stands in for a verifier refusing a signature. Held as
// a package-level value because the tests assert the cause survives the wrap
// (errors.Is), which is exactly what a dynamically built error cannot support.
var errSignatureMismatch = errors.New("signature mismatch")

const releaseImagePublicKey = ".forgejo/keys/cosign.pub"

const releaseImageDigestRef = "codeberg.org/itiquette/nanolinter:v1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestVerifyExistingReleaseImage_Verified(t *testing.T) {
	verifier := &fakeReleaseImageVerifier{payload: releaseImagePayload(t, releaseImageFields{
		tag: "v1", commit: "cafe1234", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml",
	})}

	var out bytes.Buffer

	result, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, nil, &out, releaseImageVerifyInput())
	if err != nil {
		t.Fatalf("VerifyExistingReleaseImage: %v", err)
	}

	if result.Status != appcontainer.ReleaseImageStatusVerified {
		t.Fatalf("status = %q, want %q", result.Status, appcontainer.ReleaseImageStatusVerified)
	}

	// Each request is compared whole: a verifier asked about the right
	// predicate on another image, or against another key, proves nothing
	// about this release.
	want := []any{
		cosign.VerifyImageInput{ImageRef: releaseImageDigestRef, KeyRef: releaseImagePublicKey},
		cosign.VerifyAttestationInput{ImageRef: releaseImageDigestRef, PredicateType: "cyclonedx", KeyRef: releaseImagePublicKey},
		cosign.VerifyAttestationInput{ImageRef: releaseImageDigestRef, PredicateType: "slsaprovenance1", KeyRef: releaseImagePublicKey},
	}
	if !slices.Equal(verifier.requests, want) {
		t.Errorf("requests =\n%+v\nwant\n%+v", verifier.requests, want)
	}
}

func TestVerifyExistingReleaseImage_VerifiedWithBaseLineage(t *testing.T) {
	baseRef := "codeberg.org/itiquette/nanolinter-base@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseInputID := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	input := releaseImageVerifyInput()
	input.ExpectedBaseRef = baseRef
	input.ExpectedBaseInputID = baseInputID
	verifier := &fakeReleaseImageVerifier{payload: releaseImagePayload(t, releaseImageFields{
		tag: "v1", commit: "cafe1234", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml", baseRef: baseRef, baseInputID: baseInputID,
	})}

	result, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, nil, io.Discard, input)
	if err != nil {
		t.Fatalf("VerifyExistingReleaseImage: %v", err)
	}

	if result.Status != appcontainer.ReleaseImageStatusVerified {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestVerifyExistingReleaseImage_ReattestableWhenProvenanceMismatches(t *testing.T) {
	verifier := &fakeReleaseImageVerifier{payload: releaseImagePayload(t, releaseImageFields{
		tag: "other", commit: "other", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml",
	})}
	registry := &fakeReleaseImageLabels{labels: map[string]string{
		"org.opencontainers.image.revision": "cafe1234",
		"org.opencontainers.image.version":  "v1",
		"org.opencontainers.image.ref.name": "v1",
		"org.opencontainers.image.source":   "https://CODEBERG.org/itiquette/nanolinter",
	}}
	input := releaseImageVerifyInput()
	input.AllowReattest = true

	result, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, registry, io.Discard, input)
	if err != nil {
		t.Fatalf("VerifyExistingReleaseImage: %v", err)
	}

	if result.Status != appcontainer.ReleaseImageStatusReattestable {
		t.Fatalf("status = %q, want %q", result.Status, appcontainer.ReleaseImageStatusReattestable)
	}

	if registry.ref != "codeberg.org/itiquette/nanolinter@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("labels ref = %q", registry.ref)
	}

	// The full check runs first; the re-attestation precheck then verifies
	// the signature and SBOM again, never the provenance it is replacing.
	signature := cosign.VerifyImageInput{ImageRef: releaseImageDigestRef, KeyRef: releaseImagePublicKey}
	sbom := cosign.VerifyAttestationInput{ImageRef: releaseImageDigestRef, PredicateType: "cyclonedx", KeyRef: releaseImagePublicKey}

	want := []any{
		signature, sbom,
		cosign.VerifyAttestationInput{ImageRef: releaseImageDigestRef, PredicateType: "slsaprovenance1", KeyRef: releaseImagePublicKey},
		signature, sbom,
	}
	if !slices.Equal(verifier.requests, want) {
		t.Errorf("requests =\n%+v\nwant\n%+v", verifier.requests, want)
	}
}

func TestVerifyExistingReleaseImage_ReattestRejectsWrongIdentity(t *testing.T) {
	verifier := &fakeReleaseImageVerifier{payload: releaseImagePayload(t, releaseImageFields{
		tag: "other", commit: "other", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml",
	})}
	registry := &fakeReleaseImageLabels{labels: map[string]string{
		"org.opencontainers.image.revision": "other",
		"org.opencontainers.image.version":  "v1",
		"org.opencontainers.image.ref.name": "v1",
		"org.opencontainers.image.source":   "https://codeberg.org/itiquette/nanolinter",
	}}
	input := releaseImageVerifyInput()
	input.AllowReattest = true

	_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, registry, io.Discard, input)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation — reattesting an image whose labels name another build is a refusal, not a retry", err)
	}
}

func TestVerifyExistingReleaseImage_ReattestRequiresSignedSBOM(t *testing.T) {
	verifier := &fakeReleaseImageVerifier{
		payload:   releaseImagePayload(t, releaseImageFields{tag: "other", commit: "other", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml"}),
		attestErr: errMissingSBOMAttestation,
	}
	input := releaseImageVerifyInput()
	input.AllowReattest = true

	_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, &fakeReleaseImageLabels{}, io.Discard, input)

	// The cause survives the wrap, as in VerifierFailureIsValidation: an
	// operator has to be able to tell "no signed SBOM to carry forward"
	// from any other reattest refusal.
	if !errors.Is(err, errMissingSBOMAttestation) {
		t.Fatalf("err = %v, want the SBOM precheck failure", err)
	}
}

func releaseImageVerifyInput() appcontainer.ReleaseImageVerifyExistingInput {
	return appcontainer.ReleaseImageVerifyExistingInput{
		Ref:              releaseImageDigestRef,
		CosignPublicKey:  releaseImagePublicKey,
		ExpectedTag:      "v1",
		ExpectedCommit:   "cafe1234",
		ExpectedSource:   "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow: ".forgejo/workflows/release.yml",
	}
}

func TestVerifyExistingReleaseImage_VerifierFailureIsValidation(t *testing.T) {
	t.Parallel()

	verifyErr := errSignatureMismatch

	_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), &fakeReleaseImageVerifier{verifyErr: verifyErr}, &fakeReleaseImageLabels{}, io.Discard, releaseImageVerifyInput())
	if !errors.Is(err, verifyErr) || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want verifier cause + ErrValidation", err)
	}
}

type fakeReleaseImageVerifier struct {
	verifyErr       error
	attestErr       error
	attestOutputErr error
	payload         []byte
	// requests holds every request in call order, whole.
	requests []any
	// stderr is what cosign writes on its error stream. It used to be
	// discarded, so the diagnostic printer's redaction ran over nothing.
	stderr string
}

func (f *fakeReleaseImageVerifier) VerifyImage(_ context.Context, in cosign.VerifyImageInput, errOut io.Writer) error {
	f.requests = append(f.requests, in)

	if f.stderr != "" {
		_, _ = io.WriteString(errOut, f.stderr)
	}

	return f.verifyErr
}

func (f *fakeReleaseImageVerifier) VerifyAttestation(_ context.Context, in cosign.VerifyAttestationInput, _ io.Writer) error {
	f.requests = append(f.requests, in)

	return f.attestErr
}

func (f *fakeReleaseImageVerifier) VerifyAttestationOutput(_ context.Context, in cosign.VerifyAttestationInput, out, _ io.Writer) error {
	f.requests = append(f.requests, in)
	if f.attestOutputErr != nil {
		return f.attestOutputErr
	}

	_, err := out.Write(f.payload)

	return err
}

type fakeReleaseImageLabels struct {
	ref    string
	labels map[string]string
	err    error
}

func (f *fakeReleaseImageLabels) Labels(_ context.Context, ref string) (map[string]string, error) {
	f.ref = ref

	return f.labels, f.err
}

type releaseImageFields struct {
	tag         string
	commit      string
	source      string
	workflow    string
	baseRef     string
	baseInputID string
}

func releaseImagePayload(t *testing.T, fields releaseImageFields) []byte {
	t.Helper()

	return releaseImagePayloadWith(t, fields, nil)
}

// releaseImagePayloadWith builds the same signed-output shape and lets a test
// change the decoded statement before it is encoded, so one field at a time
// can be made wrong.
func releaseImagePayloadWith(t *testing.T, fields releaseImageFields, mutate func(statement map[string]any)) []byte {
	t.Helper()

	external := map[string]any{
		"workflow": map[string]any{
			"ref":        fields.tag,
			"repository": fields.source,
			"path":       fields.workflow,
		},
	}

	deps := []any{map[string]any{"digest": map[string]any{"gitCommit": fields.commit}}}
	if fields.baseRef != "" {
		external["base"] = map[string]any{"ref": fields.baseRef, "input_id": fields.baseInputID}
		deps = append(deps, map[string]any{
			"uri":         "oci://" + fields.baseRef,
			"annotations": map[string]any{"base_input_id": fields.baseInputID},
		})
	}

	statement := map[string]any{
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"externalParameters":   external,
				"resolvedDependencies": deps,
			},
		},
	}

	if mutate != nil {
		mutate(statement)
	}

	statementBody, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal statement: %v", err)
	}

	body, err := json.Marshal([]map[string]string{{"payload": base64.StdEncoding.EncodeToString(statementBody)}})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	return body
}

// TestVerifyExistingReleaseImage_RedactsUnsafeVerifierOutput makes the
// redaction observable.
//
// When verification fails, cosign's own stderr is echoed into the job log so an
// operator can see why. cosign talks to a registry, so that stream can carry an
// Authorization header, a bearer token, a registry URL with inline credentials,
// or an OIDC JWT — and the release-verification path is exactly where a
// misconfigured registry credential surfaces. The filter that drops those lines
// existed and had never been given a line to drop: the verifier fake wrote
// nothing to its error stream, so every case ran the printer over an empty
// buffer.
//
// Both halves are asserted. Dropping everything would satisfy an absence-only
// test while destroying the diagnostic the echo exists for.
func TestVerifyExistingReleaseImage_RedactsUnsafeVerifierOutput(t *testing.T) {
	const (
		safeLine  = "Error: no matching signatures found for the supplied key"
		safeLine2 = "main.go:74: error during command execution"
	)

	unsafe := []struct {
		name, line, canary string
	}{
		{name: "authorization header", line: "GET /v2/ HTTP/1.1 Authorization: Basic Y2FuYXJ5OnBhc3N3b3Jk", canary: "Y2FuYXJ5OnBhc3N3b3Jk"},
		{name: "bearer token", line: "retrying with Bearer CANARY-BEARER-9f31c8", canary: "CANARY-BEARER-9f31c8"},
		{name: "registry password", line: "password=CANARY-REGISTRY-PW-4a17", canary: "CANARY-REGISTRY-PW-4a17"},
		//nolint:gosec // G101: a synthetic canary in a fixture line, not a credential.
		{name: "credential URL", line: "fetching https://user:CANARY-INLINE-PW-77b2@registry.example/v2/", canary: "CANARY-INLINE-PW-77b2"},
		{
			name: "an OIDC JWT", canary: "eyJhbGciOiJIUzI1NiJ9",
			line: "identity token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		},
	}

	var stderr strings.Builder

	stderr.WriteString(safeLine + "\n")

	for _, u := range unsafe {
		stderr.WriteString(u.line + "\n")
	}

	stderr.WriteString(safeLine2 + "\n")

	verifier := &fakeReleaseImageVerifier{
		verifyErr: errReleaseImageVerify,
		stderr:    stderr.String(),
	}

	var out bytes.Buffer

	_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, nil, &out, releaseImageVerifyInput())
	if err == nil {
		t.Fatal("a failing verification reported success")
	}

	got := out.String()

	for _, u := range unsafe {
		if strings.Contains(got, u.canary) {
			t.Errorf("%s survived redaction:\n%s", u.name, got)
		}
	}

	// The safe lines are the reason the stream is echoed at all.
	for _, want := range []string{safeLine, safeLine2} {
		if !strings.Contains(got, want) {
			t.Errorf("a safe diagnostic line was dropped (%q):\n%s", want, got)
		}
	}
}

var errReleaseImageVerify = errors.New("cosign: verification failed") //nolint:err113 // test fixture sentinel.

// TestVerifyExistingReleaseImage_EachIdentityFieldIsChecked makes one
// provenance field wrong at a time on a statement that otherwise matches,
// base lineage included. Every existing mismatch fixture changed the tag and
// the commit together, so a check that ignored either one, or the source,
// workflow, base reference, base input ID, base dependency or its annotation,
// still refused them for the other reason.
func TestVerifyExistingReleaseImage_EachIdentityFieldIsChecked(t *testing.T) {
	t.Parallel()

	baseRef := "codeberg.org/itiquette/nanolinter-base@sha256:" + strings.Repeat("b", 64)
	baseInputID := strings.Repeat("c", 64)
	fields := releaseImageFields{
		tag: "v1", commit: "cafe1234", source: "https://codeberg.org/itiquette/nanolinter",
		workflow: ".forgejo/workflows/release.yml", baseRef: baseRef, baseInputID: baseInputID,
	}

	input := releaseImageVerifyInput()
	input.ExpectedBaseRef = baseRef
	input.ExpectedBaseInputID = baseInputID

	path := func(statement map[string]any, keys ...string) map[string]any {
		node := statement
		for _, key := range keys {
			node = node[key].(map[string]any) //nolint:forcetypeassert // the fixture builds these maps.
		}

		return node
	}
	dependency := func(statement map[string]any, index int) map[string]any {
		deps := path(statement, "predicate", "buildDefinition")["resolvedDependencies"].([]any) //nolint:forcetypeassert // fixture shape.

		return deps[index].(map[string]any) //nolint:forcetypeassert // fixture shape.
	}

	for name, mutate := range map[string]func(map[string]any){
		"predicate type": func(s map[string]any) { s["predicateType"] = "https://slsa.dev/provenance/v0.2" },
		"tag": func(s map[string]any) {
			path(s, "predicate", "buildDefinition", "externalParameters", "workflow")["ref"] = "v2"
		},
		"source": func(s map[string]any) {
			path(s, "predicate", "buildDefinition", "externalParameters", "workflow")["repository"] = "https://codeberg.org/other/nanolinter"
		},
		"workflow": func(s map[string]any) {
			path(s, "predicate", "buildDefinition", "externalParameters", "workflow")["path"] = ".forgejo/workflows/other.yml"
		},
		"commit": func(s map[string]any) { path(dependency(s, 0), "digest")["gitCommit"] = "beef5678" },
		"base ref": func(s map[string]any) {
			path(s, "predicate", "buildDefinition", "externalParameters", "base")["ref"] = "codeberg.org/other@sha256:" + strings.Repeat("d", 64)
		},
		"base input id": func(s map[string]any) {
			path(s, "predicate", "buildDefinition", "externalParameters", "base")["input_id"] = strings.Repeat("e", 64)
		},
		"base dependency uri": func(s map[string]any) {
			dependency(s, 1)["uri"] = "oci://codeberg.org/other@sha256:" + strings.Repeat("d", 64)
		},
		"base dependency annotation": func(s map[string]any) {
			path(dependency(s, 1), "annotations")["base_input_id"] = strings.Repeat("e", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			verifier := &fakeReleaseImageVerifier{payload: releaseImagePayloadWith(t, fields, mutate)}

			_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, nil, io.Discard, input)
			if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "lacks expected release fields") {
				t.Errorf("err = %v, want the provenance refused", err)
			}
		})
	}

	t.Run("control", func(t *testing.T) {
		t.Parallel()

		verifier := &fakeReleaseImageVerifier{payload: releaseImagePayloadWith(t, fields, func(map[string]any) {})}

		result, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, nil, io.Discard, input)
		if err != nil || result.Status != appcontainer.ReleaseImageStatusVerified {
			t.Errorf("status = %q, err = %v; want the unmodified statement verified", result.Status, err)
		}
	})
}
