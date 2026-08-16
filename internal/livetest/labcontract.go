// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The neutral live-target contract: one JSON object describing disposable
// provider infrastructure, produced by Forge Lab or by any operator willing to
// meet the same shape.
//
// It replaces the sourced shell environment this tier used to read. The
// difference is not only the encoding. The neutral contract carries
// infrastructure facts and nothing else -- no resource owner, no namespace, no
// expected identity, no destructive confirmation -- because those are the
// consumer's to declare. A producer that also named the owner and the
// confirmation would be handing out permission along with the address, and any
// consumer holding the file could act on any other consumer's fixtures.
//
// So this file parses only what the producer knows. Everything that authorizes
// destruction is re-derived in livetest.go from this suite's own constants and
// the operator's typed confirmation.
//
// Only the fields this suite uses are decoded, and unknown fields are refused:
// a contract carrying something this consumer does not understand is a contract
// this consumer should not act on.

const contractVersion = 1

type labContract struct {
	Version    int            `json:"version"`
	Generation labGeneration  `json:"generation"`
	CAFile     *string        `json:"ca_file"`
	Endpoints  []labEndpoint  `json:"endpoints"`
	Interfaces *labInterfaces `json:"interfaces"`
}

type labGeneration struct {
	ID string `json:"id"`
}

type labInterfaces struct {
	CredentialCleanup *string `json:"credential_cleanup"`
}

type labEndpoint struct {
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	WebBaseURL string        `json:"web_base_url"`
	APIBaseURL string        `json:"api_base_url"`
	GitBaseURL *string       `json:"git_base_url"`
	Credential labCredential `json:"credential"`
}

type labCredential struct {
	Username   string        `json:"username"`
	Token      string        `json:"token"`
	ProviderID *string       `json:"provider_id"`
	Name       string        `json:"name"`
	CreatedAt  string        `json:"created_at"`
	ExpiresAt  *string       `json:"expires_at"`
	Revocation labRevocation `json:"revocation"`
}

type labRevocation struct {
	Required     bool    `json:"required"`
	GenerationID *string `json:"generation_id"`
}

// loadLabContract reads and strictly decodes the contract named by path.
//
// The file is read whole rather than streamed: it is small, and a decoder that
// stops at the first syntactically complete object would accept a file with a
// second object appended to it.
func loadLabContract(path string) (labContract, error) {
	if !strings.HasPrefix(path, "/") {
		return labContract{}, fmt.Errorf("contract path %q must be absolute: %w", path, errs.ErrValidation)
	}

	body, err := os.ReadFile(path) //nolint:gosec // an operator-named contract path is the input this tier exists to read
	if err != nil {
		return labContract{}, fmt.Errorf("read live-target contract: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()

	var contract labContract
	if decodeErr := decoder.Decode(&contract); decodeErr != nil {
		return labContract{}, fmt.Errorf("decode live-target contract: %w: %w", decodeErr, errs.ErrMalformedInput)
	}

	if decoder.More() {
		return labContract{}, fmt.Errorf("live-target contract has trailing data: %w", errs.ErrMalformedInput)
	}

	if contract.Version != contractVersion {
		return labContract{}, fmt.Errorf(
			"live-target contract version must be %d, got %d: %w", contractVersion, contract.Version, errs.ErrValidation)
	}

	return contract, nil
}

// endpointFor returns the single endpoint of the given kind.
//
// Selection is by kind rather than by name because this suite addresses a forge
// by what it is, not by what an operator called it. Two endpoints of one kind
// is an ambiguity the suite refuses rather than resolves: picking the first
// would make which fixtures get destroyed depend on file ordering.
func (c labContract) endpointFor(kind string) (labEndpoint, error) {
	var found []labEndpoint

	for _, endpoint := range c.Endpoints {
		if endpoint.Kind == kind {
			found = append(found, endpoint)
		}
	}

	switch len(found) {
	case 0:
		return labEndpoint{}, fmt.Errorf("contract selects no %s endpoint: %w", kind, errs.ErrValidation)
	case 1:
	default:
		return labEndpoint{}, fmt.Errorf("contract carries %d %s endpoints; expected exactly one: %w",
			len(found), kind, errs.ErrValidation)
	}

	endpoint := found[0]
	if err := endpoint.validateComplete(); err != nil {
		return labEndpoint{}, err
	}

	return endpoint, nil
}

// validateComplete refuses a selected endpoint that is missing anything this
// suite needs. The neutral contract permits an unselected endpoint to be
// incomplete; a selected one must not be.
func (e labEndpoint) validateComplete() error {
	if e.Name == "" {
		return fmt.Errorf("%s endpoint has no name: %w", e.Kind, errs.ErrValidation)
	}

	if _, err := e.host(); err != nil {
		return err
	}

	if e.Credential.Token == "" {
		return fmt.Errorf("%s endpoint carries no token: %w", e.Kind, errs.ErrValidation)
	}

	if e.Credential.Name == "" {
		return fmt.Errorf("%s credential has no name: %w", e.Kind, errs.ErrValidation)
	}

	return nil
}

// host projects the API base URL down to the host[:port] the adapters address.
//
// The API root is the authority here, not the web root: it is what this suite
// actually calls. They differ on hosted providers -- github.com against
// api.github.com -- and taking the wrong one would allowlist a host the suite
// never contacts while leaving the one it does unchecked.
func (e labEndpoint) host() (string, error) {
	parsed, err := url.Parse(e.APIBaseURL)
	if err != nil {
		return "", fmt.Errorf("%s api_base_url is not a URL: %w: %w", e.Kind, err, errs.ErrValidation)
	}

	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%s api_base_url must be https, got %q: %w", e.Kind, parsed.Scheme, errs.ErrValidation)
	}

	if parsed.User != nil {
		return "", fmt.Errorf("%s api_base_url must not carry userinfo: %w", e.Kind, errs.ErrValidation)
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s api_base_url must not carry a query or fragment: %w", e.Kind, errs.ErrValidation)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("%s api_base_url has no host: %w", e.Kind, errs.ErrValidation)
	}

	return parsed.Host, nil
}

// tokenMeta projects the credential onto the shape the destructive guard
// checks. The guard's rules are unchanged by the move to a file: a credential
// must still be run-bound and must still not outlive the run.
func (e labEndpoint) tokenMeta(generationID string) tokenMetadata {
	meta := tokenMetadata{
		name:      e.Credential.Name,
		createdAt: e.Credential.CreatedAt,
	}

	if e.Credential.ProviderID != nil {
		meta.id = *e.Credential.ProviderID
	}

	if e.Credential.ExpiresAt != nil {
		meta.expiresAt = *e.Credential.ExpiresAt
	}

	// Revocation metadata is carried only when the producer says revocation is
	// required. A credential with a native expiry needs none, and reporting a
	// generation for it anyway reads to the guard as an expiry contradicted by
	// a revocation fallback -- which is a refusal, not a stricter check.
	if !e.Credential.Revocation.Required {
		return meta
	}

	meta.revocationRequired = true

	// The revocation must name this run. One naming another generation is not
	// this run's to rely on, because this run's cleanup would not revoke it, so
	// it is left blank for the guard to reject.
	if e.Credential.Revocation.GenerationID != nil && *e.Credential.Revocation.GenerationID == generationID {
		meta.revocationRunID = generationID
	}

	return meta
}

// cleanupCommand is the producer's idempotent credential-revocation entry
// point, or empty when it exposes none.
func (c labContract) cleanupCommand() string {
	if c.Interfaces == nil || c.Interfaces.CredentialCleanup == nil {
		return ""
	}

	return *c.Interfaces.CredentialCleanup
}
