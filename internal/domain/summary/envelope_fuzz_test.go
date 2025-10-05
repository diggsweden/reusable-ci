// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// The result envelopes decide whether a pipeline reports success, so their
// parsers must fail closed on anything malformed. Invariants, for every input:
//
//   - never panics, and every refusal is classified as malformed input
//   - an accepted envelope carries only known results and a non-empty name
//   - an accepted envelope survives a canonical marshal and re-parse unchanged
//
// The seeds are the valid shapes plus the malformed families these parsers
// have refused before: duplicate members, wrong shapes, unknown versions and
// results, control characters and trailing data. Each seed states whether it
// is accepted, and that is checked before fuzzing, so the committed corpus
// also pins that no malformed seed reads as a success.

type envelopeSeed struct {
	input    string
	accepted bool
}

func addSeeds[R any](f *testing.F, parse func(string) (R, error), seeds []envelopeSeed) {
	f.Helper()

	for _, seed := range seeds {
		if _, err := parse(seed.input); (err == nil) != seed.accepted {
			f.Fatalf("seed %q: err = %v, want accepted %t", seed.input, err, seed.accepted)
		}

		f.Add(seed.input)
	}
}

func FuzzParseStageResultEnvelope(f *testing.F) {
	addSeeds(f, summary.ParseStageResultEnvelope, []envelopeSeed{
		{"", true},
		{`{"version":1,"stage":"build","result":"success","ran":true,"targets":{"mac":"success","linux":"success"}}`, true},
		{`{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"linux":"failure"}}`, true},
		{`{"version":1,"stage":"build","result":"skipped","ran":false,"targets":{}}`, true},
		{`{"version":1,"stage":"build","result":"failure","result":"success","ran":true,"targets":{"linux":"success"}}`, false},
		{`{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":"failure","a":"success"}}`, false},
		{`{"version":2,"stage":"build","result":"success","ran":true,"targets":{"linux":"success"}}`, false},
		{"{\"version\":1,\"stage\":\"build\x01\",\"result\":\"passed\",\"ran\":true,\"targets\":[]}", false},
		{`{"version":1,"stage":"build","result":"success","ran":true,"targets":{}} {}`, false},
		{`[]`, false},
	})

	f.Fuzz(func(t *testing.T, input string) {
		envelope, err := summary.ParseStageResultEnvelope(input)
		if err != nil {
			requireMalformed(t, err)

			return
		}

		if strings.TrimSpace(input) == "" {
			return
		}

		if envelope.Stage == "" || !summary.IsResult(envelope.Result) {
			t.Fatalf("accepted %q as stage %q with result %q", input, envelope.Stage, envelope.Result)
		}

		for _, target := range envelope.Targets {
			if !summary.IsResult(target.Result) {
				t.Fatalf("accepted %q with target %q result %q", input, target.Name, target.Result)
			}
		}

		if !slices.IsSortedFunc(envelope.Targets, func(a, b summary.Target) int { return strings.Compare(a.Name, b.Name) }) {
			t.Fatalf("targets of %q are not sorted by name: %v", input, envelope.Targets)
		}

		requireRoundTrip(t, envelope, summary.ParseStageResultEnvelope)
	})
}

func FuzzParseJobResultEnvelope(f *testing.F) {
	addSeeds(f, func(input string) (summary.JobResultEnvelope, error) {
		return summary.ParseJobResultEnvelope([]byte(input))
	}, []envelopeSeed{
		{`{"version":1,"job":"build","result":"success"}`, true},
		{`{"version":1,"job":"build","result":"cancelled"}`, true},
		{`{"version":1,"job":"build","job":"other","result":"success"}`, false},
		{`{"version":2,"job":"build","result":"success"}`, false},
		{`{"version":1,"job":" ","result":"success"}`, false},
		{`{"version":1,"job":"build","result":"failed"}`, false},
		{`{"version":1,"job":"build","result":"success","extra":true}`, true},
		{`{"version":1,"job":"build","result":"failure","Result":"success"}`, false},
		{`null`, false},
	})

	f.Fuzz(func(t *testing.T, input string) {
		envelope, err := summary.ParseJobResultEnvelope([]byte(input))
		if err != nil {
			requireMalformed(t, err)

			return
		}

		if strings.TrimSpace(envelope.Job) == "" || !summary.IsResult(envelope.Result) {
			t.Fatalf("accepted %q as job %q with result %q", input, envelope.Job, envelope.Result)
		}

		requireRoundTrip(t, envelope, func(value string) (summary.JobResultEnvelope, error) {
			return summary.ParseJobResultEnvelope([]byte(value))
		})
	})
}

func FuzzParseJobResultsMap(f *testing.F) {
	addSeeds(f, func(input string) ([]summary.JobResultEnvelope, error) {
		return summary.ParseJobResultsMap([]byte(input))
	}, []envelopeSeed{
		{`{}`, true},
		{`{"build":{"result":"success"},"test":{"result":"failure"}}`, true},
		{`{"build":{"result":"success"},"build":{"result":"failure"}}`, true},
		{`{"build":{"result":"neutral"}}`, true},
		{`{"build":{"result":"success","result":"failure"}}`, false},
		{`{"":{"result":"success"}}`, false},
		{`{"build":{"result":"success"}} []`, false},
		{`{"build":"success"}`, false},
	})

	f.Fuzz(func(t *testing.T, input string) {
		records, err := summary.ParseJobResultsMap([]byte(input))
		if err != nil {
			requireMalformed(t, err)

			return
		}

		for _, record := range records {
			if strings.TrimSpace(record.Job) == "" || !summary.IsResult(record.Result) {
				t.Fatalf("accepted %q with job %q result %q", input, record.Job, record.Result)
			}
		}
	})
}

func requireMalformed(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("refusal is not classified as malformed input: %v", err)
	}
}

func requireRoundTrip[E json.Marshaler](t *testing.T, envelope E, parse func(string) (E, error)) {
	t.Helper()

	body, err := envelope.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal %+v: %v", envelope, err)
	}

	again, err := parse(string(body))
	if err != nil || !reflect.DeepEqual(again, envelope) {
		t.Fatalf("round trip of %+v through %s = %+v, %v", envelope, body, again, err)
	}
}
