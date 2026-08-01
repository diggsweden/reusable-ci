// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeGit struct {
	runErr      error
	tags        []string
	tagsErr     error
	shortSHAOut string
	shortSHAErr error

	// captures
	runArgs    [][]string
	listTagsAt []string
	shortRefs  []string
}

func (f *fakeGit) Run(_ context.Context, args ...string) (string, error) {
	f.runArgs = append(f.runArgs, args)

	return "", f.runErr
}

func (f *fakeGit) ListTags(_ context.Context, pattern string) ([]string, error) {
	f.listTagsAt = append(f.listTagsAt, pattern)

	return f.tags, f.tagsErr
}

func (f *fakeGit) ShortSHA(_ context.Context, ref string, _ int) (string, error) {
	f.shortRefs = append(f.shortRefs, ref)

	return f.shortSHAOut, f.shortSHAErr
}

func TestGenerateSnapshotVersion_HappyPath(t *testing.T) {
	ops := &fakeGit{
		tags:        []string{"v0.4.0", "v0.5.9", "v0.5.0"},
		shortSHAOut: "abc1234", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "feat/awesome",
	}); err != nil {
		t.Fatalf("GenerateSnapshotVersion: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if got != "0.5.9-snapshot-feat-awesome-abc1234" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSnapshotVersion_NoTagsFallsBackToZero(t *testing.T) {
	ops := &fakeGit{
		shortSHAOut: "deadbee",
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatalf("GenerateSnapshotVersion: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if got != "0.0.0-snapshot-main-deadbee" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSnapshotVersion_FetchFailureIgnored(t *testing.T) {
	ops := &fakeGit{
		runErr:      errors.New("offline"), //nolint:err113 // test mock error
		tags:        []string{"v1.0.0"},
		shortSHAOut: "1234567",
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
	}); err != nil {
		t.Fatalf("expected fetch failure to be ignored: %v", err)
	}

	if !strings.Contains(out.String(), "1.0.0-snapshot-main-1234567") {
		t.Errorf("output = %q", out.String())
	}
}

func TestGenerateSnapshotVersion_ShortSHAErrorBubbles(t *testing.T) {
	ops := &fakeGit{
		tags:        []string{"v1.0.0"},
		shortSHAErr: errors.New("not a git repo"), //nolint:err113 // test mock error
	}
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &bytes.Buffer{}, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateSnapshotVersion_RequiresRefName(t *testing.T) {
	if err := appversion.GenerateSnapshotVersion(context.Background(), &fakeGit{}, &bytes.Buffer{}, appversion.GenerateSnapshotVersionInput{}); err == nil {
		t.Fatal("expected ref-name error")
	}
}

func TestGenerateSnapshotVersion_JSONFormat_EmitsObject(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{
		tags:        []string{"v1.2.3"},
		shortSHAOut: "abc1234",
	}

	var out bytes.Buffer

	err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
		Format:  output.FormatJSON,
	})
	require.NoError(t, err)

	var decoded struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Equal(t, "1.2.3-snapshot-main-abc1234", decoded.Version)
}

func TestGenerateSnapshotVersion_TextFormat_PreservesBareLine(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{tags: []string{"v0.1.0"}, shortSHAOut: "deadbee"}

	var out bytes.Buffer

	err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
		Format:  output.FormatText,
	})
	require.NoError(t, err)
	require.Equal(t, "0.1.0-snapshot-main-deadbee\n", out.String())
}

func TestGenerateSnapshotVersion_GitHubFormatEmitsOutput(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{tags: []string{"v0.2.0"}, shortSHAOut: "abc1234"}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "feature/demo",
		Format:  output.FormatGitHub,
		Sink:    sink,
	})
	require.NoError(t, err)
	require.Equal(t, "0.2.0-snapshot-feature-demo-abc1234", sink.Single("snapshot-version"))
	require.Equal(t, "0.2.0-snapshot-feature-demo-abc1234\n", out.String())
}
