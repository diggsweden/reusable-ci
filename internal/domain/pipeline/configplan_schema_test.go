// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/stretchr/testify/require"
)

// validConfigPlanJSON is the smallest plan the boundary accepts, so each case
// below changes exactly one thing and the refusal is attributable to it.
const validConfigPlanJSON = `{
  "version": 1,
  "artifacts": {"all": []},
  "containers": {"has_containers": false},
  "pipeline_sboms": "none",
  "sign": {"method": "gpg", "imports_gpg_key": true},
  "git_signing": {"method": "gpg", "imports_gpg_key": true}
}`

// TestConfigPlanSchema_NullAndWrongTypedMembers covers the shapes JSON lets
// through that a Go struct quietly absorbs.
//
// encoding/json accepts null for any member and leaves the zero value behind, so
// a plan that omitted a field and a plan that explicitly nulled it arrive
// identical. That is fine where the zero value is a real answer and wrong where
// it is not: a nulled version is not version 0, it is a producer that failed to
// state one. Wrong-typed members are the other half — they must fail as parse
// errors rather than being coerced.
func TestConfigPlanSchema_NullAndWrongTypedMembers(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		member  string
		value   string
		refused bool
	}{
		{name: "nulled version is not version zero", member: "version", value: "null", refused: true},
		{name: "wrong-typed version", member: "version", value: `"1"`, refused: true},
		{name: "unsupported version", member: "version", value: "2", refused: true},
		{name: "nulled artifacts is an empty set", member: "artifacts", value: "null"},
		{name: "wrong-typed artifacts", member: "artifacts", value: `"all"`, refused: true},
		{name: "nulled containers is no containers", member: "containers", value: "null"},
		{name: "wrong-typed containers", member: "containers", value: "[]", refused: true},
		// An unstated SBOM policy is not "none": the producer has to say which
		// layers the pipeline carries, and the expander refuses an empty value.
		{name: "nulled pipeline_sboms states nothing", member: "pipeline_sboms", value: "null", refused: true},
		{name: "wrong-typed pipeline_sboms", member: "pipeline_sboms", value: "5", refused: true},
		{name: "nulled sign block", member: "sign", value: "null", refused: true},
		{name: "wrong-typed sign block", member: "sign", value: `"gpg"`, refused: true},
		{name: "nulled git_signing block", member: "git_signing", value: "null", refused: true},
		{name: "nulled any_require_authorization is false", member: "any_require_authorization", value: "null"},
		{name: "wrong-typed any_require_authorization", member: "any_require_authorization", value: `"yes"`, refused: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := withConfigPlanMember(t, testCase.member, testCase.value)

			plan, err := pipeline.DecodeConfigPlan(raw)
			if err == nil {
				err = pipeline.ValidateConfigPlan(plan)
			}

			if !testCase.refused {
				require.NoError(t, err, raw)

				return
			}

			require.ErrorIs(t, err, errs.ErrInvalidConfig, raw)
		})
	}
}

// withConfigPlanMember replaces one member of the valid plan, or adds it when
// the baseline leaves it out, so each case differs from the baseline by exactly
// that member.
func withConfigPlanMember(t *testing.T, member, value string) string {
	t.Helper()

	marker := `"` + member + `":`
	body := strings.ReplaceAll(validConfigPlanJSON, `"`+member+`": `, marker)

	start := strings.Index(body, marker)
	if start < 0 {
		return strings.Replace(body, "{\n", "{\n  "+marker+value+",\n", 1)
	}

	rest := body[start+len(marker):]
	end := len(rest)
	depth := 0

	for index, char := range rest {
		switch char {
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				end = index

				goto done
			}

			depth--
		case ',':
			if depth == 0 {
				end = index

				goto done
			}
		}
	}

done:

	return body[:start+len(marker)] + value + rest[end:]
}
