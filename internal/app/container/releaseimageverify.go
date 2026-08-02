// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainprovenance "github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// Release-image verification outcomes: fully verified evidence, or signed
// with matching identity so the signer may re-attest it.
const (
	ReleaseImageStatusVerified     = "verified"
	ReleaseImageStatusReattestable = "reattestable"
)

// ReleaseImageVerifyExistingInput drives `container release-image
// verify-existing`: the digest-pinned ref plus the release identity the
// signed evidence must match.
type ReleaseImageVerifyExistingInput struct {
	Ref string

	CosignPublicKey string

	ExpectedTag      string
	ExpectedCommit   string
	ExpectedSource   string
	ExpectedWorkflow string

	ExpectedBaseRef     string
	ExpectedBaseInputID string

	AllowReattest bool

	ExpectedIdentityVersion string
	ExpectedIdentityRefName string
	ExpectedIdentitySource  string
}

// ReleaseImageVerifyExistingResult reports which verification outcome the
// existing release image reached (ReleaseImageStatusVerified or
// ReleaseImageStatusReattestable).
type ReleaseImageVerifyExistingResult struct {
	Status string
}

// imageEvidenceVerifier is the cosign surface release-image
// verification needs: verify the signature and the attached
// attestations, optionally capturing the attestation output.
type imageEvidenceVerifier interface {
	VerifyImage(ctx context.Context, in domaincontainer.ImageVerifyRequest, errOut io.Writer) error
	VerifyAttestation(ctx context.Context, in domaincontainer.AttestationVerifyRequest, errOut io.Writer) error
	VerifyAttestationOutput(ctx context.Context, in domaincontainer.AttestationVerifyRequest, out, errOut io.Writer) error
}

type releaseImageProvenanceExpectation struct {
	Tag             string
	Commit          string
	Source          string
	Workflow        string
	BaseRef         string
	BaseInputID     string
	IdentityVersion string
	IdentityRefName string
	IdentitySource  string
}

// VerifyExistingReleaseImage checks an already-published release image's
// signature, SBOM, and SLSA provenance against the expected release identity;
// with AllowReattest it falls back to the re-attestation eligibility check.
func VerifyExistingReleaseImage(ctx context.Context, verifier imageEvidenceVerifier, registry OCIImageLabelsRegistry, out io.Writer, in ReleaseImageVerifyExistingInput) (ReleaseImageVerifyExistingResult, error) {
	if verifier == nil {
		return ReleaseImageVerifyExistingResult{}, fmt.Errorf("release image verify: cosign verifier is required: %w", errs.ErrUsage)
	}

	expected, err := validateReleaseImageVerifyInput(in)
	if err != nil {
		return ReleaseImageVerifyExistingResult{}, err
	}

	evidenceErr := verifyReleaseImageEvidence(ctx, verifier, out, in.Ref, in.CosignPublicKey, expected)
	if evidenceErr == nil {
		_, _ = fmt.Fprintf(out, "Verified release image signature, SBOM, and provenance: %s\n", in.Ref)

		return ReleaseImageVerifyExistingResult{Status: ReleaseImageStatusVerified}, nil
	}

	if !in.AllowReattest {
		return ReleaseImageVerifyExistingResult{}, evidenceErr
	}

	_, _ = fmt.Fprintf(out, "release image full verification did not pass; checking re-attestation eligibility: %v\n", evidenceErr)

	if registry == nil {
		return ReleaseImageVerifyExistingResult{}, fmt.Errorf("release image verify: registry is required when --allow-reattest is set: %w", errs.ErrUsage)
	}

	if expected.BaseRef != "" || expected.BaseInputID != "" {
		return ReleaseImageVerifyExistingResult{}, fmt.Errorf("release image verify: re-attestation is only supported without base-lineage expectations: %w", errs.ErrUsage)
	}

	if err := releaseImageCanBeReattested(ctx, verifier, registry, out, in.Ref, in.CosignPublicKey, expected); err != nil {
		return ReleaseImageVerifyExistingResult{}, err
	}

	_, _ = fmt.Fprintf(out, "Release image is signed, SBOM-attested, and has matching OCI release identity: %s\n", in.Ref)

	return ReleaseImageVerifyExistingResult{Status: ReleaseImageStatusReattestable}, nil
}

