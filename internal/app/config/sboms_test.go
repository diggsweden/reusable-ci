// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	appconfig "github.com/diggsweden/reusable-ci/internal/app/config"
)

func TestExpandSBOMs_OutputFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input appconfig.ExpandSBOMsInput
		want  string
	}{
		{name: "json_all", input: appconfig.ExpandSBOMsInput{Value: "all"}, want: `["build","analyzed-artifact","analyzed-container"]` + "\n"},
		{name: "json_none", input: appconfig.ExpandSBOMsInput{Value: "none"}, want: "[]\n"},
		{name: "comma_all", input: appconfig.ExpandSBOMsInput{Value: "all", Format: appconfig.ExpandSBOMsFormatComma}, want: "build,analyzed-artifact,analyzed-container\n"},
		{name: "comma_none", input: appconfig.ExpandSBOMsInput{Value: "none", Format: appconfig.ExpandSBOMsFormatComma}, want: "\n"},
		{name: "comma_exclude", input: appconfig.ExpandSBOMsInput{Value: "all", Format: appconfig.ExpandSBOMsFormatComma, Exclude: []string{"analyzed-container"}}, want: "build,analyzed-artifact\n"},
		{name: "json_exclude_stack_and_noop", input: appconfig.ExpandSBOMsInput{Value: "all", Exclude: []string{"analyzed-artifact", "analyzed-container", "missing"}}, want: "[\"build\"]\n"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := appconfig.ExpandSBOMs(&buf, testCase.input); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != testCase.want {
				t.Errorf("got  %q\nwant %q", got, testCase.want)
			}
		})
	}
}

func TestExpandSBOMs_RejectsBadValue(t *testing.T) {
	t.Parallel()
	err := appconfig.ExpandSBOMs(&bytes.Buffer{}, appconfig.ExpandSBOMsInput{
		Value: "banana",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown token") {
		t.Errorf("err = %v", err)
	}
}

func TestExpandSBOMs_EmptyValueErrors(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "   "} {
		t.Run("value_"+strconv.Quote(value), func(t *testing.T) {
			t.Parallel()
			err := appconfig.ExpandSBOMs(&bytes.Buffer{}, appconfig.ExpandSBOMsInput{Value: value})
			if err == nil || !strings.Contains(err.Error(), "value required") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestExpandSBOMs_InvalidFormatErrors(t *testing.T) {
	t.Parallel()
	err := appconfig.ExpandSBOMs(&bytes.Buffer{}, appconfig.ExpandSBOMsInput{
		Value:  "all",
		Format: "xml",
	})
	if err == nil || !strings.Contains(err.Error(), "--format must be json or comma") {
		t.Errorf("err = %v", err)
	}
}
