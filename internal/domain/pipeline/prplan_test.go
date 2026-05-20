// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestNewPRPlan_BuildsQualityStagePlan(t *testing.T) {
	t.Parallel()

	plan := pipeline.NewPRPlan(pipeline.PRPlanInput{
		ProjectType: projecttype.Go,
		Engine:      pipeline.LintEngineNanolinter,
		SwiftFormat: true,
	})
	if plan.Version != pipeline.PRPlanVersion || plan.Stages.Quality.Stage != "pr-quality" {
		t.Errorf("plan = %+v", plan)
	}

	if !plan.Stages.Quality.Targets.Nanolinter.Runs {
		t.Errorf("quality targets = %+v", plan.Stages.Quality.Targets)
	}

	if !plan.Policy.Swift || !plan.Stages.Quality.Targets.Swift.Runs {
		t.Errorf("swift policy/target = %+v %+v", plan.Policy, plan.Stages.Quality.Targets.Swift)
	}
}
