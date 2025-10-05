// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"sort"
	"strconv"
	"strings"
)

// StageResultEnvelopeVersion is the current stage-result JSON contract version.
const StageResultEnvelopeVersion = 1

// AggregateResults reduces a list of normalised results to one with the
// priority: failure > cancelled > success. Empty input → success
// .
func AggregateResults(results []Result) Result {
	for _, r := range results {
		if r == ResultFailure {
			return ResultFailure
		}
	}

	for _, r := range results {
		if r == ResultCancelled {
			return ResultCancelled
		}
	}

	return ResultSuccess
}

// StageResult resolves the manifest's `result` field. When the stage
// didn't run, it's always "skipped" regardless of any per-target values.
func StageResult(ran bool, results []Result) Result {
	if !ran {
		return ResultSkipped
	}

	return AggregateResults(results)
}

// Target is one (name, result) pair carried inside the stage-result
// manifest's `targets` object.
type Target struct {
	Name   string
	Result Result
}

// StageResultEnvelope is the typed shape of <stage>-result.json.
//
// Stage / Result / Ran are always present; Targets and Extras are
// rendered as separate JSON keys (Extras follows Result/Ran but
// precedes Targets, matching the bash's emission order).
type StageResultEnvelope struct {
	Version int
	Stage   string
	Result  Result
	Ran     bool
	Extras  []KeyValue // ordered extras, e.g. project_type
	Targets []Target
}

// KeyValue is a string-keyed string-valued ordered pair.
type KeyValue struct{ Key, Value string }

// MarshalJSON emits the canonical envelope shape:
//
//	{"version":1,"stage":"…","result":"…","ran":<bool>,
//	 [extra-key]:"<extra-value>",…,
//	 "targets":{"k":"v",…}}
//
// Targets serialise in declaration order (Go map ordering would be
// non-deterministic; bats checks substring presence but the ordering
// matters for byte-equal comparisons in the manifest file too).
func (e StageResultEnvelope) MarshalJSON() ([]byte, error) {
	version := e.Version
	if version == 0 {
		version = StageResultEnvelopeVersion
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	writeKV(&buf, "version", strconv.Itoa(version))
	buf.WriteByte(',')
	writeKV(&buf, "stage", quote(e.Stage))
	buf.WriteByte(',')
	writeKV(&buf, "result", quote(string(e.Result)))
	buf.WriteByte(',')

	if e.Ran {
		writeKV(&buf, "ran", "true")
	} else {
		writeKV(&buf, "ran", "false")
	}

	for _, kv := range e.Extras {
		buf.WriteByte(',')
		writeKV(&buf, kv.Key, quote(kv.Value))
	}

	buf.WriteByte(',')
	writeTargets(&buf, e.Targets)
	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// ParseStageResultEnvelope parses and validates a non-empty stage-result JSON
// contract. Empty input is treated as an absent/skipped whole stage so summary
// commands can still render workflows that were intentionally skipped.
func ParseStageResultEnvelope(value string) (StageResultEnvelope, error) {
	if strings.TrimSpace(value) == "" {
		return StageResultEnvelope{Version: StageResultEnvelopeVersion, Result: ResultSkipped}, nil
	}

	// A duplicate member or target name would let a later value replace an
	// earlier one, so {"a":"failure","a":"success"} read as a clean success.
	if err := checkConsumedMembers([]byte(value), "stage-result", "version", "stage", "result", "ran", "targets"); err != nil {
		return StageResultEnvelope{}, fmt.Errorf("parse stage-result JSON: %w", err)
	}

	var raw struct {
		Version int             `json:"version"`
		Stage   string          `json:"stage"`
		Result  Result          `json:"result"`
		Ran     bool            `json:"ran"`
		Targets json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		// Classify as malformed input (EX_DATAERR) like the field checks
		// below, not the unclassified EX_SOFTWARE default — a bad envelope
		// is the caller's data, not our bug. The shape hint replaces Go's
		// "cannot unmarshal X into struct{…}" dump, which leaks internals.
		return StageResultEnvelope{}, fmt.Errorf(
			"parse stage-result JSON: not a valid stage-result object (want {version, stage, result, ran, targets}): %w",
			errs.ErrMalformedInput)
	}

	if raw.Version != StageResultEnvelopeVersion {
		return StageResultEnvelope{}, fmt.Errorf("stage-result JSON has unsupported version %d: %w", raw.Version, errs.ErrMalformedInput)
	}

	if strings.TrimSpace(raw.Stage) == "" {
		return StageResultEnvelope{}, fmt.Errorf("stage-result JSON missing stage"+": %w", errs.ErrMalformedInput)
	}

	if !IsResult(raw.Result) {
		return StageResultEnvelope{}, fmt.Errorf("stage-result JSON has invalid result %q: %w", raw.Result, errs.ErrMalformedInput)
	}

	targets, err := parseStageTargets(raw.Targets)
	if err != nil {
		return StageResultEnvelope{}, err
	}

	if err := checkStageSummary(raw.Result, raw.Ran, targets); err != nil {
		return StageResultEnvelope{}, err
	}

	return StageResultEnvelope{
		Version: raw.Version,
		Stage:   raw.Stage,
		Result:  raw.Result,
		Ran:     raw.Ran,
		Targets: targets,
	}, nil
}

// parseStageTargets validates every target name and result, and returns the
// targets sorted by name. A target named twice is refused: decoding into a map
// keeps only the last value, which could hide a failure.
func parseStageTargets(body json.RawMessage) ([]Target, error) {
	if len(body) == 0 || string(body) == "null" {
		return nil, fmt.Errorf("stage-result JSON missing targets"+": %w", errs.ErrMalformedInput)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if start, err := decoder.Token(); err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("stage-result JSON targets must be an object: %w", errs.ErrMalformedInput)
	}

	seen := map[string]bool{}
	targets := []Target{}

	for decoder.More() {
		key, err := decoder.Token()

		name, ok := key.(string)
		if err != nil || !ok {
			return nil, fmt.Errorf("stage-result JSON contains an invalid target member: %w", errs.ErrMalformedInput)
		}

		var result Result
		if err := decoder.Decode(&result); err != nil {
			return nil, fmt.Errorf("stage-result JSON target %q is not a result: %w", name, errs.ErrMalformedInput)
		}

		if err := checkStageTarget(name, result, seen); err != nil {
			return nil, err
		}

		seen[name] = true
		targets = append(targets, Target{Name: name, Result: result})
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })

	return targets, nil
}

func checkStageTarget(name string, result Result, seen map[string]bool) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("stage-result JSON contains an empty target name"+": %w", errs.ErrMalformedInput)
	case seen[name]:
		return fmt.Errorf("stage-result JSON names target %q twice: %w", name, errs.ErrMalformedInput)
	case !IsResult(result):
		return fmt.Errorf("stage-result JSON target %q has invalid result %q: %w", name, result, errs.ErrMalformedInput)
	}

	return nil
}

