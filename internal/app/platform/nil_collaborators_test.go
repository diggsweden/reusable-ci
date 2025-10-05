// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestPlatformUseCases_MissingCollaboratorsAreUsageErrorsWithoutEffects: a
// nil Git port, Git factory, output sink or diagnostic writer is a wiring
// mistake reported as a usage error before anything is created or called.
// The optional human writers of Checkout and ResolveRef stay optional.
func TestPlatformUseCases_MissingCollaboratorsAreUsageErrorsWithoutEffects(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join(t.TempDir(), "checkout")
	in := appplatform.CheckoutInput{Repository: "owner/repo", ServerURL: "https://forge.example", Ref: "main", Workspace: workspace}

	if _, err := appplatform.Checkout(t.Context(), nil, fakeoutputsink.New(t), nil, in); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("nil git factory: err = %v", err)
	}

	git := &fakeCheckoutGit{}
	if _, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), nil, nil, in); !errors.Is(err, errs.ErrUsage) || len(git.events) != 0 {
		t.Errorf("nil sink: err = %v, git calls %d", err, len(git.events))
	}

	if _, err := os.Stat(filepath.Dir(workspace)); err != nil {
		t.Fatal(err)
	}

	if entries, _ := os.ReadDir(filepath.Dir(workspace)); len(entries) != 0 {
		t.Errorf("refusals created %v", entries)
	}

	refIn := appplatform.ResolveRefInput{RemoteURL: "https://forge.example/owner/repo.git", Ref: "main"}
	if _, err := appplatform.ResolveRef(t.Context(), nil, fakeoutputsink.New(t), nil, refIn); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("resolve-ref nil git: err = %v", err)
	}

	recorder := &refRecorder{}
	if _, err := appplatform.ResolveRef(t.Context(), recorder, nil, nil, refIn); !errors.Is(err, errs.ErrUsage) || len(recorder.calls) != 0 {
		t.Errorf("resolve-ref nil sink: err = %v, git calls %v", err, recorder.calls)
	}

	if err := appplatform.DebugWorkspace(nil, appplatform.DebugWorkspaceInput{Root: testfs.NewReal(t).Root}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("debug-workspace nil writer: err = %v", err)
	}
}
