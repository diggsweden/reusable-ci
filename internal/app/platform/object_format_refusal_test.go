// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// TestCheckout_CheckedOutHeadMustMatchTheFormatInBothDirections completes the
// late half of the object-format matrix: after Git ran, a HEAD of the other
// width (a SHA-256 commit under sha1, a SHA-1 commit under sha256) or a
// malformed one is refused with no output, no summary line, and the new
// checkout directory never published. The matching widths are the controls.
func TestCheckout_CheckedOutHeadMustMatchTheFormatInBothDirections(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		format, head string
		ok           bool
	}{
		{format: "sha1", head: strings.Repeat("a", 40), ok: true},
		{format: "sha256", head: strings.Repeat("b", 64), ok: true},
		{format: "sha1", head: strings.Repeat("b", 64)},
		{format: "sha256", head: strings.Repeat("a", 40)},
		{format: "sha256", head: strings.Repeat("A", 64)},
		{format: "sha256", head: strings.Repeat("c", 63)},
	} {
		t.Run(tc.format+"/"+tc.head, func(t *testing.T) {
			t.Parallel()

			in := baseInput(t, "main")
			in.ObjectFormat = tc.format
			in.Workspace = filepath.Join(in.Workspace, "checkout")

			git := &fakeCheckoutGit{headSHA: tc.head}
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			sha, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, in)
			if git.initFormat != tc.format {
				t.Errorf("init format = %q, want %q", git.initFormat, tc.format)
			}

			if tc.ok {
				if err != nil || sha != tc.head || sink.Single("checkout-sha") != tc.head {
					t.Fatalf("err = %v, sha = %q, output = %q", err, sha, sink.Single("checkout-sha"))
				}

				return
			}

			if !errors.Is(err, errs.ErrValidation) || sha != "" || len(sink.Keys()) != 0 || out.Len() != 0 {
				t.Fatalf("err = %v, sha = %q, keys = %v, out = %q", err, sha, sink.Keys(), &out)
			}

			if _, statErr := os.Stat(in.Workspace); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("refused checkout was published at %s (%v)", in.Workspace, statErr)
			}
		})
	}
}
