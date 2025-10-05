// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

func TestSARIFToGitLabSAST_InvalidCVSSFallsBackToLevel(t *testing.T) {
	t.Parallel()

	for _, score := range []any{"NaN", "+Inf", "-Inf", "-1", "10.1", math.NaN(), math.Inf(1), math.Inf(-1), float64(-1), 10.1, json.Number("-1")} {
		result := map[string]any{"ruleId": "rule", "level": "error", "properties": map[string]any{"security-severity": score}}
		doc := map[string]any{"runs": []any{map[string]any{"results": []any{result}}}}

		report := security.SARIFToGitLabSAST(doc, security.Options{Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if len(report.Vulnerabilities) != 1 || report.Vulnerabilities[0].Severity != security.SeverityHigh {
			t.Errorf("invalid score %v did not fall back to error level", score)
		}
	}
}

func TestSARIFIdentities_KeepDelimiterBearingFieldsDistinct(t *testing.T) {
	t.Parallel()

	body := []byte(`{"runs":[{"results":[
 {"ruleId":"RULE|file","message":{"text":"message"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"name.go"},"region":{"startLine":7}}}]},
 {"ruleId":"RULE","message":{"text":"message"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"file|name.go"},"region":{"startLine":7}}}]}
 ]}]}`)

	out, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Runs []struct {
			Results []struct {
				PartialFingerprints map[string]string `json:"partialFingerprints"`
			} `json:"results"`
		} `json:"runs"`
	}
	if decodeErr := json.Unmarshal(out, &got); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	results := got.Runs[0].Results

	first, second := results[0].PartialFingerprints["primaryLocationLineHash"], results[1].PartialFingerprints["primaryLocationLineHash"]
	if first == "" || second == "" || first == second {
		t.Fatal("distinct tuples received the same or empty fallback identity")
	}

	again, err := security.EnrichGitHubSARIF(out)
	if err != nil || !bytes.Equal(out, again) {
		t.Fatalf("repeat enrichment changed existing identities: %v", err)
	}

	report := security.SARIFToGitLabSAST(parseSARIF(t, string(body)), security.Options{})
	if len(report.Vulnerabilities) != 2 || report.Vulnerabilities[0].ID == report.Vulnerabilities[1].ID {
		t.Fatal("GitLab identities collided for the same tuple boundary case")
	}
}

func TestSARIFRewriters_PreserveNumbersDuringActiveChanges(t *testing.T) {
	t.Parallel()

	for name, rewrite := range map[string]func([]byte) ([]byte, error){
		"enrichment": security.EnrichGitHubSARIF,
		"category":   func(body []byte) ([]byte, error) { return security.SetSARIFCategory(body, "new-category") },
	} {
		for _, number := range []string{"9007199254740993", "9223372036854775807", "0.12345678901234567890123456789", "1e400"} {
			t.Run(name+"/"+number, func(t *testing.T) {
				t.Parallel()

				body := []byte(`{"marker":` + number + `,"extra":{"flags":[true,null,"keep"]},"runs":[{"automationDetails":{"description":{"text":"keep"}},"tool":{"driver":{"name":"fixture"}},"results":[{"ruleId":"rule","message":{"text":"keep"},"partialFingerprints":{"other":"keep"},"properties":{"score":` + number + `},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"file.go"},"region":{"startLine":42,"endLine":44}}}]}]}]}`)

				out, err := rewrite(body)
				if err != nil {
					t.Fatal(err)
				}

				var exact struct {
					Marker json.RawMessage `json:"marker"`
				}
				if err := json.Unmarshal(out, &exact); err != nil {
					t.Fatal(err)
				}

				if string(exact.Marker) != number {
					t.Errorf("numeric spelling changed: %s", exact.Marker)
				}

				var expected string
				if name == "enrichment" {
					expected = strings.Replace(string(body), `"other":"keep"`, `"other":"keep","primaryLocationLineHash":"[\"rule\",\"file.go\",\"42\",\"keep\"]"`, 1)
				} else {
					expected = strings.Replace(string(body), `"automationDetails":{`, `"automationDetails":{"id":"new-category",`, 1)
				}

				if bytes.Equal(body, []byte(expected)) {
					t.Fatal("expected update did not alter the fixture")
				}

				if !reflect.DeepEqual(decodeNumberPreservingObject(t, []byte(expected)), decodeNumberPreservingObject(t, out)) {
					t.Error("active rewrite did not preserve the complete expected document")
				}
			})
		}
	}
}

func decodeNumberPreservingObject(t *testing.T, body []byte) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}

	return doc
}

func TestSARIFRewriters_RejectTrailingJSON(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"runs":[]} {}`, `{"runs":[]} trailing`} {
		if _, err := security.EnrichGitHubSARIF([]byte(body)); err == nil {
			t.Error("enrichment accepted trailing JSON")
		}

		if _, err := security.SetSARIFCategory([]byte(body), "category"); err == nil {
			t.Error("category accepted trailing JSON")
		}
	}
}

func TestSARIFToGitLabSAST_AcceptsNumberPreservingDecode(t *testing.T) {
	t.Parallel()

	for _, numbers := range [][2]string{{"12", "14"}, {"12.0", "14.0"}, {"1.2e1", "1.4e1"}} {
		t.Run(numbers[0], func(t *testing.T) {
			t.Parallel()

			body := strings.NewReplacer(`"8.5"`, `9.4`, `"startLine": 12`, `"startLine": `+numbers[0], `"endLine": 14`, `"endLine": `+numbers[1]).Replace(sampleSARIF)
			doc := decodeNumberPreservingObject(t, []byte(body))
			report := security.SARIFToGitLabSAST(doc, security.Options{})

			location := report.Vulnerabilities[0].Location
			if location.StartLine != 12 || location.EndLine != 14 {
				t.Errorf("location lost: %+v", location)
			}

			if report.Vulnerabilities[0].Severity != security.SeverityCritical {
				t.Error("CVSS score lost")
			}
		})
	}
}
