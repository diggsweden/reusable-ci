// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package secrettext handles diagnostic text only, not credential verification.
package secrettext

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

//nolint:gochecknoglobals // immutable token-shape matcher.
var compactJWS = regexp.MustCompile(`([A-Za-z0-9_-]+)\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)

// ContainsJWT recognizes compact JWS-shaped text with a JSON algorithm header.
// Header spelling/whitespace is irrelevant. Oversized candidates are treated as
// sensitive without decoding; the detector never verifies credentials.
func ContainsJWT(body []byte) bool {
	for match := compactJWS.FindSubmatchIndex(body); match != nil; match = compactJWS.FindSubmatchIndex(body) {
		header := body[match[2]:match[3]]
		if len(header) > 16<<10 {
			return true
		}

		decoded, err := base64.RawURLEncoding.DecodeString(string(header))
		if err == nil {
			var value struct {
				Algorithm string `json:"alg"`
			}
			if json.Unmarshal(decoded, &value) == nil && value.Algorithm != "" {
				return true
			}
		}

		body = body[match[1]:]
	}

	return false
}

// RedactError preserves errors.Is classifications without exposing a raw
// secret-bearing cause via errors.As or an unwrap chain.
func RedactError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}

	message := err.Error()

	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}

	return &redactedError{cause: err, message: message}
}

type redactedError struct {
	cause   error
	message string
}

func (e *redactedError) Error() string        { return e.message }
func (e *redactedError) Is(target error) bool { return errors.Is(e.cause, target) }
