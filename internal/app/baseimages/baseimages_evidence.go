// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

// baseImageEvidenceRetryDelay is the base wait between base-image evidence
// verification attempts; with linear backoff attempt N waits N×this.
const baseImageEvidenceRetryDelay = 15 * time.Second

type baseImageLineageExpectation struct {
	Source      string
	Workflow    string
	Flavor      string
	BaseInputID string
}

func verifyBaseImageEvidence(ctx context.Context, verifier imageEvidenceVerifier, out io.Writer, ref, publicKey string, expected baseImageLineageExpectation, attempts int) error {
	attempts = retry.Attempts(attempts, 1)

	if err := validateBaseImageRef(ref, domaincontainer.StripTagOrDigest(ref)); err != nil {
		return err
	}

	digest := digestFromRef(ref)

	var (
		lastVerifyStep string
		lastCosignErr  []byte
	)

	_, err := retry.Do(ctx, nil, attempts, baseImageEvidenceRetryDelay, func() (struct{}, error) {
		verifyStep, cosignErr, oneErr := verifyBaseImageEvidenceOnce(ctx, verifier, ref, publicKey, expected)
		lastVerifyStep, lastCosignErr = verifyStep, cosignErr

		return struct{}{}, oneErr
	}, retry.OnRetry(func(attempt, total int, _ time.Duration, _ error) {
		_, _ = fmt.Fprintf(out, "Base image signature/attestation verification did not pass (attempt %d/%d); retrying.\n", attempt, total)
		printBaseImageVerificationError(out, expected.Flavor, digest, lastVerifyStep, lastCosignErr)
	}))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("base images: verify base image signature and lineage: %w", err)
		}

		_, _ = fmt.Fprintln(out, "ERROR: failed to verify base image signature and lineage after retries")
		printBaseImageVerificationError(out, expected.Flavor, digest, lastVerifyStep, lastCosignErr)

		return fmt.Errorf("base images: failed to verify base image signature and lineage: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "Verified base image signature and lineage: flavor=%s digest=%s\n", expected.Flavor, digest)

	return nil
}

// verifyBaseImageEvidenceOnce runs one signature/SBOM/lineage verification
// pass. On failure it names the step that failed and returns the cosign
// stderr captured for that step.
func verifyBaseImageEvidenceOnce(ctx context.Context, verifier imageEvidenceVerifier, ref, publicKey string, expected baseImageLineageExpectation) (string, []byte, error) {
	var errBuf bytes.Buffer

	if err := verifier.VerifyImage(ctx, domaincontainer.ImageVerifyRequest{ImageRef: ref, KeyRef: publicKey}, &errBuf); err != nil {
		return "signature", errBuf.Bytes(), err
	}

	errBuf.Reset()

	if err := verifier.VerifyAttestation(ctx, domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: domaincontainer.PredicateTypeCycloneDX, KeyRef: publicKey}, &errBuf); err != nil {
		return "CycloneDX attestation", errBuf.Bytes(), err
	}

	errBuf.Reset()

	var lineage bytes.Buffer
	if err := verifier.VerifyAttestationOutput(ctx, domaincontainer.AttestationVerifyRequest{ImageRef: ref, PredicateType: domaincontainer.PredicateTypeSLSAProvenance1, KeyRef: publicKey}, &lineage, &errBuf); err != nil {
		return "SLSA provenance (lineage) attestation", errBuf.Bytes(), err
	}

	errBuf.Reset()

	if err := baseLineageAttestationMatches(lineage.Bytes(), expected); err != nil {
		_, _ = fmt.Fprintf(&errBuf, "%v\n", err)

		return "SLSA provenance (lineage) predicate", errBuf.Bytes(), err
	}

	return "", nil, nil
}

func printBaseImageVerificationError(out io.Writer, flavor, digest, verifyStep string, raw []byte) {
	_, _ = fmt.Fprintf(out, "  flavor: %s\n", flavor)
	_, _ = fmt.Fprintf(out, "  digest: %s\n", digest)
	_, _ = fmt.Fprintf(out, "  check:  %s\n", verifyStep)

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || domaincontainer.UnsafeCosignErrorLine(line) {
			continue
		}

		_, _ = fmt.Fprintf(out, "  cosign: %s\n", line)
	}
}

func baseLineageAttestationMatches(body []byte, expected baseImageLineageExpectation) error {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse SLSA provenance attestation output: %w: %w", err, errs.ErrMalformedInput)
	}

	envelopes, ok := raw.([]any)
	if !ok {
		envelopes = []any{raw}
	}

	for _, envelope := range envelopes {
		payload, ok := provenance.EnvelopePayload(envelope)
		if !ok {
			continue
		}

		statement, err := provenance.DecodeStatement(payload)
		if err != nil {
			return err
		}

		if baseLineageStatementMatches(statement, expected) {
			return nil
		}
	}

	return fmt.Errorf("SLSA provenance attestation lacks expected base lineage fields: %w", errs.ErrValidation)
}

func baseLineageStatementMatches(statement map[string]any, expected baseImageLineageExpectation) bool {
	if provenance.StatementString(statement, "predicateType") != provenance.PredicateTypeV1 {
		return false
	}

	params, ok := provenance.NestedMap(statement, "predicate", "buildDefinition", "externalParameters")
	if !ok {
		return false
	}

	return baseLineageSourceMatches(provenance.StatementString(params, "source"), expected.Source) &&
		provenance.StatementString(params, "workflow") == expected.Workflow &&
		provenance.StatementString(params, "flavor") == expected.Flavor &&
		provenance.StatementString(params, "base_input_id") == expected.BaseInputID
}

// baseLineageSourceMatches accepts the expected source repository URL in
// both spellings signed base images carry: the bare URL the retired
// `base-images sign` engine baked into its bespoke lineage predicate, and
// the SLSA-standard VCS URI ("git+" + URL) that `release provenance`
// computes for the base predicate `container ledger sign` enriches. Same
// repository either way, so verify/promote keep one EXPECTED_SOURCE
// configuration across the signer flip and images signed on both sides of
// it stay verifiable.
func baseLineageSourceMatches(got, expected string) bool {
	return expected != "" && (got == expected || got == "git+"+expected)
}
