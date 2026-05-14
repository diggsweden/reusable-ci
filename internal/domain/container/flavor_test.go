// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
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
	for _, in := range []string{"latest=true", "latest=auto"} {
		err := container.ValidateFlavor(in)
		if err == nil || !strings.Contains(err.Error(), "latest=auto/true is not supported") {
			t.Errorf("%q should be refused: err=%v", in, err)
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
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q → err=%v, want %q", in, err, want)
		}
	}
}

func TestValidateFlavor_UnknownEntry(t *testing.T) {
	t.Parallel()
	err := container.ValidateFlavor("nonsense")
	if err == nil || !strings.Contains(err.Error(), "unknown FLAVOR entry") {
		t.Errorf("err = %v", err)
	}
}
