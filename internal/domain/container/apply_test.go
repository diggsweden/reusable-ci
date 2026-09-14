// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestIsCleanRefTag_RejectsRefsThatWouldBreakAStagingTag(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{"v1.2.3", true},
		{"v1.2.3-rc.1", true},
		{"2024.01", true},
		{"releases/1.2.3", false}, // '/' would break a raw-ref staging tag
		{"feature/x", false},
		{"", false},
		{".leading-dot", false}, // illegal first char
	} {
		if got := container.IsCleanRefTag(tc.ref); got != tc.want {
			t.Errorf("IsCleanRefTag(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

func TestApply_RawValue(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=raw,value=main,enable=true")

	got, ok, err := container.Apply(r, container.MetadataContext{})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "main" || got.Priority != 200 { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
	r := mustRule(t, "type=ref,event=branch") //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	got, ok, err := container.Apply(r, container.MetadataContext{
		RefName: "develop",
		RefType: provider.RefTypeBranch,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "develop" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
	// Branch names are sanitized to the docker tag grammar
	// [A-Za-z0-9_][A-Za-z0-9_.-]{0,127}.
	for _, tc := range []struct {
		ref  string
		want string
	}{
		{"feat/refactor-go", "feat-refactor-go"},             // slash -> dash
		{"user/feature@v2", "user-feature-v2"},               // run of invalid chars -> single dash
		{".hidden", "hidden"},                                // illegal leading '.'
		{"-leading", "leading"},                              // illegal leading '-'
		{"release/1.0.x", "release-1.0.x"},                   // dots kept in body
		{strings.Repeat("a", 200), strings.Repeat("a", 128)}, // capped at 128
	} {
		got, ok, err = container.Apply(r, container.MetadataContext{
			RefName: tc.ref,
			RefType: provider.RefTypeBranch,
		})
		if err != nil {
			t.Errorf("sanitize %q: %v", tc.ref, err)

			continue
		}

		if !ok || got.Tag != tc.want {
			t.Errorf("sanitize %q: got=%q ok=%v, want %q", tc.ref, got.Tag, ok, tc.want)
		}
	}
	// On a tag ref: silent skip -- no tag AND no error. Discarding the error
	// here would let "skipped because it failed" pass as "skipped by design".
	_, ok, err = container.Apply(r, container.MetadataContext{
		RefName: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefType: provider.RefTypeTag,
	})
	if ok || err != nil {
		t.Errorf("ref,event=branch on a tag ref: ok=%v err=%v, want a silent skip", ok, err)
	}
}

func TestApply_RefEventTag(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=ref,event=tag")

	got, ok, err := container.Apply(r, container.MetadataContext{
		RefName: "v1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefType: provider.RefTypeTag,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got != (container.AppliedTag{Tag: "v1.2.3", Priority: 600}) {
		t.Errorf("got=%+v ok=%v", got, ok)
	}

	got, ok, err = container.Apply(r, container.MetadataContext{
		RefName: "v1.2.3",
		RefType: provider.RefTypeBranch,
	})
	if got != (container.AppliedTag{}) || ok || err != nil {
		t.Errorf("tag rule on a branch: got=%+v ok=%v err=%v, want zero/false/nil", got, ok, err)
	}
}

func TestApply_RefEventPR(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=ref,event=pr") //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	got, ok, err := container.Apply(r, container.MetadataContext{
		PRNumber: "42",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "pr-42" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}

	_, ok, err = container.Apply(r, container.MetadataContext{})
	if ok || err != nil {
		t.Errorf("ref,event=pr with no PR number: ok=%v err=%v, want a silent skip", ok, err)
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
		{"{{patch}}", "v1.0.9", "9"},
		// no leading v — still works.
		{"{{version}}", "0.1.0", "0.1.0"},
		// literal prefix preserved, so image tags can match `uses: …@v3`.
		{"v{{version}}", "v1.2.3", "v1.2.3"},
		{"v{{major}}.{{minor}}", "v2.5.7", "v2.5"},
		{"v{{major}}", "v3.7.1", "v3"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		r := mustRule(t, "type=semver,pattern="+c.pattern)

		got, ok, err := container.Apply(r, container.MetadataContext{
			RefName: c.ref,
			RefType: provider.RefTypeTag,
		})
		if err != nil {
			t.Errorf("pattern=%q ref=%q: %v", c.pattern, c.ref, err)

			continue
		}

		if !ok || got.Tag != c.want {
			t.Errorf("pattern=%q ref=%q → got=%+v ok=%v want=%q",
				c.pattern, c.ref, got, ok, c.want)
		}
	}
}

func TestApply_SemverSilentOnNonTag(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=semver,pattern={{version}}")

	got, ok, err := container.Apply(r, container.MetadataContext{
		RefName: "v1.2.3",
		RefType: provider.RefTypeBranch,
	})
	if got != (container.AppliedTag{}) || ok || err != nil {
		t.Errorf("semver on a branch ref: got=%+v ok=%v err=%v, want zero/false/nil", got, ok, err)
	}

	got, ok, err = container.Apply(r, container.MetadataContext{
		RefName: "v1.2.3",
		RefType: provider.RefTypeTag,
	})
	if got != (container.AppliedTag{Tag: "1.2.3", Priority: 900}) || !ok || err != nil {
		t.Errorf("semver on a tag ref: got=%+v ok=%v err=%v, want 1.2.3/900, true, nil", got, ok, err)
	}
}

func TestApply_SemverUnsupportedPattern(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=semver,pattern={{unknown}}")

	_, _, err := container.Apply(r, container.MetadataContext{
		RefName: "v1.0.0",
		RefType: provider.RefTypeTag,
	})
	// A mistyped tag-rule is a bad value in the caller's configuration.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "unsupported semver pattern") {
		t.Errorf("err = %v, want it to name the pattern problem", err)
	}
}

func TestApply_SemverStrict(t *testing.T) {
	t.Parallel()

	// A valid prerelease passes through {{version}} (still a valid docker tag).
	r := mustRule(t, "type=semver,pattern={{version}}")

	got, ok, err := container.Apply(r, container.MetadataContext{
		RefName: "v1.0.0-rc.1", RefType: provider.RefTypeTag,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "1.0.0-rc.1" {
		t.Errorf("prerelease: got=%+v ok=%v", got, ok)
	}

	// Tags that aren't strict semver (missing patch, leading zeros, date-like)
	// don't match the official grammar, so semver rules skip them.
	for _, ref := range []string{"v1.2", "v01.2.3", "2024.01.15"} {
		mm := mustRule(t, "type=semver,pattern={{major}}.{{minor}}")

		_, ok, err := container.Apply(mm, container.MetadataContext{
			RefName: ref, RefType: provider.RefTypeTag,
		})
		// Skipped, not rejected: a non-semver tag is simply not this rule's
		// business, so it must not fail the whole metadata run.
		if ok || err != nil {
			t.Errorf("%q: ok=%v err=%v, want a silent skip", ref, ok, err)
		}
	}
}

func TestApply_SHAWithBranchTemplate(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=sha,prefix={{branch}}-")

	got, ok, err := container.Apply(r, container.MetadataContext{
		BranchName: "feat-x",
		ShortSHA:   "abcdef0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "feat-x-abcdef0" {
		t.Errorf("got=%+v ok=%v", got, ok)
	}
	// A slashed branch in the {{branch}} template is sanitized too.
	got, ok, err = container.Apply(r, container.MetadataContext{
		BranchName: "feat/x",
		ShortSHA:   "abcdef0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ok || got.Tag != "feat-x-abcdef0" {
		t.Errorf("slash branch: got=%+v ok=%v", got, ok)
	}
}

func TestApply_RejectsInvalidTag(t *testing.T) {
	t.Parallel()
	// A raw value the operator mistyped (slash is illegal in a tag) must fail
	// loudly here rather than surfacing as 'invalid reference format' at push.
	r := mustRule(t, "type=raw,value=bad/tag,enable=true")

	_, _, err := container.Apply(r, container.MetadataContext{})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "invalid image tag") {
		t.Errorf("err = %v, want it to name the tag problem", err)
	}
}

func TestApply_SHASkipWithoutShortSHA(t *testing.T) {
	t.Parallel()
	r := mustRule(t, "type=sha,prefix=sha-")

	_, ok, err := container.Apply(r, container.MetadataContext{})
	if ok || err != nil {
		t.Errorf("sha rule without a short SHA: ok=%v err=%v, want a silent skip", ok, err)
	}
}

// TestFromEventContext_ProjectsEveryField compares the whole projection, for
// both branch sources.
//
// Only the fallback was tested, and only BranchName was read, so a projection
// that dropped ShortSHA or PRNumber -- the inputs to the sha and pr tag rules --
// passed; those rules would then skip silently and a build would publish fewer
// tags with no error. Every field carries a distinct value so a swapped or
// wiped field shows up as a difference.
func TestFromEventContext_ProjectsEveryField(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		evt  provider.EventContext
		want container.MetadataContext
	}{
		"an explicit branch wins over the ref name": {
			evt: provider.EventContext{
				RefName: "refs-name", RefType: provider.RefTypePR, Branch: "feature/x",
				ShortSHA: "abc1234", PRNumber: "42",
			},
			want: container.MetadataContext{
				RefName: "refs-name", RefType: provider.RefTypePR, BranchName: "feature/x",
				ShortSHA: "abc1234", PRNumber: "42",
			},
		},
		"no branch falls back to the ref name": {
			evt: provider.EventContext{
				RefName: "v1.0.0", RefType: provider.RefTypeTag, ShortSHA: "def5678",
			},
			want: container.MetadataContext{
				RefName: "v1.0.0", RefType: provider.RefTypeTag, BranchName: "v1.0.0", ShortSHA: "def5678",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := container.FromEventContext(&tc.evt); got != tc.want {
				t.Errorf("FromEventContext = %+v\nwant             %+v", got, tc.want)
			}
		})
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

// TestApply_RefusesATagThatIsNotValidUTF8 is the half of the byte-preservation
// contract that makes it safe. ParseRules keeps a raw value's bytes; Apply is
// where an invalid tag must stop, so a value carrying 0xFF has to be refused
// here rather than pushed as a tag the registry will reject or mangle.
func TestApply_RefusesATagThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()

	rules, err := container.ParseRules("type=raw,value=a\xffb")
	if err != nil || len(rules) != 1 {
		t.Fatalf("parse: rules = %+v, err = %v; the parser keeps bytes and leaves validity to Apply", rules, err)
	}

	applied, ok, err := container.Apply(rules[0], container.MetadataContext{RefName: "main", RefType: provider.RefTypeBranch})
	if !errors.Is(err, errs.ErrValidation) || ok {
		t.Errorf("Apply = (%+v, %v, %v), want an ErrValidation refusal", applied, ok, err)
	}
}

// TestApply_SilentSkipsReturnTheZeroTuple pins every documented skip to the
// whole (zero, false, nil) result. A skip that returned a tag alongside
// false, or an error alongside a zero tag, is a different contract, and the
// single-field checks the skip rows above started with could not see either.
//
// The empty-RefName rows put each ref-driven rule in the context it matches,
// so the only reason left to skip is the missing name; a name that sanitizes
// to nothing is the same case reached through the sanitizer.
func TestApply_SilentSkipsReturnTheZeroTuple(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		rule string
		ctx  container.MetadataContext
	}{
		"disabled rule":                   {rule: "type=raw,value=main,enable=false"},
		"disabled rule with a bad tag":    {rule: "type=raw,value=bad/tag,enable=false"},
		"branch rule on a tag":            {rule: "type=ref,event=branch", ctx: container.MetadataContext{RefName: "v1.0.0", RefType: provider.RefTypeTag}},
		"tag rule on a branch":            {rule: "type=ref,event=tag", ctx: container.MetadataContext{RefName: "main", RefType: provider.RefTypeBranch}},
		"pr rule without a number":        {rule: "type=ref,event=pr", ctx: container.MetadataContext{RefName: "feature", RefType: provider.RefTypePR}},
		"semver rule on a non-semver tag": {rule: "type=semver,pattern={{version}}", ctx: container.MetadataContext{RefName: "2024.01.15", RefType: provider.RefTypeTag}},
		"sha rule without a short sha":    {rule: "type=sha,prefix={{branch}}-", ctx: container.MetadataContext{BranchName: "main"}},
		"branch rule with an empty name":  {rule: "type=ref,event=branch", ctx: container.MetadataContext{RefType: provider.RefTypeBranch}},
		"tag rule with an empty name":     {rule: "type=ref,event=tag", ctx: container.MetadataContext{RefType: provider.RefTypeTag}},
		"semver rule with an empty name":  {rule: "type=semver,pattern={{version}}", ctx: container.MetadataContext{RefType: provider.RefTypeTag}},
		"branch name that sanitizes away": {rule: "type=ref,event=branch", ctx: container.MetadataContext{RefName: "/.-", RefType: provider.RefTypeBranch}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok, err := container.Apply(mustRule(t, tc.rule), tc.ctx)
			if got != (container.AppliedTag{}) || ok || err != nil {
				t.Errorf("got=%+v ok=%v err=%v, want zero/false/nil", got, ok, err)
			}
		})
	}
}
