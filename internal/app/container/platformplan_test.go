// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestPlatformPlan_EmitsMatrixAndSuffix(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	res, err := appcontainer.PlatformPlan(context.Background(), sink, &out, appcontainer.PlatformPlanInput{
		Platforms: "linux/amd64, linux/arm64",
		Platform:  "linux/arm64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("platforms-json"); got != `["linux/amd64","linux/arm64"]` {
		t.Errorf("platforms-json = %q", got)
	}

	if got := sink.Single("suffix"); got != "arm64" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("suffix = %q", got)
	}

	if len(res.Platforms) != 2 || res.Suffix != "arm64" {
		t.Errorf("output = %+v", res)
	}
}

func TestPlatformPlan_RejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.PlatformPlan(context.Background(), fakeoutputsink.New(t), nil, appcontainer.PlatformPlanInput{
		Platforms: "linux/amd64, linux/arm/v7",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported container platform") {
		t.Fatalf("err = %v", err)
	}

	_, err = appcontainer.PlatformPlan(context.Background(), fakeoutputsink.New(t), nil, appcontainer.PlatformPlanInput{
		Platform: "windows/amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported container platform") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlatformPlan_DefaultsEmptyPlatforms(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	out, err := appcontainer.PlatformPlan(context.Background(), sink, nil, appcontainer.PlatformPlanInput{})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("platforms-json"); got != `["linux/amd64"]` {
		t.Errorf("platforms-json = %q", got)
	}

	if len(out.Platforms) != 1 || out.Platforms[0] != "linux/amd64" {
		t.Fatalf("platforms = %v", out.Platforms)
	}
}

func TestPlatformPlan_RejectsCommaOnlyPlatforms(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.PlatformPlan(context.Background(), fakeoutputsink.New(t), nil, appcontainer.PlatformPlanInput{Platforms: ", ,"})
	if err == nil || !strings.Contains(err.Error(), "platforms is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlatformSuffix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"linux/amd64", "amd64"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"linux/arm64", "arm64"},
		{"darwin/arm64", "darwin-arm64"},
		{"", ""},
	} {
		if got := appcontainer.PlatformSuffix(tc.in); got != tc.want {
			t.Errorf("PlatformSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
