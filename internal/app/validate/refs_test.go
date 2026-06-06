// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestRefType_TagSucceedsWithMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := appvalidate.RefType(&buf, appvalidate.RefTypeInput{
		RefType: provider.RefTypeTag, RefName: "v1.0.0", Ref: "refs/tags/v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Triggered by tag: v1.0.0") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRefType_BranchFailsWithGuidance(t *testing.T) {
	t.Parallel()

	err := appvalidate.RefType(&bytes.Buffer{}, appvalidate.RefTypeInput{
		RefType: provider.RefTypeBranch, RefName: "main", Ref: "refs/heads/main",
	})
	if err == nil {
		t.Fatal("expected failure")
	}

	for _, want := range []string{
		"release workflow must be triggered by pushing a tag",
		"Current trigger: branch",
		"git tag -s v1.0.0",
		"git push origin v1.0.0",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\nfull: %s", want, err.Error())
		}
	}
}

func TestRefType_EmptyTypeUsage(t *testing.T) {
	t.Parallel()

	err := appvalidate.RefType(&bytes.Buffer{}, appvalidate.RefTypeInput{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v", err)
	}
}

func TestTagFormat_StableReleaseOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Valid semantic version tag",
		"Version: 1.0.0",
		"Stable release",
		"validation passed",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in output:\n%s", want, buf.String())
		}
	}
}

func TestTagFormat_StandardPrerelease(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0-rc.2"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Pre-release: rc.2") {
		t.Errorf("output = %s", buf.String())
	}

	if !strings.Contains(buf.String(), "follows convention") {
		t.Errorf("output should flag standard prerelease, got:\n%s", buf.String())
	}
}

func TestTagFormat_NonStandardPrerelease(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0-custom.1"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Non-standard pre-release identifier") {
		t.Errorf("expected non-standard warning, got:\n%s", buf.String())
	}

	if !strings.Contains(buf.String(), "informational only") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestTagFormat_BadTagShowsHelp(t *testing.T) {
	t.Parallel()

	err := appvalidate.TagFormat(&bytes.Buffer{}, appvalidate.TagFormatInput{Tag: "1.0.0"})
	if err == nil {
		t.Fatal("expected failure")
	}

	for _, want := range []string{"invalid tag format", "vMAJOR.MINOR.PATCH", "semver.org", "v1.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err missing %q: %s", want, err.Error())
		}
	}
}

func TestChangelog_RequiredMissing(t *testing.T) {
	t.Parallel()
	path := testfs.NewReal(t).Path("CHANGELOG.md")
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     path,
		Required: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestChangelog_RequiredPresent(t *testing.T) {
	t.Parallel()
	path := testfs.NewReal(t).WriteFile("CHANGELOG.md", []byte("# Changelog\n## v1.0.0\n- thing\n"))

	var buf bytes.Buffer

	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, &buf, appvalidate.ChangelogInput{
		Path:     path,
		Required: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Full changelog found (3 lines)") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestChangelog_MinimalAbsentEmitsSentinel(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     testfs.NewReal(t).Path("missing.txt"),
		Required: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("content"); got != "No changes for this release" {
		t.Errorf("content = %q", got)
	}
}

func TestChangelog_MinimalPresentEmitsContent(t *testing.T) {
	t.Parallel()

	body := "Line one\nLine two\n"
	path := testfs.NewReal(t).WriteFile("minimal.txt", []byte(body))
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     path,
		Required: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.Multiline("content")
	if len(got) != 2 || got[0] != "Line one" || got[1] != "Line two" {
		t.Errorf("content lines = %v", got)
	}
}
