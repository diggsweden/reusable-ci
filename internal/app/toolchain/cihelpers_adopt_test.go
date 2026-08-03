// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"testing"
)

// plainRunner satisfies MiseRunner without the SetBin capability.
type plainRunner struct{}

func (plainRunner) Run(context.Context, []string, ...string) (string, error) { return "", nil }

// binRecordingRunner additionally records SetBin calls.
type binRecordingRunner struct {
	plainRunner

	bin string
}

func (r *binRecordingRunner) SetBin(bin string) { r.bin = bin }

// TestAdoptInstalledMise pins the PATH-resolution fix: after an in-process
// mise install, the runner must be pointed at the installed binary — a bare
// "mise" resolves against the parent PATH, which cannot see ~/.local/bin
// entries written this step (nanolinter's v0.8.6 release failure).
func TestAdoptInstalledMise(t *testing.T) {
	t.Parallel()

	r := &binRecordingRunner{}
	adoptInstalledMise(r, "/root/.local/bin/mise")

	if r.bin != "/root/.local/bin/mise" {
		t.Fatalf("runner not pinned to installed mise, bin=%q", r.bin)
	}

	// Empty path (installer reported nothing) leaves the runner alone.
	r2 := &binRecordingRunner{}
	adoptInstalledMise(r2, "")

	if r2.bin != "" {
		t.Fatalf("empty install path must not pin the runner, bin=%q", r2.bin)
	}

	// A runner without the capability is tolerated.
	adoptInstalledMise(plainRunner{}, "/x/mise")
}
