// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package npm_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestNewAndNewNpx_ChooseTheirOwnBinary replaces a TestNew that asserted only
// that New() was non-nil. New returns &Adapter{}, and the address of a struct
// literal is never nil, so that test could not fail -- and every other test in
// this package constructs an adapter anyway, so it covered nothing besides.
//
// The distinction worth pinning is the one the two constructors exist for.
// NewNpx drives npx, which is how the pinned one-shot SBOM tool (cyclonedx-npm)
// is run from build/npm.go. Were NewNpx to lose its Bin and fall back to npm,
// the tool would be invoked through the wrong runner and fail during a release,
// with nothing here to catch it. Both other tests set Bin explicitly, so the
// default this asserts was previously unexercised.
func TestNewAndNewNpx_ChooseTheirOwnBinary(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("npm", `printf 'ran npm\n'`)
	m.Add("npx", `printf 'ran npx\n'`)

	for _, tc := range []struct {
		name    string
		adapter *npm.Adapter
		want    string
	}{
		{name: "New defaults to npm", adapter: npm.New(), want: "ran npm"},
		{name: "NewNpx drives npx", adapter: npm.NewNpx(), want: "ran npx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			if err := tc.adapter.RunInherit(context.Background(), "", &stdout, &stderr); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(stdout.String(), tc.want) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestAdapter_RunInherit(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	m.Add("npm", `printf 'npm %s\n' "$*"; printf 'warn\n' >&2`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	var stdout, stderr bytes.Buffer
	if err := a.RunInherit(context.Background(), "", &stdout, &stderr, "version", "1.2.3", "--no-git-tag-version"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "npm version 1.2.3 --no-git-tag-version") {
		t.Errorf("stdout = %q", stdout.String())
	}

	if strings.TrimSpace(stderr.String()) != "warn" {
		t.Errorf("stderr = %q", stderr.String())
	}

	want := []string{"version", "1.2.3", "--no-git-tag-version"}
	if got := m.Invocations("npm")[0].Args; !slices.Equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestAdapter_RunInheritWrapsFailure(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `exit 7`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	err := a.RunInherit(context.Background(), "", &bytes.Buffer{}, &bytes.Buffer{}, "version", "1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("exit 7 = %v, want ErrValidation", err)
	}

	// The subcommand is in the message so the failing step is identifiable
	// from the log alone.
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should name the subcommand: %v", err)
	}
}

func TestAdapter_RunCapturesOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `printf 'out'; printf 'err' >&2`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	stdout, stderr, err := a.Run(context.Background(), "", "pack", "--json")
	if err != nil {
		t.Fatal(err)
	}

	if stdout != "out" || stderr != "err" {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestAdapter_RunCapturesExitStderr(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("npm", `printf 'missing' >&2; exit 1`)
	a := &npm.Adapter{Bin: m.Path("npm")}

	_, stderr, err := a.Run(context.Background(), "", "view", "pkg@1.0.0", "version")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(stderr, "missing") {
		t.Errorf("stderr = %q", stderr)
	}
}
