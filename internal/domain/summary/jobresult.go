// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// JobResultEnvelopeVersion is the current job-result JSON contract version.
const JobResultEnvelopeVersion = 1

// JobResultEnvelope is the typed shape of jobs/<job>.json — one CI job's own
// recorded outcome. A job writes this as its last (always-run) step; a
// downstream summary job collects every record to aggregate a stage result,
// the forge-neutral replacement for GitHub's `toJson(needs)`.
type JobResultEnvelope struct {
	Version int
	Job     string
	Result  Result
}

// MarshalJSON emits the canonical shape:
//
//	{"version":1,"job":"…","result":"…"}
func (e JobResultEnvelope) MarshalJSON() ([]byte, error) {
	version := e.Version
	if version == 0 {
		version = JobResultEnvelopeVersion
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	writeKV(&buf, "version", strconv.Itoa(version))
	buf.WriteByte(',')
	writeKV(&buf, "job", quote(e.Job))
	buf.WriteByte(',')
	writeKV(&buf, "result", quote(string(e.Result)))
	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// ParseJobResultEnvelope parses and validates one job-result document.
func ParseJobResultEnvelope(data []byte) (JobResultEnvelope, error) {
	var raw struct {
		Version int    `json:"version"`
		Job     string `json:"job"`
		Result  Result `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return JobResultEnvelope{}, fmt.Errorf(
			"parse job-result JSON: not a valid job-result object (want {version, job, result}): %w",
			errs.ErrMalformedInput)
	}

	if raw.Version != JobResultEnvelopeVersion {
		return JobResultEnvelope{}, fmt.Errorf("job-result JSON has unsupported version %d: %w", raw.Version, errs.ErrMalformedInput)
	}

	if strings.TrimSpace(raw.Job) == "" {
		return JobResultEnvelope{}, fmt.Errorf("job-result JSON missing job: %w", errs.ErrMalformedInput)
	}

	if !IsResult(raw.Result) {
		return JobResultEnvelope{}, fmt.Errorf("job-result JSON has invalid result %q: %w", raw.Result, errs.ErrMalformedInput)
	}

	return JobResultEnvelope{Version: raw.Version, Job: raw.Job, Result: raw.Result}, nil
}

// ParseJobResultsMap decodes a job-results map — a generic
// {"<job>":{"result":"<status>"},…} object — into job records, the second
// input adapter (alongside collected manifests) that feeds ResolveTargetResults.
//
// On GitHub/Forgejo the summary job feeds this from `toJson(needs)`, the only
// way to read sibling `uses:` job results; the shape is not vendor-specific, so
// it is named for what it is. Each status is normalised fail-closed
// (NormalizeJobStatus), and records sort by name for deterministic output.
func ParseJobResultsMap(data []byte) ([]JobResultEnvelope, error) {
	var raw map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf(
			"parse job-results map: not a valid {job:{result}} object: %w", errs.ErrMalformedInput)
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	out := make([]JobResultEnvelope, 0, len(names))
	for _, name := range names {
		out = append(out, JobResultEnvelope{
			Version: JobResultEnvelopeVersion,
			Job:     name,
			Result:  NormalizeJobStatus(raw[name].Result),
		})
	}

	return out, nil
}

// ResolveTargetResults maps each planned target name to the result reported by
// a matching job record. Matching is by name with snake_case/kebab-case
// equivalence (a `gradle_android` target matches a `gradle-android` job),
// mirroring the stage-plan vs job-name spelling conventions.
//
// Targets with no matching record are omitted from the map; the caller applies
// the fail-closed default (a planned target that never reported is a failure).
// When two records would map to the same target, the last one wins — callers
// collect records from a flat directory where each job writes once, so
// collisions only arise from malformed input.
func ResolveTargetResults(targetNames []string, jobs []JobResultEnvelope) map[string]Result {
	byJob := make(map[string]Result, len(jobs))
	for _, job := range jobs {
		byJob[job.Job] = job.Result
	}

	out := make(map[string]Result, len(targetNames))

	for _, name := range targetNames {
		if result, ok := matchJobResult(byJob, name); ok {
			out[name] = result
		}
	}

	return out
}

// matchJobResult finds the record for a target, accepting the snake_case form
// recorded as kebab-case (and vice versa).
func matchJobResult(byJob map[string]Result, target string) (Result, bool) {
	if result, ok := byJob[target]; ok {
		return result, true
	}

	if dashed := strings.ReplaceAll(target, "_", "-"); dashed != target {
		if result, ok := byJob[dashed]; ok {
			return result, true
		}
	}

	return "", false
}