func validateReleaseImageVerifyInput(in ReleaseImageVerifyExistingInput) (releaseImageProvenanceExpectation, error) {
	if err := validateReleaseImageVerifyRequired(in); err != nil {
		return releaseImageProvenanceExpectation{}, err
	}

	if err := validateReleaseImageVerifyBase(in); err != nil {
		return releaseImageProvenanceExpectation{}, err
	}

	identityVersion := in.ExpectedIdentityVersion
	if identityVersion == "" {
		identityVersion = in.ExpectedTag
	}

	identityRefName := in.ExpectedIdentityRefName
	if identityRefName == "" {
		identityRefName = in.ExpectedTag
	}

	identitySource := in.ExpectedIdentitySource
	if identitySource == "" {
		identitySource = in.ExpectedSource
	}

	return releaseImageProvenanceExpectation{
		Tag:             in.ExpectedTag,
		Commit:          in.ExpectedCommit,
		Source:          in.ExpectedSource,
		Workflow:        in.ExpectedWorkflow,
		BaseRef:         in.ExpectedBaseRef,
		BaseInputID:     in.ExpectedBaseInputID,
		IdentityVersion: identityVersion,
		IdentityRefName: identityRefName,
		IdentitySource:  identitySource,
	}, nil
}

// validateReleaseImageVerifyRequired checks the always-required inputs.
func validateReleaseImageVerifyRequired(in ReleaseImageVerifyExistingInput) error {
	if !validReleaseImageDigestRef(in.Ref) {
		return fmt.Errorf("release image verify: --ref must be digest-pinned: %s: %w", in.Ref, errs.ErrValidation)
	}

	if strings.TrimSpace(in.CosignPublicKey) == "" {
		return fmt.Errorf("release image verify: --cosign-public-key-path is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedTag) == "" {
		return fmt.Errorf("release image verify: --expected-tag is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedCommit) == "" {
		return fmt.Errorf("release image verify: --expected-commit is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedSource) == "" {
		return fmt.Errorf("release image verify: --expected-source is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedWorkflow) == "" {
		return fmt.Errorf("release image verify: --expected-workflow is required: %w", errs.ErrUsage)
	}

	return nil
}

// validateReleaseImageVerifyBase checks the optional base-lineage pair.
func validateReleaseImageVerifyBase(in ReleaseImageVerifyExistingInput) error {
	if (in.ExpectedBaseRef == "") != (in.ExpectedBaseInputID == "") {
		return fmt.Errorf("release image verify: --expected-base-ref and --expected-base-input-id must be supplied together: %w", errs.ErrUsage)
	}

	if in.ExpectedBaseRef != "" && !validReleaseImageDigestRef(in.ExpectedBaseRef) {
		return fmt.Errorf("release image verify: --expected-base-ref must be digest-pinned: %s: %w", in.ExpectedBaseRef, errs.ErrValidation)
	}

	if in.ExpectedBaseInputID != "" && !hex64RE.MatchString(in.ExpectedBaseInputID) {
		return fmt.Errorf("release image verify: --expected-base-input-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

func validReleaseImageDigestRef(ref string) bool {
	canonical, err := domaincontainer.CanonicalImageRef(ref)
	if err != nil {
		return false
	}

	_, digest, ok := strings.Cut(canonical, "@")

	return ok && domaincontainer.ValidDigest(digest)
}

func verifyReleaseImageEvidence(ctx context.Context, verifier imageEvidenceVerifier, out io.Writer, ref, publicKey string, expected releaseImageProvenanceExpectation) error {
	var errBuf bytes.Buffer
	if err := verifier.VerifyImage(ctx, domaincontainer.ImageVerifyRequest{ImageRef: ref, KeyRef: publicKey}, &errBuf); err != nil {
		printReleaseImageVerificationError(out, ref, "signature", errBuf.Bytes())

		return fmt.Errorf("release image verify: signature verification failed for %s: %w", ref, err)
	}

	errBuf.Reset()

	if err := verifier.VerifyAttestation(ctx, domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: domaincontainer.PredicateTypeCycloneDX, KeyRef: publicKey}, &errBuf); err != nil {
		printReleaseImageVerificationError(out, ref, "CycloneDX attestation", errBuf.Bytes())

		return fmt.Errorf("release image verify: CycloneDX attestation verification failed for %s: %w", ref, err)
	}

	errBuf.Reset()

	var provenance bytes.Buffer
	if err := verifier.VerifyAttestationOutput(ctx, domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: domaincontainer.PredicateTypeSLSAProvenance1, KeyRef: publicKey}, &provenance, &errBuf); err != nil {
		printReleaseImageVerificationError(out, ref, "SLSA provenance attestation", errBuf.Bytes())

		return fmt.Errorf("release image verify: SLSA provenance attestation verification failed for %s: %w", ref, err)
	}

	if err := releaseImageProvenanceAttestationMatches(provenance.Bytes(), expected); err != nil {
		_, _ = fmt.Fprintln(out, "ERROR: signed release image provenance does not match this release")
		_, _ = fmt.Fprintf(out, "  image:           %s\n", ref)
		_, _ = fmt.Fprintf(out, "  expected tag:    %s\n", expected.Tag)
		_, _ = fmt.Fprintf(out, "  expected commit: %s\n", expected.Commit)

		_, _ = fmt.Fprintf(out, "  expected source: %s\n", expected.Source)
		if expected.BaseRef != "" || expected.BaseInputID != "" {
			_, _ = fmt.Fprintf(out, "  expected base:   %s\n", expected.BaseRef)
			_, _ = fmt.Fprintf(out, "  base input id:   %s\n", expected.BaseInputID)
		}

		_, _ = fmt.Fprintln(out, "Refusing to reuse an immutable image tag from another release commit.")

		return err
	}

	return nil
}

func releaseImageCanBeReattested(ctx context.Context, verifier imageEvidenceVerifier, registry OCIImageLabelsRegistry, out io.Writer, ref, publicKey string, expected releaseImageProvenanceExpectation) error {
	var errBuf bytes.Buffer
	if err := verifier.VerifyImage(ctx, domaincontainer.ImageVerifyRequest{ImageRef: ref, KeyRef: publicKey}, &errBuf); err != nil {
		_, _ = fmt.Fprintf(out, "re-attest declined: cosign verify did not pass for %s:\n", ref)
		printReleaseImageVerificationError(out, ref, "signature", errBuf.Bytes())

		return fmt.Errorf("release image verify: re-attestation signature precheck failed: %w", err)
	}

	errBuf.Reset()

	if err := verifier.VerifyAttestation(ctx, domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: domaincontainer.PredicateTypeCycloneDX, KeyRef: publicKey}, &errBuf); err != nil {
		_, _ = fmt.Fprintf(out, "re-attest declined: cosign verify-attestation (cyclonedx) did not pass for %s:\n", ref)
		printReleaseImageVerificationError(out, ref, "CycloneDX attestation", errBuf.Bytes())

		return fmt.Errorf("release image verify: re-attestation CycloneDX precheck failed: %w", err)
	}

	inspectRef := releaseImageDigestOnlyRef(ref)

	labels, err := registry.Labels(ctx, inspectRef)
	if err != nil {
		_, _ = fmt.Fprintf(out, "re-attest declined: could not read OCI labels for %s\n", inspectRef)

		return fmt.Errorf("release image verify: read OCI labels: %w", err)
	}

	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("release image verify: encode OCI labels JSON: %w", err)
	}

	identityMatches, err := OCIReleaseIdentityMatches(string(labelsJSON), OCIReleaseIdentityInput{
		Revision: expected.Commit,
		Version:  expected.IdentityVersion,
		RefName:  expected.IdentityRefName,
		Source:   expected.IdentitySource,
	})
	if err != nil {
		_, _ = fmt.Fprintf(out, "re-attest declined: could not evaluate OCI release identity for %s\n", inspectRef)

		return err
	}

	if identityMatches {
		return nil
	}

	_, _ = fmt.Fprintln(out, "Existing release image is signed but its OCI labels do not match this release; it cannot be re-attested safely.")
	_, _ = fmt.Fprintf(out, "  image:            %s\n", ref)
	_, _ = fmt.Fprintf(out, "  expected tag:     %s\n", expected.IdentityRefName)
	_, _ = fmt.Fprintf(out, "  expected version: %s\n", expected.IdentityVersion)
	_, _ = fmt.Fprintf(out, "  expected commit:  %s\n", expected.Commit)
	_, _ = fmt.Fprintf(out, "  expected source:  %s\n", expected.IdentitySource)

	return fmt.Errorf("release image verify: OCI release identity does not match: %w", errs.ErrValidation)
}

func releaseImageDigestOnlyRef(ref string) string {
	refWithoutDigest, digest, ok := strings.Cut(ref, "@")
	if !ok {
		return ref
	}

	lastPathComponent := refWithoutDigest[strings.LastIndex(refWithoutDigest, "/")+1:]
	if strings.Contains(lastPathComponent, ":") {
		refWithoutDigest = refWithoutDigest[:strings.LastIndex(refWithoutDigest, ":")]
	}

	return refWithoutDigest + "@" + digest
}

func releaseImageProvenanceAttestationMatches(body []byte, expected releaseImageProvenanceExpectation) error {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("release image verify: parse SLSA provenance attestation output: %w: %w", err, errs.ErrMalformedInput)
	}

	envelopes, ok := raw.([]any)
	if !ok {
		envelopes = []any{raw}
	}

	for _, envelope := range envelopes {
		payload, ok := domainprovenance.EnvelopePayload(envelope)
		if !ok {
			continue
		}

		statement, err := domainprovenance.DecodeStatement(payload)
		if err != nil {
			return err
		}

		if releaseImageProvenanceStatementMatches(statement, expected) {
			return nil
		}
	}

	return fmt.Errorf("release image verify: SLSA provenance attestation lacks expected release fields: %w", errs.ErrValidation)
}

func releaseImageProvenanceStatementMatches(statement map[string]any, expected releaseImageProvenanceExpectation) bool {
	if domainprovenance.StatementString(statement, "predicateType") != domainprovenance.PredicateTypeV1 {
		return false
	}

	build, ok := domainprovenance.NestedMap(statement, "predicate", "buildDefinition")
	if !ok {
		return false
	}

	params, ok := domainprovenance.NestedMap(build, "externalParameters")
	if !ok {
		return false
	}

	if !releaseImageWorkflowMatches(params, expected) {
		return false
	}

	if !releaseImageHasGitCommitDependency(build, expected.Commit) {
		return false
	}

	if expected.BaseRef == "" && expected.BaseInputID == "" {
		return true
	}

	return releaseImageBaseMatches(params, build, expected)
}

// releaseImageWorkflowMatches checks the provenance workflow identity fields.
func releaseImageWorkflowMatches(params map[string]any, expected releaseImageProvenanceExpectation) bool {
	workflow, ok := domainprovenance.NestedMap(params, "workflow")
	if !ok {
		return false
	}

	return domainprovenance.StatementString(workflow, "ref") == expected.Tag &&
		domainprovenance.StatementString(workflow, "repository") == expected.Source &&
		domainprovenance.StatementString(workflow, "path") == expected.Workflow
}

// releaseImageBaseMatches checks the provenance base-lineage fields and the
// matching resolved dependency.
func releaseImageBaseMatches(params, build map[string]any, expected releaseImageProvenanceExpectation) bool {
	base, ok := domainprovenance.NestedMap(params, "base")
	if !ok || domainprovenance.StatementString(base, "ref") != expected.BaseRef || domainprovenance.StatementString(base, "input_id") != expected.BaseInputID {
		return false
	}

	return releaseImageHasBaseDependency(build, expected.BaseRef, expected.BaseInputID)
}

func releaseImageHasGitCommitDependency(build map[string]any, commit string) bool {
	for _, dep := range releaseImageDependencies(build) {
		digest, ok := domainprovenance.NestedMap(dep, "digest")
		if ok && domainprovenance.StatementString(digest, "gitCommit") == commit {
			return true
		}
	}

	return false
}

func releaseImageHasBaseDependency(build map[string]any, baseRef, baseInputID string) bool {
	for _, dep := range releaseImageDependencies(build) {
		annotations, ok := domainprovenance.NestedMap(dep, "annotations")
		if domainprovenance.StatementString(dep, "uri") == "oci://"+baseRef && ok && domainprovenance.StatementString(annotations, "base_input_id") == baseInputID {
			return true
		}
	}

	return false
}

func releaseImageDependencies(build map[string]any) []map[string]any {
	items, ok := build["resolvedDependencies"].([]any)
	if !ok {
		return nil
	}

	deps := make([]map[string]any, 0, len(items))
	for _, item := range items {
		dep, ok := item.(map[string]any)
		if ok {
			deps = append(deps, dep)
		}
	}

	return deps
}

func printReleaseImageVerificationError(out io.Writer, ref, verifyStep string, raw []byte) {
	_, _ = fmt.Fprintf(out, "  image: %s\n", ref)
	_, _ = fmt.Fprintf(out, "  check: %s\n", verifyStep)

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || domaincontainer.UnsafeCosignErrorLine(line) {
			continue
		}

		_, _ = fmt.Fprintf(out, "  cosign: %s\n", line)
	}
}
