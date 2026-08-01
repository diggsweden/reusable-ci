// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package summary orchestrates `reusable-ci summary ...` subcommands.
// The generic stage-result writer composes a typed StageResultEnvelope from a
// stage plan and explicit target results, then dual-writes scalar outputs and a
// JSON manifest.
package summary

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

const stagePlanVersion = 1

// StageResultInput drives the generic `summary stage-result` command.
//
// Per-target results come from one of three sources, in priority order:
//   - Results set → explicit `target=result` pairs (the caller knows the
//     outcomes; every planned target must be supplied, else it's an error).
//   - JobResultsMap set → a {job:{result}} map decoded into job records. The
//     forge-native feed on GitHub/Forgejo, where the summary job supplies
//     `toJson(needs)` (the only way to read sibling `uses:` job results).
//   - otherwise → the per-job records collected from the JobResultStore. The
//     forge-native feed on GitLab, where each job self-records because no job
//     can read a sibling's status.
//
// The last two are record sources: both decode into job records and flow
// through the same domainsummary.ResolveTargetResults aggregator, and a planned
// target with no matching record is fail-closed to failure (the job crashed or
// was cancelled before recording its outcome).
type StageResultInput struct {
	StagePlanJSON string
	Results       []domainsummary.KeyValue
	JobResultsMap string
	Extras        []domainsummary.KeyValue
	JSONOutputKey string
	JSONFields    []domainsummary.KeyValue
}

type genericStagePlanJSON struct {
	Version int                        `json:"version"`
	Stage   string                     `json:"stage"`
	Targets map[string]stagePlanTarget `json:"targets"`
}

type stagePlanTarget struct {
	Runs *bool `json:"runs"`
}

// StageResult composes a stage-result manifest from any typed stage plan whose
// targets object contains per-target runs flags.
//
//nolint:cyclop // envelope marshalling: one branch per stage-result field (provider × ecosystem × status).
func StageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	jobs ci.JobResultStore,
	in StageResultInput,
) (*domainsummary.StageResultEnvelope, error) {
	if in.StagePlanJSON == "" {
		return nil, fmt.Errorf("stage-plan-json is required: %w", errs.ErrUsage)
	}

	var plan genericStagePlanJSON
	if err := json.Unmarshal([]byte(in.StagePlanJSON), &plan); err != nil {
		// Classify like the field checks below (EX_CONFIG), not the
		// unclassified EX_SOFTWARE default — a malformed stage plan is
		// caller-supplied data, not an internal bug.
		return nil, fmt.Errorf("parse stage plan: not valid JSON: %w", errs.ErrInvalidConfig)
	}

	if err := validateGenericStagePlan(plan); err != nil {
		return nil, err
	}

	targetNames := make([]string, 0, len(plan.Targets))
	for name := range plan.Targets {
		targetNames = append(targetNames, name)
	}

	sort.Strings(targetNames)

	resultByTarget, fromRecords, err := buildResultMap(ctx, jobs, targetNames, in)
	if err != nil {
		return nil, err
	}

	if _, err := keyValueMap("extra", in.Extras, reservedStageResultKeys()); err != nil {
		return nil, err
	}

	if in.JSONOutputKey != "" {
		if _, err := keyValueMap("json-field", in.JSONFields, nil); err != nil {
			return nil, err
		}
	}

	results := make([]domainsummary.Result, 0, len(targetNames))
	targets := make([]domainsummary.Target, 0, len(targetNames))
	ran := false

	for _, name := range targetNames {
		runs := targetRuns(plan.Targets[name])

		value, hasResult := resultByTarget[name]
		if runs && !hasResult {
			if !fromRecords {
				return nil, fmt.Errorf("stage target %q runs but no result was provided: %w", name, errs.ErrUsage)
			}
			// Record mode: a planned target with no record crashed or was
			// cancelled before recording its outcome — fail closed so the
			// stage cannot be declared a success behind a missing job.
			value = string(domainsummary.ResultFailure)
		}

		result, err := plannedResult(value, runs)
		if err != nil {
			return nil, fmt.Errorf("stage target %q: %w", name, err)
		}

		ran = ran || runs

		results = append(results, result)
		targets = append(targets, domainsummary.Target{Name: name, Result: result})
	}

	if !fromRecords {
		for name := range resultByTarget {
			if _, ok := plan.Targets[name]; !ok {
				return nil, fmt.Errorf("result provided for unknown stage target %q: %w", name, errs.ErrUsage)
			}
		}
	}

	env := &domainsummary.StageResultEnvelope{
		Stage:   plan.Stage,
		Result:  domainsummary.StageResult(ran, results),
		Ran:     ran,
		Extras:  in.Extras,
		Targets: targets,
	}
	if err := emitStageOutputs(ctx, out, manifest, plan.Stage, env); err != nil {
		return nil, err
	}

	if in.JSONOutputKey != "" {
		if err := emitJSONOutput(ctx, out, in.JSONOutputKey, in.JSONFields); err != nil {
			return nil, err
		}
	}

	return env, nil
}

