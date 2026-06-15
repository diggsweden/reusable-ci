// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build unix

package cliio_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/cliio"
)

// TestWithLock_SerializesReadModifyWrite is the regression for the ledger
// lost-update race: without the exclusive lock, concurrent read-modify-
// write would interleave and drop increments, so the final count would be
// less than n.
func TestWithLock_SerializesReadModifyWrite(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "counter")
	if err := os.WriteFile(counter, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}

	const n = 50

	var wg sync.WaitGroup

	for range n {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = cliio.WithLock(counter, func() error {
				b, err := os.ReadFile(counter) //nolint:gosec // test fixture path
				if err != nil {
					return err
				}

				v, _ := strconv.Atoi(strings.TrimSpace(string(b)))

				return os.WriteFile(counter, []byte(strconv.Itoa(v+1)), 0o600)
			})
		}()
	}

	wg.Wait()

	b, err := os.ReadFile(counter) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	got, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if got != n {
		t.Errorf("counter = %d, want %d — lost updates mean locking is broken", got, n)
	}
}
