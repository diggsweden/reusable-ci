// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxInputBytes = 1024 * 1024

const (
	modeIDToken            = "id-token"
	modeFulcioCertificates = "fulcio-certificates"
)

var errInvalidProbeJSON = errors.New("invalid probe JSON")

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string, input io.Reader, output io.Writer) error { //nolint:cyclop // Two bounded decode modes share input and output enforcement.
	if len(args) != 1 || (args[0] != modeIDToken && args[0] != modeFulcioCertificates) {
		return fmt.Errorf("%w: usage: probe-json id-token|fulcio-certificates", errInvalidProbeJSON)
	}

	body, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxInputBytes {
		return fmt.Errorf("%w: input must be non-empty and at most %d bytes", errInvalidProbeJSON, maxInputBytes)
	}

	var values []string

	switch args[0] {
	case modeIDToken:
		value, decodeErr := decodeIDToken(body)
		if decodeErr != nil {
			return decodeErr
		}

		values = []string{value}
	case modeFulcioCertificates:
		values, err = decodeFulcioCertificates(body)
		if err != nil {
			return err
		}
	}

	for _, value := range values {
		if _, err = fmt.Fprintln(output, value); err != nil {
			return fmt.Errorf("write probe JSON result: %w", err)
		}
	}

	return nil
}

func decodeIDToken(body []byte) (string, error) {
	var response struct {
		Value string `json:"value"`
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if decodeErr := decoder.Decode(&response); decodeErr != nil || response.Value == "" {
		return "", fmt.Errorf("%w: response has no id token", errInvalidProbeJSON)
	}

	if eofErr := requireJSONEOF(decoder); eofErr != nil {
		return "", eofErr
	}

	return response.Value, nil
}

func decodeFulcioCertificates(body []byte) ([]string, error) {
	var response struct {
		Chains []struct {
			Certificates []string `json:"certificates"`
		} `json:"chains"`
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if decodeErr := decoder.Decode(&response); decodeErr != nil || len(response.Chains) == 0 || len(response.Chains[0].Certificates) == 0 {
		return nil, fmt.Errorf("%w: response has no Fulcio certificates", errInvalidProbeJSON)
	}

	if eofErr := requireJSONEOF(decoder); eofErr != nil {
		return nil, eofErr
	}

	for _, certificate := range response.Chains[0].Certificates {
		if certificate == "" {
			return nil, fmt.Errorf("%w: response has an empty Fulcio certificate", errInvalidProbeJSON)
		}
	}

	return response.Chains[0].Certificates, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON data", errInvalidProbeJSON)
	}

	return nil
}
