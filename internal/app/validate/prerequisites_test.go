// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// stubGit returns canned git answers for every call the orchestrator
// makes. Behavior here is deliberately permissive; tests that need a
// specific failure inject errors.
type stubGit struct {
	uniqueTagsErr error
	tagSHA        string
	verifyOK      bool
}

func (s stubGit) RevParse(_ context.Context, ref string) (string, error) { return "abc1234", nil }
func (s stubGit) TagsPointingAt(_ context.Context, _ string) ([]string, error) {
	if s.uniqueTagsErr != nil {
		return nil, s.uniqueTagsErr
	}

	return []string{"v1.0.0"}, nil //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}
func (s stubGit) IsAncestor(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (s stubGit) CatFileType(_ context.Context, _ string) (string, error) { return "tag", nil } //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
func (s stubGit) CatFileTag(_ context.Context, _ string) (string, error) {
	return "object 0000\ntype commit\ntag v1.0.0\ntagger x <x@y> 0 +0000\n\nmsg\n-----BEGIN PGP SIGNATURE-----\n", nil
}
func (s stubGit) VerifyTagSignature(_ context.Context, _ string, armor []byte) (string, string, bool, error) {
	if len(armor) == 0 || !s.verifyOK {
		return "", "", false, nil
	}

	return "x <x@y>", "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD", true, nil
}

func (s stubGit) VerifyTagSSHAgainstAllowedSigners(_ context.Context, _, _ string) (bool, string, error) {
	// SSH path: tests use armor signatures (GPG), so this never runs.
	// Return ok=true with empty output as a defensive default.
	return true, "", nil
}
func (s stubGit) TaggerInfo(_ context.Context, _ string) (git.TaggerInfo, error) {
	return git.TaggerInfo{Tagger: "x", Date: "x@y"}, nil
}
func (s stubGit) TagMessage(_ context.Context, _ string) (string, error) { return "msg", nil }
func (s stubGit) TagSHA(_ context.Context, _ string) (string, error)     { return s.tagSHA, nil }

func TestPrerequisites_NonTagRefSkipsTagChecks(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	result, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "branch",
		Tag:          "main",
		Ref:          "refs/heads/main",
		ReleaseToken: "ghp_dummy",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Repository:   "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})

	// RefType("branch") errors with guidance — that's by design.
	// The test asserts skips are recorded for tag-only checks.
	if err == nil {
		t.Errorf("expected ref-type validator to fail on non-tag trigger")
	}

	skipped := map[string]bool{}

	for _, c := range result.Checks {
		if c.Skipped {
			skipped[c.Name] = true
		}
	}

	for _, want := range []string{"tag-format", "tag-uniqueness", "tag-commit", "tag-signature"} {
		if !skipped[want] {
			t.Errorf("expected %s to be skipped on non-tag ref", want)
		}
	}
}

func TestPrerequisites_TagRefRunsAllTagChecks(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "tag",
		Tag:          "v1.0.0",
		Ref:          "refs/tags/v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseToken: "ghp_dummy",
		Repository:   "owner/repo",
	})

	skipped := map[string]bool{}

	for _, c := range result.Checks {
		if c.Skipped {
			skipped[c.Name] = true
		}
	}

	for _, mustRun := range []string{"tag-format", "tag-uniqueness", "tag-commit", "tag-signature"} {
		if skipped[mustRun] {
			t.Errorf("%s should have run on tag-trigger", mustRun)
		}
	}
}

func TestPrerequisites_PolicyFlagsGateOptionalChecks(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:               "tag",
		Tag:                   "v1.0.0",
		Ref:                   "refs/tags/v1.0.0",
		ReleaseToken:          "ghp_dummy",
		Repository:            "owner/repo",
		SignArtifacts:         false,
		HasMavenCentralTarget: false,
		HasCargoTarget:        false,
	})

	skipped := map[string]string{}

	for _, c := range result.Checks {
		if c.Skipped {
			skipped[c.Name] = c.SkipReason
		}
	}

	// Release authorisation no longer has its own validator — it's now
	// folded into tag-signature via the allowed_signers /
	// allowed_gpg_keys.asc files. Only the per-flag skips remain.
	for _, want := range []string{"gpg-public-key", "maven-central", "cargo"} {
		if _, ok := skipped[want]; !ok {
			t.Errorf("expected %s to be skipped (flag disabled)", want)
		}
	}
}

func TestPrerequisites_FailureProducesValidationError(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)

	_, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{uniqueTagsErr: errors.New("git boom")}, //nolint:err113 // test mock error
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "tag",
		Tag:          "v1.0.0",
		Ref:          "refs/tags/v1.0.0",
		ReleaseToken: "ghp_dummy",
		Repository:   "owner/repo",
	})
	if err == nil {
		t.Fatal("expected aggregate failure")
	}

	if !strings.Contains(err.Error(), "prerequisites failed") {
		t.Errorf("expected aggregated message, got %v", err)
	}
}

func TestPrerequisites_OutputIsOrdered(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)

	var out bytes.Buffer

	_, _ = appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, &out, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "tag",
		Tag:          "v1.0.0",
		Ref:          "refs/tags/v1.0.0",
		ReleaseToken: "ghp_dummy",
		Repository:   "owner/repo",
	})

	// Even though validators ran concurrently, output is rendered in
	// the canonical order so log diffs stay deterministic.
	body := out.String()
	posRefType := strings.Index(body, "Triggered by tag")
	posTagFormat := strings.Index(body, "Validating Tag Format")

	posTagUnique := strings.Index(body, "Validating Tag Points to Unique")
	if posRefType == -1 || posTagFormat == -1 || posTagUnique == -1 {
		t.Fatalf("expected log markers in output: %q", body)
	}

	if posRefType >= posTagFormat || posTagFormat >= posTagUnique {
		t.Errorf("ordering violated: ref-type=%d tag-format=%d tag-unique=%d", posRefType, posTagFormat, posTagUnique)
	}
}

func TestPrerequisites_RequiresDeps(t *testing.T) {
	_, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{})
	if err == nil || !strings.Contains(err.Error(), "requires GitRepo") {
		t.Fatalf("err = %v", err)
	}
}
