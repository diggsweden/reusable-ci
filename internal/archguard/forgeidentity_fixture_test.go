// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/parser"
	"go/token"
	"testing"
)

// The app tree is clean, which is the state a guard is least trustworthy in.
// These fixtures feed the detector each spelling directly.
//
// The `substring` column records what the six-string list this replaced would
// have said. Two rows are the reason the replacement happened: `!=` is how both
// live violations were written, and neither was ever reported.

func TestForgeIdentityBranches_ResolvesTheConstantNotTheSpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		imports   string
		body      string
		want      bool
		substring string
	}{
		{
			name: "switch case", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) int {\n\tswitch a {\n\tcase provider.ForgeGitHub:\n\t\treturn 1\n\t}\n\n\treturn 0\n}\n",
			want: true, substring: "caught",
		},
		{
			name: "equality", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) bool { return a == provider.ForgeGitLab }\n",
			want: true, substring: "caught",
		},
		{
			name: "inequality", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) bool { return a != provider.ForgeGitHub }\n",
			want: true, substring: "MISSED — this is how both live violations were written",
		},
		{
			name: "ForgeLocal", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) bool { return a == provider.ForgeLocal }\n",
			want: true, substring: "MISSED — the list never had this constant",
		},
		{
			name: "operands reversed", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) bool { return provider.ForgeGitHub == a }\n",
			want: true, substring: "MISSED",
		},
		{
			name: "renamed import", imports: `p "` + providerImportPath + `"`,
			body: "func f(a p.ForgeAPI) bool { return a == p.ForgeGitHub }\n",
			want: true, substring: "MISSED",
		},
		{
			name: "dot import", imports: `. "` + providerImportPath + `"`,
			body: "func f(a ForgeAPI) bool { return a == ForgeGitHub }\n",
			want: true, substring: "MISSED",
		},
		{
			name: "bound to a local first", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) bool {\n\tgh := provider.ForgeGitHub\n\n\treturn a == gh\n}\n",
			want: true, substring: "MISSED",
		},
		{
			name: "switch true with a comparison arm", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) int {\n\tswitch {\n\tcase a == provider.ForgeForgejo:\n\t\treturn 1\n\t}\n\n\treturn 0\n}\n",
			want: true, substring: "MISSED — the case arm is a comparison, not the constant",
		},
		{
			name: "extra whitespace", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) int {\n\tswitch a {\n\tcase  provider.ForgeGitHub:\n\t\treturn 1\n\t}\n\n\treturn 0\n}\n",
			want: true, substring: "MISSED — two spaces is a different string",
		},

		{
			name: "a comment describing the rule", imports: `"` + providerImportPath + `"`,
			body: "// Never write case provider.ForgeGitHub here.\nfunc f(a provider.ForgeAPI) bool { return a == \"\" }\n",
			want: false, substring: "FALSE POSITIVE — the first one teaches people to phrase around the guard",
		},
		{
			name: "the text inside a string", imports: `"` + providerImportPath + `"`,
			body: "func f(a provider.ForgeAPI) string { _ = a\n\n\treturn \"case provider.ForgeGitHub\" }\n",
			want: false, substring: "FALSE POSITIVE",
		},
		{
			name: "passing a constant, not comparing it", imports: `"` + providerImportPath + `"`,
			body: "func f(g func(provider.ForgeAPI)) { g(provider.ForgeGitHub) }\n",
			want: false, substring: "not caught, correctly: handing the model a value is not branching on it",
		},
		{
			name: "a same-named constant from another package", imports: `"example.com/other"`,
			body: "func f(a other.Thing) bool { return a == other.ForgeGitHub }\n",
			want: false, substring: "MISSED as a false positive: the substring list would flag `== provider.ForgeGitHub` only, so this one it got right by luck",
		},
		{
			name: "the provider package not imported at all", imports: `"strings"`,
			body: "func f(a string) bool { return a == strings.TrimSpace(\"ForgeGitHub\") }\n",
			want: false, substring: "not caught, correctly",
		},
		{
			name: "comparing two forge-typed values", imports: `"` + providerImportPath + `"`,
			body: "func f(a, b provider.ForgeAPI) bool { return a == b }\n",
			want: false, substring: "not caught, correctly: no concrete identity is named",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := "package fixture\n\nimport " + tc.imports + "\n\n" + tc.body

			fset := token.NewFileSet()

			file, err := parser.ParseFile(fset, "fixture.go", source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse fixture: %v\n%s", err, source)
			}

			findings := forgeIdentityBranches(fset, file)
			if got := len(findings) > 0; got != tc.want {
				t.Errorf("reported=%v (%v), want %v\nold substring guard: %s\n%s",
					got, findings, tc.want, tc.substring, source)
			}
		})
	}
}
