// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
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

func TestGenerateDevVersion_HappyPath(t *testing.T) {
	ops := &fakeGit{
		tags:        []string{"v0.4.0", "v0.5.9", "v0.5.0"},
		shortSHAOut: "abc1234",
	}
	var stdout bytes.Buffer
	if err := appversion.GenerateDevVersion(context.Background(), ops, &stdout, appversion.GenerateDevVersionInput{
		RefName: "feat/awesome",
	}); err != nil {
		t.Fatalf("GenerateDevVersion: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if got != "0.5.9-dev-feat-awesome-abc1234" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateDevVersion_NoTagsFallsBackToZero(t *testing.T) {
	ops := &fakeGit{
		shortSHAOut: "deadbee",
	}
	var stdout bytes.Buffer
	if err := appversion.GenerateDevVersion(context.Background(), ops, &stdout, appversion.GenerateDevVersionInput{
		RefName: "main",
	}); err != nil {
		t.Fatalf("GenerateDevVersion: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if got != "0.0.0-dev-main-deadbee" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateDevVersion_FetchFailureIgnored(t *testing.T) {
	ops := &fakeGit{
		runErr:      errors.New("offline"),
		tags:        []string{"v1.0.0"},
		shortSHAOut: "1234567",
	}
	var stdout bytes.Buffer
	if err := appversion.GenerateDevVersion(context.Background(), ops, &stdout, appversion.GenerateDevVersionInput{
		RefName: "main",
	}); err != nil {
		t.Fatalf("expected fetch failure to be ignored: %v", err)
	}
	if !strings.Contains(stdout.String(), "1.0.0-dev-main-1234567") {
		t.Errorf("output = %q", stdout.String())
	}
}

func TestGenerateDevVersion_ShortSHAErrorBubbles(t *testing.T) {
	ops := &fakeGit{
		tags:        []string{"v1.0.0"},
		shortSHAErr: errors.New("not a git repo"),
	}
	if err := appversion.GenerateDevVersion(context.Background(), ops, &bytes.Buffer{}, appversion.GenerateDevVersionInput{
		RefName: "main",
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateDevVersion_RequiresRefName(t *testing.T) {
	if err := appversion.GenerateDevVersion(context.Background(), &fakeGit{}, &bytes.Buffer{}, appversion.GenerateDevVersionInput{}); err == nil {
		t.Fatal("expected ref-name error")
	}
}

func TestGenerateDevVersion_JSONFormat_EmitsObject(t *testing.T) {
	t.Parallel()
	ops := &fakeGit{
		tags:        []string{"v1.2.3"},
		shortSHAOut: "abc1234",
	}
	var stdout bytes.Buffer
	err := appversion.GenerateDevVersion(context.Background(), ops, &stdout, appversion.GenerateDevVersionInput{
		RefName: "main",
		Format:  output.FormatJSON,
	})
	require.NoError(t, err)

	var decoded struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	require.Equal(t, "1.2.3-dev-main-abc1234", decoded.Version)
}

func TestGenerateDevVersion_TextFormat_PreservesBareLine(t *testing.T) {
	t.Parallel()
	ops := &fakeGit{tags: []string{"v0.1.0"}, shortSHAOut: "deadbee"}
	var stdout bytes.Buffer
	err := appversion.GenerateDevVersion(context.Background(), ops, &stdout, appversion.GenerateDevVersionInput{
		RefName: "main",
		Format:  output.FormatText,
	})
	require.NoError(t, err)
	require.Equal(t, "0.1.0-dev-main-deadbee\n", stdout.String())
}
