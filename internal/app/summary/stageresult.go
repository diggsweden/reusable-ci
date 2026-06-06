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

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

const stagePlanVersion = 1

// StageResultInput drives the generic `summary stage-result` command.
//
// Results and NeedsJSON are mutually exclusive sources of per-target results:
// Results carries explicit `target=result` pairs, NeedsJSON consumes a
// `${{ toJson(needs) }}` payload and maps each target to needs[<job>].result
// by matching target name to job key (with underscores normalised to dashes).
type StageResultInput struct {
	StagePlanJSON string
	Results       []domainsummary.KeyValue
	NeedsJSON     string
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

// needsEntry is one entry in a GHA `toJson(needs)` payload. Only the result
// field is consumed here; outputs/etc. are intentionally ignored.
type needsEntry struct {
	Result string `json:"result"`
}

// StageResult composes a stage-result manifest from any typed stage plan whose
// targets object contains per-target runs flags.
//nolint:cyclop // envelope marshalling: one branch per stage-result field (provider × ecosystem × status).
func StageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in StageResultInput,
) (*domainsummary.StageResultEnvelope, error) {
	if in.StagePlanJSON == "" {
		return nil, fmt.Errorf("stage-plan-json is required: %w", errs.ErrUsage)
	}

	var plan genericStagePlanJSON
	if err := json.Unmarshal([]byte(in.StagePlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("parse stage plan: %w", err)
	}

	if err := validateGenericStagePlan(plan); err != nil {
		return nil, err
	}

	if len(in.Results) > 0 && strings.TrimSpace(in.NeedsJSON) != "" {
		return nil, fmt.Errorf("--result and --needs-json are mutually exclusive: %w", errs.ErrUsage)
	}

	resultByTarget, fromNeedsJSON, err := buildResultMap(plan, in)
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

	targetNames := make([]string, 0, len(plan.Targets))
	for name := range plan.Targets {
		targetNames = append(targetNames, name)
	}

	sort.Strings(targetNames)

	results := make([]domainsummary.Result, 0, len(targetNames))
	targets := make([]domainsummary.Target, 0, len(targetNames))
	ran := false

	for _, name := range targetNames {
		runs := targetRuns(plan.Targets[name])

		value, hasResult := resultByTarget[name]
		if runs && !hasResult {
			return nil, fmt.Errorf("stage target %q runs but no result was provided: %w", name, errs.ErrUsage)
		}

		result, err := plannedResult(value, runs)
		if err != nil {
			return nil, fmt.Errorf("stage target %q: %w", name, err)
		}

		ran = ran || runs

		results = append(results, result)
		targets = append(targets, domainsummary.Target{Name: name, Result: result})
	}

	if !fromNeedsJSON {
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

// buildResultMap resolves the target→result mapping from either explicit
// --result pairs or a `toJson(needs)` payload. The second return value
// reports whether the map came from --needs-json (which permits extra
// unmatched jobs in needs, e.g. sibling status jobs).
func buildResultMap(plan genericStagePlanJSON, in StageResultInput) (map[string]string, bool, error) {
	if strings.TrimSpace(in.NeedsJSON) == "" {
		m, err := keyValueMap("result", in.Results, nil)

		return m, false, err
	}

	var needs map[string]needsEntry
	if err := json.Unmarshal([]byte(in.NeedsJSON), &needs); err != nil {
		return nil, true, fmt.Errorf("parse needs-json: %w: %w", err, errs.ErrUsage)
	}

	m := make(map[string]string, len(plan.Targets)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for name := range plan.Targets {
		entry, ok := lookupNeeds(needs, name)
		if !ok {
			continue
		}

		m[name] = entry.Result
	}

	return m, true, nil
}

// lookupNeeds finds the needs entry matching a target name. Job keys use
// kebab-case by convention; targets that span multiple words use snake_case
// (e.g. gradle_android, github_packages), so we accept either spelling.
func lookupNeeds(needs map[string]needsEntry, target string) (needsEntry, bool) {
	if entry, ok := needs[target]; ok {
		return entry, true
	}

	dashed := strings.ReplaceAll(target, "_", "-")
	if dashed != target {
		if entry, ok := needs[dashed]; ok {
			return entry, true
		}
	}

	return needsEntry{}, false
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
