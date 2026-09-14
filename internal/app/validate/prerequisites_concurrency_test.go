// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// overlapWriter records whether two writes were ever in progress at once.
// Each write holds itself open briefly so that an unserialised second writer
// overlaps it instead of slipping in after it by luck.
type overlapWriter struct {
	inFlight atomic.Int32
	overlaps atomic.Int32

	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *overlapWriter) Write(p []byte) (int, error) {
	if w.inFlight.Add(1) > 1 {
		w.overlaps.Add(1)
	}

	time.Sleep(5 * time.Millisecond)

	w.mu.Lock()
	w.buf.Write(p)
	w.mu.Unlock()

	w.inFlight.Add(-1)

	return len(p), nil
}

// TestPrerequisites_ConcurrentAnnotationsAreSerialisedAndComplete enables four
// checks that annotate while they run concurrently: the tag signature with no
// allowlist, missing Maven Central secrets, and Cargo and JVM checks over real
// temp manifests. No two annotation writes overlap, every annotation arrives
// whole and exactly once, and the ordinary output and the recorded checks
// follow the canonical order however the goroutines were scheduled.
//
// Not parallel: the Cargo and JVM checks read the working directory.
func TestPrerequisites_ConcurrentAnnotationsAreSerialisedAndComplete(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.WriteFile("services/api/pom.xml", []byte(reproduciblePOM))
	fsys.Chdir()

	plan := validationPlanJSON(t, validationConfigPlan(t,
		config.Artifact{Name: "api", ProjectType: projecttype.Cargo, WorkingDirectory: "crates/api"},
		config.Artifact{Name: "service", ProjectType: projecttype.Maven, WorkingDirectory: "services/api"},
	))

	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithBotPermissions(provider.BotPermissions{
		UserAccessible: true, RepoAccessible: true, BranchesAccessible: true,
	})

	annotations := &overlapWriter{}

	var out bytes.Buffer

	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  newHistoryGit([]string{"c0", "c1"}, "", "", "c1"),
		Provider: fp,
		Cargo:    fakeCargoTool{version: "cargo 1.90.0"},
	}, &out, output.NewAnnotator(annotations, output.FormatGitHub), appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic token and key markers; nothing real is read.
		RefType: "tag", Tag: rerunRequestTag, Ref: "refs/tags/" + rerunRequestTag, Branch: "main",
		ReleaseToken: "github_pat_AAAA", Repository: "owner/repo", ReleaseGPGPublicKey: "test-key",
		RequiresGPGSigning: true, HasMavenCentralTarget: true, HasCargoTarget: true, HasJVMTarget: true,
		ConfigPlanJSON: plan,
	})

	if n := annotations.overlaps.Load(); n != 0 {
		t.Errorf("%d annotation writes overlapped", n)
	}

	got := strings.Split(strings.TrimSuffix(annotations.buf.String(), "\n"), "\n")
	slices.Sort(got)

	want := []string{
		"::error::Missing MAVEN_CENTRAL_USERNAME secret",
		"::notice::Cargo.lock present in crates/api",
		"::notice::Maven reproducibility configured in services/api (outputTimestamp=2026-01-01T00:00:00Z)",
		"::notice::Toolchain pin present in crates/api",
		"::warning title=No release signer allowlist::GPG release ran with NO signer allowlist — any valid signature was accepted. Commit .reusable-ci/allowed_gpg_keys.asc to authorise specific signers.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("annotations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	names := make([]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		names = append(names, check.Name)
	}

	wantNames := []string{"ref-type", "tag-format", "tag-uniqueness", "tag-commit", "tag-signature", "final-tag-rerun", "gpg-public-key", "release-token", "bot-permissions", "maven-central", "cargo", "jvm-reproducibility"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("checks = %v, want %v", names, wantNames)
	}

	position := 0

	for _, check := range result.Checks {
		if check.Output == "" {
			continue
		}

		index := strings.Index(out.String()[position:], check.Output)
		if index < 0 {
			t.Fatalf("output of %s missing or out of order:\n%s", check.Name, out.String())
		}

		position += index + len(check.Output)
	}
}
