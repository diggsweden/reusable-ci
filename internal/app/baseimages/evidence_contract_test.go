// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	evidenceCycloneStep   = "CycloneDX attestation"
	evidenceSLSAStep      = "SLSA provenance (lineage) attestation"
	evidencePredicateStep = "SLSA provenance (lineage) predicate"
)

func TestBaseImageEvidenceOnce_Contract(t *testing.T) {
	t.Parallel()

	for _, pin := range []struct{ ref, key string }{
		{"registry.example/contract/first@sha256:" + strings.Repeat("1", 64), "keys/leaf-first.pub"},
		{"registry.example/contract/second@sha256:" + strings.Repeat("2", 64), "keys/leaf-second.pub"},
	} {
		for _, tc := range evidenceContractCases() {
			t.Run(pin.key+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				verifier := newEvidenceRecorder(t, ctx, map[string][]byte{pin.ref: tc.payload}, tc.failAt, tc.portErr)

				step, stderr, err := verifyBaseImageEvidenceOnce(ctx, verifier, pin.ref, pin.key, baseImageLineageExpectation{
					Source: "https://source.example/evidence", Workflow: "evidence-build.yml", Flavor: "rust", BaseInputID: strings.Repeat("b", 64),
				})
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("leaf error = %v, want %v", err, tc.wantErr)
				}

				if step != tc.step {
					t.Errorf("step = %q, want %q", step, tc.step)
				}

				if tc.errorPrefix != "" && !strings.HasPrefix(err.Error(), tc.errorPrefix) {
					t.Errorf("error = %q, want prefix %q", err, tc.errorPrefix)
				}

				wantStderr := tc.stderr
				if !bytes.Equal(stderr, []byte(wantStderr)) || (err == nil && stderr != nil) {
					t.Errorf("stderr = %q, want %q (nil on success)", stderr, wantStderr)
				}

				wantCalls := evidenceRequests(pin.ref, pin.key)[:tc.calls]
				if !reflect.DeepEqual(verifier.calls, wantCalls) {
					t.Errorf("calls = %#v, want exact prefix %#v", verifier.calls, wantCalls)
				}
			})
		}
	}
}

func TestBaseImageEvidence_OneAttemptDiagnostics(t *testing.T) {
	t.Parallel()

	const key = "keys/outer-canary.pub"

	digest := "sha256:" + strings.Repeat("3", 64)
	ref := "registry.example/contract/outer@" + digest

	for _, tc := range evidenceContractCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			verifier := newEvidenceRecorder(t, ctx, map[string][]byte{ref: tc.payload}, tc.failAt, tc.portErr)

			var out bytes.Buffer

			err := verifyBaseImageEvidence(ctx, verifier, &out, ref, key, baseImageLineageExpectation{
				Source: "https://source.example/evidence", Workflow: "evidence-build.yml", Flavor: "rust", BaseInputID: strings.Repeat("b", 64),
			}, 1)
			wantOutput := "Verified base image signature and lineage: flavor=rust digest=" + digest + "\n"

			switch {
			case tc.wantErr == nil:
				if err != nil {
					t.Fatalf("verify evidence: %v", err)
				}
			case errors.Is(tc.wantErr, context.Canceled), errors.Is(tc.wantErr, context.DeadlineExceeded):
				if !errors.Is(err, tc.wantErr) || !strings.HasPrefix(err.Error(), "base images: verify base image signature and lineage: ") {
					t.Fatalf("context error = %v, want wrapped %v", err, tc.wantErr)
				}

				wantOutput = ""
			default:
				// Ordinary exhaustion classifies the failure; only the leaf preserves the port sentinel.
				if !errors.Is(err, errs.ErrValidation) || !strings.HasPrefix(err.Error(), "base images: failed to verify base image signature and lineage: ") {
					t.Fatalf("exhaustion error = %v, want ErrValidation and failure prefix", err)
				}

				wantOutput = "ERROR: failed to verify base image signature and lineage after retries\n" +
					"  flavor: rust\n  digest: " + digest + "\n  check:  " + tc.step + "\n  cosign: " + strings.TrimSpace(tc.stderr) + "\n"
			}

			if out.String() != wantOutput {
				t.Errorf("output = %q, want %q", &out, wantOutput)
			}

			if want := evidenceRequests(ref, key)[:tc.calls]; !reflect.DeepEqual(verifier.calls, want) {
				t.Errorf("calls = %#v, want one-attempt prefix %#v", verifier.calls, want)
			}
		})
	}
}

