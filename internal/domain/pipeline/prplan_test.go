// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package pipeline_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestNewPRPlan_BuildsQualityStagePlan(t *testing.T) {
	t.Parallel()

	plan := pipeline.NewPRPlan(pipeline.PRPlanInput{
		ProjectType:      projecttype.Go,
		DependencyReview: true,
		SASTOpengrep:     true,
		DevbaseCheck:     true,
		SwiftFormat:      true,
	})
	if plan.Version != pipeline.PRPlanVersion || plan.Stages.Quality.Stage != "pr-quality" {
		t.Errorf("plan = %+v", plan)
	}

	if !plan.Stages.Quality.Targets.DependencyReview.Runs || !plan.Stages.Quality.Targets.SASTOpengrep.Runs {
		t.Errorf("quality targets = %+v", plan.Stages.Quality.Targets)
	}

	if !plan.Policy.Swift || !plan.Stages.Quality.Targets.Swift.Runs {
		t.Errorf("swift policy/target = %+v %+v", plan.Policy, plan.Stages.Quality.Targets.Swift)
	}

	if plan.Context.SASTOpengrepRules != "p/default" || plan.Context.SASTOpengrepFailOnSeverity != "high" {
		t.Errorf("context defaults = %+v", plan.Context)
	}
}
