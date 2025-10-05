// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const warningConfig = `
artifacts:
  - name: app
    project-type: maven
    build-type: application
    publish-to: [forge-packages]
`

var (
	errOutputRefused  = errors.New("output refused")
	errSummaryRefused = errors.New("summary refused")
)

type refusingOutputSink struct{ *fakeoutputsink.Sink }

func (refusingOutputSink) Set(context.Context, string, string) error { return errOutputRefused }

type recordingSummary struct {
	appended []string
	err      error
}

func (s *recordingSummary) Append(_ context.Context, markdown string) error {
	s.appended = append(s.appended, markdown)

	return s.err
}

// TestValidate_WarningsAreBestEffort: a config with a warning validates with
// a nil warning writer (it used to panic writing to nil) and with a writer
// that fails, and a working writer receives exactly the warning line.
func TestValidate_WarningsAreBestEffort(t *testing.T) {
	t.Parallel()

	path := testfs.NewReal(t).WriteFile("artifacts.yml", []byte(warningConfig))

	require.NoError(t, appconfig.Validate(path, nil))
	require.NoError(t, appconfig.Validate(path, failingExpansionWriter{}))

	var warnings bytes.Buffer
	require.NoError(t, appconfig.Validate(path, &warnings))
	require.Equal(t, "warning: Maven application \"app\" publishing to forge-packages — applications should not (libraries only)\n", warnings.String())
}

// TestEmitConfigPlan_OutputAndSummaryFailuresAreDistinct: a refused output
// stops before the summary and returns that refusal; a refused summary is
// returned after the plan output was written, and is not reported as the
// output failing. The success control appends exactly one summary.
func TestEmitConfigPlan_OutputAndSummaryFailuresAreDistinct(t *testing.T) {
	t.Parallel()

	path := writeYAML(t, "artifacts:\n  - name: app\n    project-type: maven\n")
	in := appconfig.EmitConfigPlanInput{Path: path}

	summary := &recordingSummary{}
	err := appconfig.EmitConfigPlan(t.Context(), refusingOutputSink{fakeoutputsink.New(t)}, summary, io.Discard, output.Annotator{}, in)
	require.ErrorIs(t, err, errOutputRefused)
	require.Empty(t, summary.appended)

	sink := fakeoutputsink.New(t)
	summary = &recordingSummary{err: errSummaryRefused}
	err = appconfig.EmitConfigPlan(t.Context(), sink, summary, io.Discard, output.Annotator{}, in)
	require.ErrorIs(t, err, errSummaryRefused)
	require.NotErrorIs(t, err, errOutputRefused)
	require.Equal(t, []string{"config-plan-json"}, sink.Keys())
	require.Len(t, summary.appended, 1)

	sink = fakeoutputsink.New(t)
	summary = &recordingSummary{}
	require.NoError(t, appconfig.EmitConfigPlan(t.Context(), sink, summary, io.Discard, output.Annotator{}, in))
	require.Equal(t, []string{"config-plan-json"}, sink.Keys())
	require.Len(t, summary.appended, 1)
	require.Contains(t, summary.appended[0], "app")
}
