// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestApply_RawValue(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=raw,value=main,enable=true")
	got, ok, err := container.Apply(r, container.MetadataContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Tag != "main" || got.Priority != 200 {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
}

func TestApply_DisabledRuleSkipped(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=raw,value=main,enable=false")
	_, ok, err := container.Apply(r, container.MetadataContext{})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("disabled rule should not fire")
	}
}

func TestApply_RefEventBranch(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=ref,event=branch")
	got, ok, _ := container.Apply(r, container.MetadataContext{
		RefName: "develop",
		RefType: provider.RefTypeBranch,
	})
	if !ok || got.Tag != "develop" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
	// On a tag ref: silent skip.
	_, ok, _ = container.Apply(r, container.MetadataContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
	})
	if ok {
		t.Error("ref,event=branch should skip on tag ref")
	}
}

func TestApply_RefEventTag(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=ref,event=tag")
	got, ok, _ := container.Apply(r, container.MetadataContext{
		RefName: "v1.2.3",
		RefType: provider.RefTypeTag,
	})
	if !ok || got.Tag != "v1.2.3" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
}

func TestApply_RefEventPR(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=ref,event=pr")
	got, ok, _ := container.Apply(r, container.MetadataContext{
		PRNumber: "42",
	})
	if !ok || got.Tag != "pr-42" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
	_, ok, _ = container.Apply(r, container.MetadataContext{})
	if ok {
		t.Error("ref,event=pr should skip when PRNumber empty")
	}
}

func TestApply_SemverPatterns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		ref     string
		want    string
	}{
		{"{{version}}", "v1.2.3", "1.2.3"},
		{"{{major}}.{{minor}}", "v2.5.7", "2.5"},
		{"{{major}}", "v3.7.1", "3"},
		// no leading v — still works.
		{"{{version}}", "0.1.0", "0.1.0"},
	}
	for _, c := range cases {
		r := mustRule(t, "type=semver,pattern="+c.pattern)
		got, ok, _ := container.Apply(r, container.MetadataContext{
			RefName: c.ref,
			RefType: provider.RefTypeTag,
		})
		if !ok || got.Tag != c.want {
			t.Errorf("pattern=%q ref=%q → got=%+v ok=%v want=%q",
				c.pattern, c.ref, got, ok, c.want)
		}
	}
}

func TestApply_SemverSilentOnNonTag(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=semver,pattern={{version}}")
	_, ok, _ := container.Apply(r, container.MetadataContext{
		RefName: "main",
		RefType: provider.RefTypeBranch,
	})
	if ok {
		t.Error("semver should be silent on non-tag refs")
	}
}

func TestApply_SemverUnsupportedPattern(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=semver,pattern={{patch}}")
	_, _, err := container.Apply(r, container.MetadataContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported semver pattern") {
		t.Errorf("error = %v", err)
	}
}

func TestApply_SHAWithBranchTemplate(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=sha,prefix={{branch}}-")
	got, ok, _ := container.Apply(r, container.MetadataContext{
		BranchName: "feat-x",
		ShortSHA:   "abcdef0",
	})
	if !ok || got.Tag != "feat-x-abcdef0" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
}

func TestApply_SHASkipWithoutShortSHA(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=sha,prefix=sha-")
	_, ok, _ := container.Apply(r, container.MetadataContext{})
	if ok {
		t.Error("sha rule should skip without ShortSHA")
	}
}

func TestFromEventContext_BranchFallsBackToRefName(t *testing.T) {
	t.Parallel()
	evt := &provider.EventContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
	}
	mc := container.FromEventContext(evt)
	if mc.BranchName != "v1.0.0" {
		t.Errorf("BranchName fallback = %q, want v1.0.0", mc.BranchName)
	}
}

func mustRule(t *testing.T, line string) container.Rule {
	t.Helper()
	rs, err := container.ParseRules(line)
	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}
	return rs[0]
}
