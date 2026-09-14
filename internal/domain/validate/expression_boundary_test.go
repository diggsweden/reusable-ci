// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"strings"
	"testing"
)

func TestExpressionBoundary_SecretFormsAndInheritance(t *testing.T) {
	t.Parallel()

	config := IsolationConfig{BuildJob: "build", SigningSecrets: []string{"SIGNING_KEY"}}

	for _, reference := range []string{"secrets.SIGNING_KEY", "secrets['SIGNING_KEY']", `secrets["SIGNING_KEY"]`, "secrets [ 'SIGNING_KEY' ]", "format('}}{0}', secrets['SIGNING_KEY'])", "format('it''s {0}', secrets.SIGNING_KEY)"} {
		body := fmt.Sprintf("jobs:\n  build:\n    env:\n      KEY: ${{ %s }}\n    steps: []\n", reference)

		violations, err := CheckIsolation([]byte(body), config)
		if err != nil || len(violations) != 1 || violations[0].Line != 4 || !strings.Contains(violations[0].Msg, `build job "build"`) || !strings.Contains(violations[0].Msg, "SIGNING_KEY") {
			t.Fatalf("reference=%s violations=%v err=%v", reference, violations, err)
		}
	}

	for _, value := range []string{"secrets.SIGNING_KEY", "${{ secrets.SIGNING_KEY_EXTRA }}", "${{ 'secrets.SIGNING_KEY' }}", "${{ 'it''s secrets.SIGNING_KEY' }}", "${{ othersecrets.SIGNING_KEY }}", "${{ github.event . secrets.SIGNING_KEY }}"} {
		body := fmt.Sprintf("jobs:\n  build:\n    name: %q\n    steps: []\n", value)
		if violations, err := CheckIsolation([]byte(body), config); err != nil || len(violations) != 0 {
			t.Fatalf("inert/extended reference matched: %q %v %v", value, violations, err)
		}
	}

	for _, override := range []string{"", "    env:\n      KEY: safe\n", "    env:\n      KEY: ${{ secrets['SIGNING_KEY'] }}\n"} {
		body := "env:\n  KEY: ${{ secrets.SIGNING_KEY }}\njobs:\n  build:\n" + override + "    steps: []\n"
		violations, err := CheckIsolation([]byte(body), config)

		safe := strings.Contains(override, "KEY: safe")
		if err != nil || (len(violations) == 0) != safe {
			t.Fatalf("override=%q violations=%v err=%v", override, violations, err)
		}

		if override == "" && (violations[0].Line != 2 || !strings.Contains(violations[0].Msg, "inherits")) {
			t.Fatalf("inherited reference not attributed: %v", violations)
		}

		if override != "" && !safe && (len(violations) != 1 || violations[0].Line != 6 || strings.Contains(violations[0].Msg, "inherits")) {
			t.Fatalf("unsafe job override not attributed: %v", violations)
		}
	}
}

func TestExpressionBoundary_SecretScalarAttribution(t *testing.T) {
	t.Parallel()

	for _, scalar := range []string{
		"|\n          harmless\n          ${{ secrets.SIGNING_KEY }} ${{ secrets.OTHER_KEY }}\n          ${{ secrets.SIGNING_KEY }}\n",
		">-\n          harmless\n          ${{ secrets.SIGNING_KEY }} ${{ secrets.OTHER_KEY }}\n          ${{ secrets.SIGNING_KEY }}\n",
		`"harmless\n${{ secrets.SIGNING_KEY }} ${{ secrets.OTHER_KEY }}\n${{ secrets.SIGNING_KEY }}"` + "\n",
	} {
		body := "jobs:\n  neighbor:\n    name: ${{ secrets.OTHER_KEY }}\n  compile:\n    steps:\n      - run: " + scalar + "      - env:\n          KEY: ${{ secrets.OTHER_KEY }}\n"

		violations, err := CheckIsolation([]byte(body), IsolationConfig{BuildJob: "compile", SigningSecrets: []string{"SIGNING_KEY", "OTHER_KEY"}})
		if err != nil || len(violations) != 3 {
			t.Fatalf("scalar=%q violations=%v err=%v", scalar, violations, err)
		}

		for index, secret := range []string{"SIGNING_KEY", "OTHER_KEY", "OTHER_KEY"} {
			wantLine := 6
			if index == 2 {
				wantLine = 7 + strings.Count(scalar, "\n")
			}

			if violations[index].Line != wantLine || !strings.Contains(violations[index].Msg, `build job "compile"`) || !strings.Contains(violations[index].Msg, `"`+secret+`"`) {
				t.Fatalf("scalar=%q violation=%v want line=%d secret=%s", scalar, violations[index], wantLine, secret)
			}
		}
	}
}

