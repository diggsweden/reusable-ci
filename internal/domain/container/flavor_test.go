// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidateFlavor_Empty(t *testing.T) {
	t.Parallel()

	if err := container.ValidateFlavor(""); err != nil {
		t.Errorf("empty flavor should be valid: %v", err)
	}
}

func TestValidateFlavor_LatestFalse(t *testing.T) {
	t.Parallel()

	if err := container.ValidateFlavor("latest=false"); err != nil {
		t.Errorf("latest=false should be valid: %v", err)
	}
}

func TestValidateFlavor_LatestTrueRefused(t *testing.T) {
	t.Parallel()

	// A capability this tool deliberately does not implement is
	// ErrUnsupported (EX_CONFIG, 78) -- permanent, not something a retry or a
	// corrected flag value fixes.
	for _, in := range []string{"latest=true", "latest=auto"} {
		err := container.ValidateFlavor(in)
		if !errors.Is(err, errs.ErrUnsupported) {
			t.Errorf("%q: err = %v, want ErrUnsupported", in, err)

			continue
		}

		if !strings.Contains(err.Error(), "latest=auto/true is not supported") {
			t.Errorf("%q: err = %v, want it to name the unsupported entry", in, err)
		}
	}
}

func TestValidateFlavor_PrefixSuffixOnlatest(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"prefix=v":   "FLAVOR prefix is not supported",
		"suffix=-rc": "FLAVOR suffix is not supported",
		"onlatest=v": "FLAVOR onlatest is not supported",
	}
	for in, want := range cases {
		err := container.ValidateFlavor(in)
		if !errors.Is(err, errs.ErrUnsupported) {
			t.Errorf("%q: err = %v, want ErrUnsupported", in, err)

			continue
		}

		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q -> err=%v, want %q", in, err, want)
		}
	}
}

func TestValidateFlavor_UnknownEntry(t *testing.T) {
	t.Parallel()

	err := container.ValidateFlavor("nonsense")
	// Unlike the unsupported-but-recognised entries above, an entry nobody
	// recognises is a typo in the operator's own input: ErrUsage.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "unknown FLAVOR entry") {
		t.Errorf("err = %v, want it to say the entry is unknown", err)
	}
}

// TestValidateFlavor_ALateUnsupportedEntryIsRefused covers an accepted entry
// followed by a refused one. Every existing case has a single entry, so a
// validator that returned after checking the first line would pass them all.
func TestValidateFlavor_ALateUnsupportedEntryIsRefused(t *testing.T) {
	t.Parallel()

	if err := container.ValidateFlavor("latest=false\n\nprefix=v"); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for the second entry", err)
	}

	if err := container.ValidateFlavor("latest=false\nnonsense"); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage for the unknown second entry", err)
	}
}