func TestVerifyExistingBaseImages_EvidenceHandoff(t *testing.T) {
	t.Parallel()

	const (
		repo = "registry.example/contract/public"
		key  = "keys/public-handoff-canary.pub"
	)

	firstID, rustID, lastID := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	firstDigest, rustDigest, lastDigest := "sha256:"+strings.Repeat("4", 64), "sha256:"+strings.Repeat("5", 64), "sha256:"+strings.Repeat("6", 64)
	firstTag, rustTag, lastTag := repo+":"+firstID+"-zig", repo+":"+rustID+"-rust", repo+":"+lastID+"-go"
	firstRef, rustRef, lastRef := repo+"@"+firstDigest, repo+"@"+rustDigest, repo+"@"+lastDigest

	for _, tc := range evidenceContractCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			registry := &fakeBaseImageRegistry{digests: map[string]string{firstTag: firstDigest, rustTag: rustDigest, lastTag: lastDigest}}
			verifier := newEvidenceRecorder(t, ctx, map[string][]byte{
				firstRef: evidenceEnvelope(evidenceStatement("zig", firstID)),
				rustRef:  tc.payload,
				lastRef:  evidenceEnvelope(evidenceStatement("go", lastID)),
			}, 3+tc.failAt, tc.portErr)

			var out bytes.Buffer

			result, err := VerifyExistingBaseImages(ctx, registry, verifier, &out, BaseImageVerifyExistingInput{
				Flavors: []string{"zig", "rust", "go"}, BaseInputID: rustID,
				BaseInputs:         []BaseInput{{Flavor: "zig", BaseInputID: firstID}, {Flavor: "go", BaseInputID: lastID}},
				ExpectedRepository: repo, ExpectedSource: "https://source.example/evidence", ExpectedWorkflow: "evidence-build.yml", CosignPublicKey: key,
			})
			wantCalls := append(evidenceRequests(firstRef, key), evidenceRequests(rustRef, key)[:tc.calls]...)

			var (
				wantResult BaseImageVerifyExistingResult
				wantOutput string
			)

			switch tc.wantErr {
			case nil:
				wantCalls = append(wantCalls, evidenceRequests(lastRef, key)...)

				if err != nil {
					t.Fatalf("VerifyExistingBaseImages: %v", err)
				}

				wantResult = BaseImageVerifyExistingResult{
					AllFound: true, MissingFlavorsJSON: `[]`,
					AllImagesJSON: `[{"flavor":"go","tag":"` + lastTag + `","ref":"` + lastRef + `","candidate_tag":"","candidate_ref":"","base_input_id":"` + lastID + `"},` +
						`{"flavor":"rust","tag":"` + rustTag + `","ref":"` + rustRef + `","candidate_tag":"","candidate_ref":"","base_input_id":"` + rustID + `"},` +
						`{"flavor":"zig","tag":"` + firstTag + `","ref":"` + firstRef + `","candidate_tag":"","candidate_ref":"","base_input_id":"` + firstID + `"}]`,
				}
				wantOutput = "Verified base image signature and lineage: flavor=zig digest=" + firstDigest + "\nBase image already exists and verifies: " + firstRef + "\n" +
					"Verified base image signature and lineage: flavor=rust digest=" + rustDigest + "\nBase image already exists and verifies: " + rustRef + "\n" +
					"Verified base image signature and lineage: flavor=go digest=" + lastDigest + "\nBase image already exists and verifies: " + lastRef + "\n"
			default:
				wantErr := errs.ErrValidation
				prefix := "base images: failed to verify base image signature and lineage: "

				if errors.Is(tc.wantErr, context.Canceled) || errors.Is(tc.wantErr, context.DeadlineExceeded) {
					wantErr = tc.wantErr
					prefix = "base images: verify base image signature and lineage: "
				}

				prefix = "base images verify: final base tag exists but does not have the expected signature and attestation: " + rustTag + ": " + prefix
				if !errors.Is(err, wantErr) || !strings.HasPrefix(err.Error(), prefix) {
					t.Errorf("error = %v, want %v and prefix %q", err, wantErr, prefix)
				}
			}

			if result != wantResult || out.String() != wantOutput {
				t.Errorf("publication: result=%+v output=%q, want result=%+v output=%q", result, &out, wantResult, wantOutput)
			}

			if !reflect.DeepEqual(verifier.calls, wantCalls) {
				t.Errorf("calls = %#v, want exact prefix %#v", verifier.calls, wantCalls)
			}

			if !reflect.DeepEqual(registry.resolved, []string{firstTag, rustTag, lastTag}) || len(registry.copied) != 0 {
				t.Errorf("registry resolved=%v copied=%v", registry.resolved, registry.copied)
			}
		})
	}
}

