// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"bytes"
	"encoding/json"
	"sort"
)

// AggregateResults reduces a list of normalised results to one with the
// priority: failure > cancelled > success. Empty input → success
// (matches the bash fall-through).
//
// Mirrors ci_aggregate_results in scripts/ci/stage-result.sh.
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
//
// Mirrors ci_stage_result in scripts/ci/stage-result.sh.
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
//	{"stage":"…","result":"…","ran":<bool>,
//	 [extra-key]:"<extra-value>",…,
//	 "targets":{"k":"v",…}}
//
// Targets serialise in declaration order (Go map ordering would be
// non-deterministic; bats checks substring presence but the ordering
// matters for byte-equal comparisons in the manifest file too).
func (e StageResultEnvelope) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	if err := writeKVRaw(&buf, "stage", quote(e.Stage)); err != nil {
		return nil, err
	}
	buf.WriteByte(',')
	if err := writeKVRaw(&buf, "result", quote(string(e.Result))); err != nil {
		return nil, err
	}
	buf.WriteByte(',')
	if e.Ran {
		writeKV(&buf, "ran", "true")
	} else {
		writeKV(&buf, "ran", "false")
	}
	for _, kv := range e.Extras {
		buf.WriteByte(',')
		if err := writeKVRaw(&buf, kv.Key, quote(kv.Value)); err != nil {
			return nil, err
		}
	}
	buf.WriteByte(',')
	if err := writeTargetsRaw(&buf, e.Targets); err != nil {
		return nil, err
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeKV(buf *bytes.Buffer, k, vRaw string) {
	buf.WriteByte('"')
	buf.WriteString(k)
	buf.WriteByte('"')
	buf.WriteByte(':')
	buf.WriteString(vRaw)
}

func writeKVRaw(buf *bytes.Buffer, k, vRaw string) error {
	writeKV(buf, k, vRaw)
	return nil
}

func writeTargetsRaw(buf *bytes.Buffer, targets []Target) error {
	buf.WriteString(`"targets":{`)
	for i, t := range targets {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(quote(t.Name))
		buf.WriteByte(':')
		buf.WriteString(quote(string(t.Result)))
	}
	buf.WriteByte('}')
	return nil
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// SortTargetsByName returns a copy with Targets sorted by Name. Used
// by tests where the bash's declaration order isn't observable.
func SortTargetsByName(in []Target) []Target {
	out := make([]Target, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
