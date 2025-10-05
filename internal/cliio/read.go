// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The extra byte distinguishes exact-cap input from overflow, even when a
// regular file grows after its descriptor's size check. Never return partial data.
func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxReadSize+1))
	if err != nil {
		return nil, err
	}

	if len(body) > maxReadSize {
		return nil, fmt.Errorf("input exceeds the %d MiB bound: %w", maxReadSize>>20, errs.ErrMalformedInput)
	}

	return body, nil
}
