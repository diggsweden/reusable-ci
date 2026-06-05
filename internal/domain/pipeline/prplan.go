// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package pipeline

import (
	"cmp"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// PRPlanVersion is the current pull-request plan contract version.
const PRPlanVersion = 1

// PRPlanInput contains workflow inputs for PR quality planning.
type PRPlanInput struct {
	ProjectType                projecttype.Type
	BaseBranch                 string
	ReusableCIBinaryRef        string
	SASTOpengrepRules          string
	SASTOpengrepFailOnSeverity string
	DependencyReview           bool
	SASTOpengrep               bool
	PublicCodeLint             bool
	DevbaseCheck               bool
	Nanolinter                 bool
	SwiftFormat                bool
	SwiftLint                  bool
}

// PRPlan is the top-level typed contract for PR quality workflows.
type PRPlan struct {
	Version int          `json:"version"`
	Context PRContext    `json:"context"`
	Policy  PRPolicy     `json:"policy"`
	Stages  PRStagePlans `json:"stages"`
}

// PRContext is workflow context consumed by PR quality jobs.
type PRContext struct {
	ProjectType                projecttype.Type `json:"project_type"`
	BaseBranch                 string           `json:"base_branch"`
	ReusableCIBinaryRef        string           `json:"reusable_ci_binary_ref"`
	SASTOpengrepRules          string           `json:"sast_opengrep_rules"`
	SASTOpengrepFailOnSeverity string           `json:"sast_opengrep_fail_on_severity"`
}

// PRPolicy is the pull-request quality gate policy.
type PRPolicy struct {
	DependencyReview bool `json:"dependency_review"`
	SASTOpengrep     bool `json:"sast_opengrep"`
	PublicCodeLint   bool `json:"public_code_lint"`
	DevbaseCheck     bool `json:"devbase_check"`
	Nanolinter       bool `json:"nanolinter"`
	SwiftFormat      bool `json:"swift_format"`
	SwiftLint        bool `json:"swift_lint"`
	Swift            bool `json:"swift"`
}

// PRStagePlans contains PR stage-specific plans.
type PRStagePlans struct {
	Quality PRQualityStagePlan `json:"quality"`
}

// PRQualityStagePlan describes PR quality-stage targets.
type PRQualityStagePlan struct {
	Version int              `json:"version"`
	Stage   string           `json:"stage"`
	Targets PRQualityTargets `json:"targets"`
}

// PRQualityTargets are the PR quality jobs.
type PRQualityTargets struct {
	DependencyReview TargetPlan[string] `json:"dependency_review"`
	SASTOpengrep     TargetPlan[string] `json:"sast_opengrep"`
	PublicCodeLint   TargetPlan[string] `json:"public_code_lint"`
	DevbaseCheck     TargetPlan[string] `json:"devbase_check"`
	Nanolinter       TargetPlan[string] `json:"nanolinter"`
	Swift            TargetPlan[string] `json:"swift"`
}

// NewPRPlan builds the PR plan contract from workflow inputs.
func NewPRPlan(in PRPlanInput) PRPlan {
	policy := PRPolicy{
		DependencyReview: in.DependencyReview,
		SASTOpengrep:     in.SASTOpengrep,
		PublicCodeLint:   in.PublicCodeLint,
		DevbaseCheck:     in.DevbaseCheck,
		Nanolinter:       in.Nanolinter,
		SwiftFormat:      in.SwiftFormat,
		SwiftLint:        in.SwiftLint,
		Swift:            in.SwiftFormat || in.SwiftLint,
	}
	quality := PRQualityStagePlan{
		Version: PRPlanVersion,
		Stage:   "pr-quality",
		Targets: PRQualityTargets{
			DependencyReview: singletonTargetPlan("dependency-review", policy.DependencyReview),
			SASTOpengrep:     singletonTargetPlan("sast-opengrep", policy.SASTOpengrep),
			PublicCodeLint:   singletonTargetPlan("publiccode-lint", policy.PublicCodeLint),
			DevbaseCheck:     singletonTargetPlan("devbase-check", policy.DevbaseCheck),
			Nanolinter:       singletonTargetPlan("nanolinter", policy.Nanolinter),
			Swift:            singletonTargetPlan("swift", policy.Swift),
		},
	}

	return PRPlan{
		Version: PRPlanVersion,
		Context: PRContext{
			ProjectType:                in.ProjectType,
			BaseBranch:                 in.BaseBranch,
			ReusableCIBinaryRef:        in.ReusableCIBinaryRef,
			SASTOpengrepRules:          cmp.Or(in.SASTOpengrepRules, "p/default"),
			SASTOpengrepFailOnSeverity: cmp.Or(in.SASTOpengrepFailOnSeverity, "high"),
		},
		Policy: policy,
		Stages: PRStagePlans{Quality: quality},
	}
}