func TestExpressionBoundary_SecretAnchorAndInheritedAttribution(t *testing.T) {
	t.Parallel()

	body := "x-secret: &key >-\n  ${{ secrets.SIGNING_KEY }}\n  ${{ secrets.OTHER_KEY }}\nenv:\n  INHERITED: *key\njobs:\n  compile:\n    env:\n      DIRECT: *key\n    steps: []\n"

	violations, err := CheckIsolation([]byte(body), IsolationConfig{BuildJob: "compile", SigningSecrets: []string{"SIGNING_KEY", "OTHER_KEY"}})
	if err != nil || len(violations) != 4 {
		t.Fatalf("violations=%v err=%v", violations, err)
	}

	for index, violation := range violations {
		if violation.Line != 1 || !strings.Contains(violation.Msg, `build job "compile"`) || strings.Contains(violation.Msg, "inherits") != (index >= 2) {
			t.Fatalf("incorrect anchor/consumer attribution: %v", violation)
		}
	}
}

func TestExpressionBoundary_JobOutputForms(t *testing.T) {
	t.Parallel()

	for _, reference := range []string{"needs.producer.outputs.value", "needs['producer'].outputs.value", `needs["producer"]["outputs"].value`, "format('}}{0}', needs['producer'].outputs.value)"} {
		for _, with := range []bool{false, true} {
			consumer := fmt.Sprintf("    if: ${{ %s }}\n", reference)
			if with {
				consumer = fmt.Sprintf("    if: always()\n    with:\n      enabled: ${{ %s }}\n", reference)
			}

			body := "jobs:\n  producer:\n    if: inputs.enabled\n    runs-on: ubuntu-latest\n  consumer:\n    uses: org/repo/workflow.yml@main\n" + consumer

			violations, err := CheckJobGraph([]byte(body))
			if err != nil || len(violations) != 1 || violations[0].Line != 6 || !strings.Contains(violations[0].Msg, `reusable job "consumer"`) || !strings.Contains(violations[0].Msg, `producer "producer"`) {
				t.Fatalf("reference=%s with=%v violations=%v err=%v", reference, with, violations, err)
			}
		}
	}

	if got := producersIn("'needs.producer.outputs.value'", true); len(got) != 0 {
		t.Fatalf("literal string matched: %v", got)
	}
}

func TestExpressionBoundary_JobOutputDedupAndExemptions(t *testing.T) {
	t.Parallel()

	for _, annotation := range []string{"", "# job-graph-guard: allow", "# job-graph-guard: allow reason=owned-proof"} {
		body := "jobs:\n  producer:\n    if: inputs.enabled\n    runs-on: ubuntu-latest\n  consumer:\n    uses: org/repo/workflow.yml@main\n    " + annotation + "\n    if: always() && needs['producer'].outputs.value && needs.producer.outputs.value\n    with:\n      value: ${{ needs[\"producer\"][\"outputs\"].value }}\n  neighbor:\n    uses: org/repo/workflow.yml@main\n    if: needs['producer'].outputs.value\n"

		violations, err := CheckJobGraph([]byte(body))
		if err != nil {
			t.Fatal(err)
		}

		want := 2
		if strings.Contains(annotation, "reason=") {
			want = 1
		} else if annotation != "" {
			want = 3
		}

		if len(violations) != want {
			t.Fatalf("annotation=%q violations=%v", annotation, violations)
		}

		neighbor := violations[len(violations)-1]
		if neighbor.Line != 12 || !strings.Contains(neighbor.Msg, `reusable job "neighbor"`) {
			t.Fatalf("exemption leaked to neighbor: %v", violations)
		}

		if annotation != "" && !strings.Contains(annotation, "reason=") && !strings.Contains(violations[0].Msg, "with no reason") {
			t.Fatalf("unreasoned waiver accepted: %v", violations)
		}
	}
}
