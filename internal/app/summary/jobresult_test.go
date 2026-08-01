// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
)

func TestJobResult_RecordsOutcome(t *testing.T) {
	t.Parallel()
	js := fakejobresultstore.New(t)

	err := appsummary.JobResult(context.Background(), js, appsummary.JobResultInput{
		Job:    "nanolinter",
		Status: "success",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := js.Body("nanolinter"), `{"version":1,"job":"nanolinter","result":"success"}`; got != want {
		t.Errorf("record = %s, want %s", got, want)
	}
}

func TestJobResult_FailClosedOnUnknownStatus(t *testing.T) {
	t.Parallel()
	js := fakejobresultstore.New(t)

	// An empty/unknown status must record a failure, never a skip.
	if err := appsummary.JobResult(context.Background(), js, appsummary.JobResultInput{Job: "build", Status: ""}); err != nil {
		t.Fatal(err)
	}

	if got := js.Body("build"); !strings.Contains(got, `"result":"failure"`) {
		t.Errorf("record = %s, want failure (fail-closed)", got)
	}
}

func TestJobResult_RequiresName(t *testing.T) {
	t.Parallel()
	js := fakejobresultstore.New(t)

	err := appsummary.JobResult(context.Background(), js, appsummary.JobResultInput{Job: "  ", Status: "success"})
	if err == nil || !strings.Contains(err.Error(), "--name is required") {
		t.Fatalf("err = %v", err)
	}
}
