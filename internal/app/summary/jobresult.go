// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// JobResultInput drives `reusable-ci report job-result`.
type JobResultInput struct {
	// Job is the recording job's name; it must match the stage-plan target
	// name the summary job aggregates against.
	Job string
	// Status is the runner's own job status (GitHub job.status /
	// GitLab CI_JOB_STATUS). Normalised fail-closed: an unknown value
	// records a failure, never a skip.
	Status string
}

// JobResult records one job's outcome to the JobResultStore so a downstream
// summary job can aggregate the stage result from it — the forge-neutral
// replacement for GitHub's toJson(needs).
func JobResult(ctx context.Context, store ci.JobResultStore, in JobResultInput) error {
	job := strings.TrimSpace(in.Job)
	if job == "" {
		return fmt.Errorf("--name is required: %w", errs.ErrUsage)
	}

	env := domainsummary.JobResultEnvelope{
		Job:    job,
		Result: domainsummary.NormalizeJobStatus(strings.TrimSpace(in.Status)),
	}

	if err := store.WriteJob(ctx, job, env); err != nil {
		return fmt.Errorf("record job result: %w", err)
	}

	return nil
}