type evidenceCase struct {
	name        string
	payload     []byte
	failAt      int
	portErr     error
	calls       int
	step        string
	stderr      string
	wantErr     error
	errorPrefix string
}

func evidenceContractCases() []evidenceCase {
	statement := evidenceStatement("rust", strings.Repeat("b", 64))
	good := evidenceEnvelope(statement)
	portErr := fmt.Errorf("recording verifier: %w", errs.ErrPermissionDenied)
	cases := make([]evidenceCase, 0, 18)

	cases = append(cases, []evidenceCase{
		{name: "verified object", payload: good, calls: 3},
		{name: "verified array after unrelated lineage", payload: []byte("[" + string(evidenceEnvelope(evidenceStatement("other", strings.Repeat("d", 64)))) + "," + string(good) + "]"), calls: 3},
		{name: "verified git source", payload: evidenceEnvelope(strings.Replace(statement, "https://source.example/evidence", "git+https://source.example/evidence", 1)), calls: 3},
		// Even output returned alongside a verifier error must not be parsed or accepted.
		{name: "signature failure", payload: []byte("{"), failAt: 1, portErr: portErr, calls: 1, step: "signature", stderr: "signature stderr canary\n", wantErr: errs.ErrPermissionDenied, errorPrefix: "recording verifier: "},
		{name: "CycloneDX failure", payload: []byte("{"), failAt: 2, portErr: portErr, calls: 2, step: evidenceCycloneStep, stderr: "CycloneDX stderr canary\n", wantErr: errs.ErrPermissionDenied, errorPrefix: "recording verifier: "},
		{name: "SLSA output failure", payload: []byte("{"), failAt: 3, portErr: portErr, calls: 3, step: evidenceSLSAStep, stderr: "SLSA stderr canary\n", wantErr: errs.ErrPermissionDenied, errorPrefix: "recording verifier: "},
		{name: "matching output with SLSA failure", payload: good, failAt: 3, portErr: portErr, calls: 3, step: evidenceSLSAStep, stderr: "SLSA stderr canary\n", wantErr: errs.ErrPermissionDenied, errorPrefix: "recording verifier: "},
		{name: "canceled", payload: good, failAt: 2, portErr: fmt.Errorf("recording verifier: %w", context.Canceled), calls: 2, step: evidenceCycloneStep, stderr: "CycloneDX stderr canary\n", wantErr: context.Canceled, errorPrefix: "recording verifier: "},
		{name: "deadline", payload: good, failAt: 3, portErr: fmt.Errorf("recording verifier: %w", context.DeadlineExceeded), calls: 3, step: evidenceSLSAStep, stderr: "SLSA stderr canary\n", wantErr: context.DeadlineExceeded, errorPrefix: "recording verifier: "},
		{name: "malformed output", payload: []byte("{"), calls: 3, step: evidencePredicateStep, wantErr: errs.ErrMalformedInput, errorPrefix: "parse SLSA provenance attestation output: ", stderr: "parse SLSA provenance attestation output: unexpected end of JSON input: malformed input\n"},
		{name: "malformed base64", payload: []byte(`{"payload":"%%%"}`), calls: 3, step: evidencePredicateStep, wantErr: errs.ErrMalformedInput, errorPrefix: "decode SLSA provenance payload: ", stderr: "decode SLSA provenance payload: illegal base64 data at input byte 0: malformed input\n"},
		{name: "malformed statement", payload: evidenceEnvelope("{"), calls: 3, step: evidencePredicateStep, wantErr: errs.ErrMalformedInput, errorPrefix: "parse SLSA provenance statement: ", stderr: "parse SLSA provenance statement: unexpected end of JSON input: malformed input\n"},
		{name: "missing payload", payload: []byte(`{}`), calls: 3, step: evidencePredicateStep, wantErr: errs.ErrValidation, errorPrefix: "SLSA provenance attestation lacks expected base lineage fields: ", stderr: "SLSA provenance attestation lacks expected base lineage fields: validation failed\n"},
	}...)
	for _, field := range []struct{ name, old, replacement string }{
		{"predicate type", "https://slsa.dev/provenance/v1", "https://slsa.dev/provenance/v0.2"},
		{"source", "https://source.example/evidence", "https://source.example/untrusted"},
		{"workflow", "evidence-build.yml", "untrusted-build.yml"},
		{"flavor", "rust", "untrusted"},
		{"base input id", strings.Repeat("b", 64), strings.Repeat("e", 64)},
	} {
		cases = append(cases, evidenceCase{
			name: "wrong " + field.name, payload: evidenceEnvelope(strings.Replace(statement, field.old, field.replacement, 1)),
			calls: 3, step: evidencePredicateStep, wantErr: errs.ErrValidation,
			errorPrefix: "SLSA provenance attestation lacks expected base lineage fields: ",
			stderr:      "SLSA provenance attestation lacks expected base lineage fields: validation failed\n",
		})
	}

	return cases
}

