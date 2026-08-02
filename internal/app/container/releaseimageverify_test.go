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
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
)

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

	if got := strings.Join(verifier.ops, ","); got != "verify,attest:cyclonedx,attest-output:slsaprovenance1" {
		t.Fatalf("ops = %s", got)
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
		t.Fatalf("labels ref = %q", registry.ref)
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
	if err == nil {
		t.Fatal("VerifyExistingReleaseImage succeeded, want identity mismatch error")
	}
}

func TestVerifyExistingReleaseImage_ReattestRequiresSignedSBOM(t *testing.T) {
	verifier := &fakeReleaseImageVerifier{
		payload:   releaseImagePayload(t, releaseImageFields{tag: "other", commit: "other", source: "https://codeberg.org/itiquette/nanolinter", workflow: ".forgejo/workflows/release.yml"}),
		attestErr: errors.New("missing sbom"), //nolint:err113 // test double error.
	}
	input := releaseImageVerifyInput()
	input.AllowReattest = true

	_, err := appcontainer.VerifyExistingReleaseImage(context.Background(), verifier, &fakeReleaseImageLabels{}, io.Discard, input)
	if err == nil {
		t.Fatal("VerifyExistingReleaseImage succeeded, want SBOM precheck error")
	}
}

func releaseImageVerifyInput() appcontainer.ReleaseImageVerifyExistingInput {
	return appcontainer.ReleaseImageVerifyExistingInput{
		Ref:              releaseImageDigestRef,
		CosignPublicKey:  ".forgejo/keys/cosign.pub",
		ExpectedTag:      "v1",
		ExpectedCommit:   "cafe1234",
		ExpectedSource:   "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow: ".forgejo/workflows/release.yml",
	}
}

type fakeReleaseImageVerifier struct {
	verifyErr       error
	attestErr       error
	attestOutputErr error
	payload         []byte
	ops             []string
}

func (f *fakeReleaseImageVerifier) VerifyImage(context.Context, cosign.VerifyImageInput, io.Writer) error {
	f.ops = append(f.ops, "verify")

	return f.verifyErr
}

func (f *fakeReleaseImageVerifier) VerifyAttestation(_ context.Context, in cosign.VerifyAttestationInput, _ io.Writer) error {
	f.ops = append(f.ops, "attest:"+in.PredicateType)

	return f.attestErr
}

func (f *fakeReleaseImageVerifier) VerifyAttestationOutput(_ context.Context, in cosign.VerifyAttestationInput, out, _ io.Writer) error {
	f.ops = append(f.ops, "attest-output:"+in.PredicateType)
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