func validateGenericStagePlan(plan genericStagePlanJSON) error {
	if plan.Version != stagePlanVersion {
		return fmt.Errorf("stage plan has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	if strings.TrimSpace(plan.Stage) == "" {
		return fmt.Errorf("stage plan missing stage: %w", errs.ErrInvalidConfig)
	}

	if !isStageName(plan.Stage) {
		return fmt.Errorf("stage plan has invalid stage name %q: %w", plan.Stage, errs.ErrInvalidConfig)
	}

	if len(plan.Targets) == 0 {
		return fmt.Errorf("stage plan missing targets: %w", errs.ErrInvalidConfig)
	}

	for name, target := range plan.Targets {
		if !isStageTargetName(name) {
			return fmt.Errorf("stage plan target %q has invalid name: %w", name, errs.ErrInvalidConfig)
		}

		if target.Runs == nil {
			return fmt.Errorf("stage plan target %q missing runs: %w", name, errs.ErrInvalidConfig)
		}
	}

	return nil
}

// buildResultMap resolves the target→result mapping from explicit --result
// pairs or from job records (a --job-results map, or those collected from the
// JobResultStore). The second return value reports whether the map came from
// records, which fail-closes planned-but-missing targets rather than erroring.
func buildResultMap(
	ctx context.Context,
	jobs ci.JobResultStore,
	targetNames []string,
	in StageResultInput,
) (map[string]string, bool, error) {
	if len(in.Results) > 0 {
		m, err := keyValueMap("result", in.Results, nil)

		return m, false, err
	}

	records, err := collectRecords(ctx, jobs, in.JobResultsMap)
	if err != nil {
		return nil, true, err
	}

	resolved := domainsummary.ResolveTargetResults(targetNames, records)

	m := make(map[string]string, len(resolved)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for name, result := range resolved {
		m[name] = string(result)
	}

	return m, true, nil
}

// collectRecords gathers job records from the forge-native source: a
// --job-results map when supplied (GitHub/Forgejo toJson(needs)), otherwise the
// per-job records collected from the store (GitLab). Both decode into the same
// record type for ResolveTargetResults.
func collectRecords(ctx context.Context, jobs ci.JobResultStore, jobResultsMap string) ([]domainsummary.JobResultEnvelope, error) {
	if strings.TrimSpace(jobResultsMap) != "" {
		return domainsummary.ParseJobResultsMap([]byte(jobResultsMap))
	}

	docs, err := jobs.CollectJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect job results: %w", err)
	}

	records := make([]domainsummary.JobResultEnvelope, 0, len(docs))

	for _, doc := range docs {
		env, err := domainsummary.ParseJobResultEnvelope(doc)
		if err != nil {
			return nil, err
		}

		records = append(records, env)
	}

	return records, nil
}

func keyValueMap(label string, kvs []domainsummary.KeyValue, reserved map[string]struct{}) (map[string]string, error) {
	m := make(map[string]string, len(kvs)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for _, kv := range kvs {
		key := strings.TrimSpace(kv.Key)
		if !isStageFieldName(key) {
			return nil, fmt.Errorf("%s key %q has invalid name: %w", label, kv.Key, errs.ErrUsage)
		}

		if _, ok := reserved[key]; ok {
			return nil, fmt.Errorf("%s key %q is reserved: %w", label, key, errs.ErrUsage)
		}

		if _, ok := m[key]; ok {
			return nil, fmt.Errorf("duplicate %s key %q: %w", label, key, errs.ErrUsage)
		}

		m[key] = kv.Value
	}

	return m, nil
}

func emitJSONOutput(ctx context.Context, out ci.OutputSink, key string, fields []domainsummary.KeyValue) error {
	body := make(map[string]string, len(fields))
	for _, field := range fields {
		body[field.Key] = field.Value
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}

	if err := out.Set(ctx, key, string(encoded)); err != nil {
		return fmt.Errorf("set %s: %w", key, err)
	}

	return nil
}

func plannedResult(result string, runs bool) (domainsummary.Result, error) {
	if !runs {
		return domainsummary.ResultSkipped, nil
	}

	switch domainsummary.Result(result) {
	case domainsummary.ResultSuccess, domainsummary.ResultFailure, domainsummary.ResultCancelled:
		return domainsummary.Result(result), nil
	case domainsummary.ResultSkipped:
		return "", fmt.Errorf("planned target returned skipped: %w", errs.ErrUsage)
	default:
		return "", fmt.Errorf("invalid result %q: %w", result, errs.ErrUsage)
	}
}

func reservedStageResultKeys() map[string]struct{} {
	return map[string]struct{}{
		"stage":   {},
		"result":  {},
		"ran":     {},
		"targets": {},
	}
}

func isStageName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}

		if r == '-' && i > 0 && i < len(value)-1 {
			continue
		}

		return false
	}

	return true
}

func isStageTargetName(value string) bool {
	return isStageFieldName(value)
}

func isStageFieldName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}

		return false
	}

	return true
}

func targetRuns(target stagePlanTarget) bool {
	return target.Runs != nil && *target.Runs
}

func emitStageOutputs(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	stageName string,
	env *domainsummary.StageResultEnvelope,
) error {
	body, err := env.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal %s envelope: %w", stageName, err)
	}

	if err := manifest.WriteJSON(ctx, stageName, env); err != nil {
		return fmt.Errorf("write %s manifest: %w", stageName, err)
	}

	if err := out.SetBool(ctx, "stage-ran", env.Ran); err != nil {
		return err
	}

	if err := out.Set(ctx, "stage-result", string(env.Result)); err != nil {
		return err
	}

	return out.Set(ctx, "result-json", string(body))
}
