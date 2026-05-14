// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package plan

import (
	"encoding/json"
)

// ReleasePolicyEnvelope is the JSON shape consumed by downstream stages
// as the `release-policy-json` output of `plan write-release-interface`.
//
// Field names use snake_case to match the existing bash output (verified
// against tests/plan/write-release-interface.bats).
type ReleasePolicyEnvelope struct {
	SignArtifacts      bool   `json:"sign_artifacts"`
	CheckAuthorization bool   `json:"check_authorization"`
	RunVersionBump     bool   `json:"run_version_bump"`
	CreateRelease      bool   `json:"create_release"`
	CreateDraftRelease bool   `json:"create_draft_release"`
	SBOMs              string `json:"sboms"`
	MakeLatest         bool   `json:"make_latest"`
	HasContainers      bool   `json:"has_containers"`
}

// MarshalReleasePolicy returns compact JSON.
func MarshalReleasePolicy(env ReleasePolicyEnvelope) ([]byte, error) {
	return json.Marshal(env)
}

// DevContext is the per-pipeline context the dev-release stage consumes.
// The fields mirror scripts/plan/write-dev-release-interface.sh exactly,
// in the same emission order.
type DevContext struct {
	ProjectType       string `json:"project_type"`
	Branch            string `json:"branch"`
	ReleaseSHA        string `json:"release_sha"`
	ReleaseActor      string `json:"release_actor"`
	ReleaseRepository string `json:"release_repository"`
	WorkingDirectory  string `json:"working_directory"`
	JavaVersion       string `json:"java_version"`
	NodeVersion       string `json:"node_version"`
	RustToolchain     string `json:"rust_toolchain"`
	Registry          string `json:"registry"`
	ScriptsRef        string `json:"scripts_ref"`
	NPMRegistry       string `json:"npm_registry"`
	PackageScope      string `json:"package_scope"`
}

// DevPolicy is the per-pipeline policy the dev-release stage consumes.
type DevPolicy struct {
	PublishNPM bool `json:"publish_npm"`
	UseCIToken bool `json:"use_ci_token"`
}

// MarshalDevContext / MarshalDevPolicy return compact JSON.
func MarshalDevContext(c DevContext) ([]byte, error) { return json.Marshal(c) }
func MarshalDevPolicy(p DevPolicy) ([]byte, error)   { return json.Marshal(p) }

// PRContext is the per-pipeline context the PR stage consumes.
type PRContext struct {
	ProjectType                string `json:"project_type"`
	BaseBranch                 string `json:"base_branch"`
	ScriptsRef                 string `json:"scripts_ref"`
	SASTOpengrepRules          string `json:"sast_opengrep_rules"`
	SASTOpengrepFailOnSeverity string `json:"sast_opengrep_fail_on_severity"`
}

// PRPolicy is the per-pipeline linter / SAST gating policy. The "swift"
// field is the disjunction of swiftformat/swiftlint, kept on the policy
// because PR jobs gate the toolchain install on that single boolean.
type PRPolicy struct {
	DependencyReview bool `json:"dependencyreview"`
	SASTOpengrep     bool `json:"sastopengrep"`
	PublicCodeLint   bool `json:"publiccodelint"`
	DevbaseCheck     bool `json:"devbasecheck"`
	SwiftFormat      bool `json:"swiftformat"`
	SwiftLint        bool `json:"swiftlint"`
	Swift            bool `json:"swift"` // = SwiftFormat || SwiftLint
}

// BuildPRPolicy applies the swift = swiftformat || swiftlint rule.
func BuildPRPolicy(in PRPolicy) PRPolicy {
	in.Swift = in.SwiftFormat || in.SwiftLint
	return in
}

func MarshalPRContext(c PRContext) ([]byte, error) { return json.Marshal(c) }
func MarshalPRPolicy(p PRPolicy) ([]byte, error)   { return json.Marshal(p) }