// checkStageSummary refuses a stage result or ran flag its targets do not
// produce. The producer derives both from the targets; an envelope that
// disagrees (success over a failed target, a stage that ran with nothing
// running) is not one it could have written.
func checkStageSummary(result Result, ran bool, targets []Target) error {
	results := make([]Result, 0, len(targets))
	targetsRan := false

	for _, target := range targets {
		results = append(results, target.Result)
		targetsRan = targetsRan || target.Result != ResultSkipped
	}

	if ran != targetsRan || result != StageResult(targetsRan, results) {
		return fmt.Errorf("stage-result JSON result %q (ran=%t) does not match its targets: %w", result, ran, errs.ErrMalformedInput)
	}

	return nil
}

// TargetResult returns the named target result, defaulting to skipped for
// unplanned/missing targets.
func (e StageResultEnvelope) TargetResult(key string) Result {
	for _, target := range e.Targets {
		if target.Name == key {
			return target.Result
		}
	}

	return ResultSkipped
}

func writeKV(buf *bytes.Buffer, k, vRaw string) {
	buf.WriteByte('"')
	buf.WriteString(k)
	buf.WriteByte('"')
	buf.WriteByte(':')
	buf.WriteString(vRaw)
}

func writeTargets(buf *bytes.Buffer, targets []Target) {
	buf.WriteString(`"targets":{`)

	for i, t := range targets { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if i > 0 {
			buf.WriteByte(',')
		}

		buf.WriteString(quote(t.Name))
		buf.WriteByte(':')
		buf.WriteString(quote(string(t.Result)))
	}

	buf.WriteByte('}')
}

func quote(s string) string {
	b, err := json.Marshal(s) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		// json.Marshal of a string never fails; defensive fallback
		// keeps the call total without poisoning the caller.
		return `""`
	}

	return string(b)
}
