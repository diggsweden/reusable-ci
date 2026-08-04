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
// This is how cosign 3.x wants to be told, and the only way it will listen. The
// service flags that used to do it -- --fulcio-url, --rekor-url, --oidc-issuer
// -- are deprecated there, and worse, they are REFUSED alongside a signing
// config:
//
//	Error: cannot specify service URLs and use signing config
//
// Since this adapter already passes a signing config whenever the transparency
// log is off, a self-hosted Sigstore with no public log -- the combination such
// a deployment actually runs -- could not work through the flags at all. So the
// endpoints belong in this document, and there is one mechanism rather than two.
//
// Built from typed values rather than assembled as text, and verified against
// `cosign signing-config create`: the same inputs produce the same document,
// field for field. Generating it by shelling out to cosign was the alternative
// and was rejected for the reason the literal it replaces gives -- that would be
// a second cosign invocation on every signing call, to produce a document whose
// shape is pinned by its own media type.

// signingConfigMediaType pins the document format. It is the one thing here
// that can rot under a cosign upgrade, which is why it is a constant rather
// than spelled inline: a format bump is then a one-line change with a name.
const signingConfigMediaType = "application/vnd.dev.sigstore.signingconfig.v0.2+json"

// signingConfigOperator is the operator name attached to each service entry.
//
// cosign requires the field and does not interpret it -- it is provenance for a
// human reading the document later, saying who runs the service. "self-hosted"
// is the honest answer for anything this adapter is pointed at: if it were a
// public Sigstore service, no entry would be written for it.
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

// signingConfigDoc is the document cosign reads.
//
// rekorTlogConfig and tsaConfig are always present, even when empty: cosign's
// own generator emits them that way, and an absent config is not the same as an
// empty one to a reader deciding whether a log was deliberately omitted.
type signingConfigDoc struct {
	MediaType       string                 `json:"mediaType"`
	CAUrls          []signingConfigService `json:"caUrls,omitempty"`
	OIDCUrls        []signingConfigService `json:"oidcUrls,omitempty"`
	RekorTlogUrls   []signingConfigService `json:"rekorTlogUrls,omitempty"`
	RekorTlogConfig map[string]string      `json:"rekorTlogConfig"`
	TSAConfig       map[string]string      `json:"tsaConfig"`
}

// signingConfigInput is where the services are. Whether the run publishes is
// not here: that is the adapter's own setting, and a field the caller fills in
// only to have it overwritten is a field someone will eventually trust.
type signingConfigInput struct {
	FulcioURL  string
	OIDCIssuer string
	RekorURL   string
}

// needed reports whether cosign has to be told anything at all. With every
// service left at its default and the transparency log on, cosign's own
// configuration is correct and passing a document could only diverge from it.
func (in signingConfigInput) needed(publishes bool) bool {
	return in.FulcioURL != "" || in.OIDCIssuer != "" || in.RekorURL != "" || !publishes
}

// buildSigningConfig renders the document for these services.
//
// validFrom is passed in rather than read from the clock so the result is
// reproducible and the tests can assert on it. Services carry a start time
// because the format requires one; it is the moment the run began, which is the
// only honest answer for a service this run was told about.
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

	// A log is named only when this run publishes to one. Naming a log while
	// transparency is off would contradict the whole point of the document: the
	// selector is what tells cosign a log must be used, so an entry with the log
	// turned off is either ignored or obeyed, and neither is a good surprise.
	if in.RekorURL != "" && publishes {
		doc.RekorTlogUrls = service(in.RekorURL)
		doc.RekorTlogConfig = map[string]string{"selector": "ANY"}
	}

	// Marshal of a struct of strings and ints cannot fail, so the error is
	// returned rather than swallowed only to keep the signature honest for a
	// future field that could.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("cosign: render signing config: %w: %w", err, errs.ErrValidation)
	}

	return encoded, nil
}
