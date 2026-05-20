// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func FuzzParseConfig(f *testing.F) {
	seeds := [][]byte{
		nil,
		[]byte(""),
		[]byte("artifacts:\n  - name: my-app\n    project-type: maven\n"),
		[]byte("artifacts:\n  - name: backend\n    project-type: maven\ncontainers:\n  - name: app\n    from: [backend]\n"),
		[]byte("not: valid: yaml: at: all: \n  - "),
		[]byte("artifacts: []\n"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := config.Parse(data)
		if err != nil {
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			return
		}

		if cfg == nil {
			t.Fatalf("nil config for successful parse of %q", string(data))
		}
	})
}
