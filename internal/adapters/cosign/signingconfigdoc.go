// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The Sigstore signing config: which services a signing run uses.
//
// cosign 3.x deprecates the --fulcio-url/--rekor-url/--oidc-issuer flags and
// rejects them alongside a signing config ("cannot specify service URLs and use
// signing config"). This adapter already passes a config whenever the
// transparency log is off, so the endpoints go in the document too.
//
// Built from typed values and verified against `cosign signing-config create`:
// the same inputs produce the same document. Shelling out to cosign to generate
// it would mean a second invocation per signing call.

const signingConfigMediaType = "application/vnd.dev.sigstore.signingconfig.v0.2+json"

// signingConfigOperator names who runs each service. cosign requires the field
// and does not interpret it.
const signingConfigOperator = "self-hosted"

// signingConfigService is one service entry: where it is, which major API it
// speaks, from when it is valid, and who runs it.
type signingConfigService struct {
	URL             string             `json:"url"`
	MajorAPIVersion int                `json:"majorApiVersion"`
	ValidFor        signingConfigValid `json:"validFor"`
	Operator        string             `json:"operator"`
}

type signingConfigValid struct {
	Start string `json:"start"`
}

// signingConfigDoc is the document cosign reads. rekorTlogConfig and tsaConfig
// are always present, even when empty, matching cosign's own generator.
type signingConfigDoc struct {
	MediaType       string                 `json:"mediaType"`
	CAUrls          []signingConfigService `json:"caUrls,omitempty"`
	OIDCUrls        []signingConfigService `json:"oidcUrls,omitempty"`
	RekorTlogUrls   []signingConfigService `json:"rekorTlogUrls,omitempty"`
	RekorTlogConfig map[string]string      `json:"rekorTlogConfig"`
	TSAConfig       map[string]string      `json:"tsaConfig"`
}

// signingConfigInput is where the services are. Whether the run publishes is
// the adapter's own setting, passed separately.
type signingConfigInput struct {
	FulcioURL  string
	OIDCIssuer string
	RekorURL   string
}

// needed reports whether cosign has to be told anything at all. With every
// service at its default and the transparency log on, cosign's own
// configuration is already correct.
func (in signingConfigInput) needed(publishes bool) bool {
	return in.FulcioURL != "" || in.OIDCIssuer != "" || in.RekorURL != "" || !publishes
}

// buildSigningConfig renders the document for these services. validFrom is
// passed in rather than read from the clock so the result is reproducible.
func buildSigningConfig(in signingConfigInput, publishes bool, validFrom time.Time) ([]byte, error) {
	start := validFrom.UTC().Format(time.RFC3339)

	service := func(url string) []signingConfigService {
		return []signingConfigService{{
			URL:             url,
			MajorAPIVersion: 1,
			ValidFor:        signingConfigValid{Start: start},
			Operator:        signingConfigOperator,
		}}
	}

	doc := signingConfigDoc{
		MediaType:       signingConfigMediaType,
		RekorTlogConfig: map[string]string{},
		TSAConfig:       map[string]string{},
	}

	if in.FulcioURL != "" {
		doc.CAUrls = service(in.FulcioURL)
	}

	if in.OIDCIssuer != "" {
		doc.OIDCUrls = service(in.OIDCIssuer)
	}

	// A log is named only when this run publishes to one: the selector tells
	// cosign a log must be used, so an entry here with transparency off would
	// contradict the rest of the document.
	if in.RekorURL != "" && publishes {
		doc.RekorTlogUrls = service(in.RekorURL)
		doc.RekorTlogConfig = map[string]string{"selector": "ANY"}
	}

	// Marshal cannot fail for this struct; the error is returned for future
	// fields that could.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("cosign: render signing config: %w: %w", err, errs.ErrValidation)
	}

	return encoded, nil
}