func evidenceStatement(flavor, id string) string {
	return fmt.Sprintf(`{"predicateType":"https://slsa.dev/provenance/v1","predicate":{"buildDefinition":{"externalParameters":{"source":"https://source.example/evidence","workflow":"evidence-build.yml","flavor":%q,"base_input_id":%q}}}}`, flavor, id)
}

func evidenceEnvelope(statement string) []byte {
	return []byte(`{"payload":"` + base64.StdEncoding.EncodeToString([]byte(statement)) + `"}`)
}

type evidenceCall struct {
	method  string
	request any
}

// Expectations spell out the whole port DTO, including inactive keyless fields,
// and use literal predicate types rather than the production constants.
func evidenceRequests(ref, key string) []evidenceCall {
	return []evidenceCall{
		{"VerifyImage", domaincontainer.ImageVerifyRequest{ImageRef: ref, Keyless: false, CertIdentityRegexp: "", CertOIDCIssuer: "", KeyRef: key}},
		{"VerifyAttestation", domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: "cyclonedx", Keyless: false, CertIdentityRegexp: "", CertOIDCIssuer: "", KeyRef: key}},
		{"VerifyAttestationOutput", domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: "slsaprovenance1", Keyless: false, CertIdentityRegexp: "", CertOIDCIssuer: "", KeyRef: key}},
	}
}

type evidenceRecorder struct {
	calls    []evidenceCall
	checkCtx func(context.Context)
	payloads map[string][]byte
	failAt   int
	portErr  error
}

func newEvidenceRecorder(t *testing.T, ctx context.Context, payloads map[string][]byte, failAt int, portErr error) *evidenceRecorder {
	t.Helper()

	return &evidenceRecorder{
		payloads: payloads, failAt: failAt, portErr: portErr,
		checkCtx: func(got context.Context) {
			t.Helper()

			if got != ctx {
				t.Errorf("verifier context was replaced: got %v, want %v", got, ctx)
			}
		},
	}
}

func (r *evidenceRecorder) VerifyImage(ctx context.Context, in domaincontainer.ImageVerifyRequest, errOut io.Writer) error {
	r.checkCtx(ctx)
	r.calls = append(r.calls, evidenceCall{"VerifyImage", in})

	if _, err := io.WriteString(errOut, "signature stderr canary\n"); err != nil {
		return err
	}

	if len(r.calls) == r.failAt {
		return r.portErr
	}

	return nil
}

func (r *evidenceRecorder) VerifyAttestation(ctx context.Context, in domaincontainer.AttestationVerifyRequest, errOut io.Writer) error {
	r.checkCtx(ctx)
	r.calls = append(r.calls, evidenceCall{"VerifyAttestation", in})

	if _, err := io.WriteString(errOut, "CycloneDX stderr canary\n"); err != nil {
		return err
	}

	if len(r.calls) == r.failAt {
		return r.portErr
	}

	return nil
}

func (r *evidenceRecorder) VerifyAttestationOutput(ctx context.Context, in domaincontainer.AttestationVerifyRequest, out, errOut io.Writer) error {
	r.checkCtx(ctx)
	r.calls = append(r.calls, evidenceCall{"VerifyAttestationOutput", in})

	if _, err := io.WriteString(errOut, "SLSA stderr canary\n"); err != nil {
		return err
	}

	if _, err := out.Write(r.payloads[in.ImageRef]); err != nil {
		return err
	}

	if len(r.calls) == r.failAt {
		return r.portErr
	}

	return nil
}
