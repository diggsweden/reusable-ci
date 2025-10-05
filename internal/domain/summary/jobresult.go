// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	if err := checkConsumedMembers(data, "job-result", "version", "job", "result"); err != nil {
		return JobResultEnvelope{}, fmt.Errorf("parse job-result JSON: %w", err)
	}

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
//
//nolint:cyclop // Keep outer duplicate-job preservation and each child's member/typed checks together.
func ParseJobResultsMap(data []byte) ([]JobResultEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))

	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("parse job-results map: not a valid {job:{result}} object: %w", errs.ErrMalformedInput)
	}

	var records []JobResultEnvelope
	// Keep duplicate job names as separate records so aggregation sees every
	// outcome instead of encoding/json's last-key-wins map behavior.
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read job name: %w: %w", err, errs.ErrMalformedInput)
		}

		name, ok := key.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("job name is empty or invalid: %w", errs.ErrMalformedInput)
		}

		var body json.RawMessage
		if err := decoder.Decode(&body); err != nil {
			return nil, fmt.Errorf("read job result: %w: %w", err, errs.ErrMalformedInput)
		}

		if err := checkConsumedMembers(body, "job-result", "result"); err != nil {
			return nil, fmt.Errorf("read job result for job %q: %w", name, err)
		}

		var record struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal(body, &record); err != nil {
			return nil, fmt.Errorf("read job result: %w: %w", err, errs.ErrMalformedInput)
		}

		records = append(records, JobResultEnvelope{Version: JobResultEnvelopeVersion, Job: name, Result: NormalizeJobStatus(record.Result)})
	}

	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close job-results map: %w: %w", err, errs.ErrMalformedInput)
	}

	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("trailing job-results data: %w", errs.ErrMalformedInput)
	}

	sort.SliceStable(records, func(i, j int) bool { return records[i].Job < records[j].Job })

	return records, nil
}

// checkConsumedMembers counts only immediate consumed fields, which encoding/json
// matches case-insensitively and would otherwise let a later duplicate
// overwrite. Typed decoding still checks shape and syntax; RawMessage leaves
// additive values opaque, including nested duplicate keys and numbers outside
// float64's range. label names the contract in errors.
func checkConsumedMembers(data []byte, label string, fields ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	start, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("not a valid %s object: %w", label, errs.ErrMalformedInput)
	}

	if start != json.Delim('{') {
		// In particular, retain the map child's existing null normalization.
		return nil
	}

	seen := make([]bool, len(fields))

	for decoder.More() {
		key, err := decoder.Token()

		name, ok := key.(string)
		if err != nil || !ok {
			return fmt.Errorf("invalid %s member: %w", label, errs.ErrMalformedInput)
		}

		for index, field := range fields {
			if !strings.EqualFold(name, field) {
				continue
			}

			if seen[index] {
				return fmt.Errorf("duplicate field %q: %w", field, errs.ErrMalformedInput)
			}

			seen[index] = true
		}

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("invalid %s member value: %w", label, errs.ErrMalformedInput)
		}
	}

	return nil
}

// ResolveTargetResults maps each planned target name to the result reported by
// a matching job record. Matching is by name with snake_case/kebab-case
// equivalence (a `gradle_android` target matches a `gradle-android` job),
// mirroring the stage-plan vs job-name spelling conventions.
//
// Targets with no matching record are omitted from the map; the caller applies
// the fail-closed default (a planned target that never reported is a failure).
// Alias collisions aggregate conservatively; input order and exact-name
// preference must never hide a failed job behind another spelling's success.
func ResolveTargetResults(targetNames []string, jobs []JobResultEnvelope) map[string]Result {
	byJob := make(map[string]Result, len(jobs))
	for _, job := range jobs {
		name := strings.ReplaceAll(job.Job, "_", "-")
		if prior, exists := byJob[name]; exists {
			if prior != job.Result {
				byJob[name] = ResultFailure
			}
		} else {
			byJob[name] = job.Result
		}
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
	result, ok := byJob[strings.ReplaceAll(target, "_", "-")]

	return result, ok
}
