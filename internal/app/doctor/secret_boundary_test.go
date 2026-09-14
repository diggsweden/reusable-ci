// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/doctor"
)

func TestDoctorSecretBoundary_FieldOnlyDiagnostics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ method, field, value string }{
		{"gpg", "key", "invented-key-canary"}, {"sigstore", "key", "invented-key-canary"},
		{"kms", "key", "invented-key-canary"}, {"kms", "key", "invented-key-canary://resource"},
		{"gpg", "oidc-issuer", "invented-issuer-canary"}, {"kms", "oidc-issuer", "invented-issuer-canary"},
		{"sigstore", "oidc-issuer", "http://invented-issuer-canary"}, {"sigstore", "oidc-issuer", "https://invented-issuer-canary%"},
		{"sigstore", "oidc-issuer", "https:invented-issuer-canary"},
	} {
		t.Run(tc.method+"/"+tc.value, func(t *testing.T) {
			config := fmt.Sprintf("artifacts:\n  - name: app\n    project-type: meta\nsign:\n  method: %s\n  %s: %q\n", tc.method, tc.field, tc.value)
			if tc.method == "kms" && tc.field == "oidc-issuer" {
				config += "  key: file:fake-public-key\n"
			}

			root := writeRepo(t, config, nil)

			checks, err := doctor.Run(doctor.Input{Root: root})
			if err != nil {
				t.Fatal(err)
			}

			if doctor.CountFailures(checks) == 0 {
				t.Fatal("invalid signing config passed")
			}

			var text, encoded bytes.Buffer
			doctor.FormatText(&text, checks)

			if err := doctor.FormatJSON(&encoded, doctor.Report{Checks: checks, Failures: doctor.CountFailures(checks)}); err != nil {
				t.Fatal(err)
			}

			for _, body := range []string{fmt.Sprint(checks), text.String(), encoded.String()} {
				if strings.Contains(body, "invented-key-canary") || strings.Contains(body, "invented-issuer-canary") {
					t.Fatalf("sensitive diagnostic: %s", body)
				}

				if !strings.Contains(body, "sign."+tc.field) {
					t.Fatalf("field not identified: %s", body)
				}
			}
		})
	}
}
