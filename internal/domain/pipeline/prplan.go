// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// PRPlanVersion is the current pull-request plan contract version.
const PRPlanVersion = 1

// PRPlanInput contains workflow inputs for PR quality planning.
type PRPlanInput struct {
	ProjectType         projecttype.Type
	BaseBranch          string
	ReusableCIBinaryRef string
	Nanolinter          bool
	SwiftFormat         bool
	SwiftLint           bool
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
	ProjectType         projecttype.Type `json:"project_type"`
	BaseBranch          string           `json:"base_branch"`
	ReusableCIBinaryRef string           `json:"reusable_ci_binary_ref"`
}

// PRPolicy is the pull-request quality gate policy.
type PRPolicy struct {
	Nanolinter  bool `json:"nanolinter"`
	SwiftFormat bool `json:"swift_format"`
	SwiftLint   bool `json:"swift_lint"`
	Swift       bool `json:"swift"`
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
	Nanolinter TargetPlan[string] `json:"nanolinter"`
	Swift      TargetPlan[string] `json:"swift"`
}

// NewPRPlan builds the PR plan contract from workflow inputs.
func NewPRPlan(in PRPlanInput) PRPlan {
	policy := PRPolicy{
		Nanolinter:  in.Nanolinter,
		SwiftFormat: in.SwiftFormat,
		SwiftLint:   in.SwiftLint,
		Swift:       in.SwiftFormat || in.SwiftLint,
	}
	quality := PRQualityStagePlan{
		Version: PRPlanVersion,
		Stage:   "pr-quality",
		Targets: PRQualityTargets{
			Nanolinter: singletonTargetPlan("nanolinter", policy.Nanolinter),
			Swift:      singletonTargetPlan("swift", policy.Swift),
		},
	}

	return PRPlan{
		Version: PRPlanVersion,
		Context: PRContext{
			ProjectType:         in.ProjectType,
			BaseBranch:          in.BaseBranch,
			ReusableCIBinaryRef: in.ReusableCIBinaryRef,
		},
		Policy: policy,
		Stages: PRStagePlans{Quality: quality},
	}
}
