// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOperatorCredentialReferences_FindsEverySpellingAndNoOther drives the
// credential detector over owned sources.
//
// The guard's job is to keep minting an operator credential inside the
// composition root, and it passes today because nothing outside mints one. A
// detector that had stopped recognizing the call would pass the same way, and
// the next place that minted one would go unnoticed. So each spelling that
// reaches the same function is checked here, along with the lookalikes that
// must not be reported: a different package's identically named symbol, a
// local variable shadowing the import, and a method that merely shares the name.
func TestOperatorCredentialReferences_FindsEverySpellingAndNoOther(t *testing.T) {
	t.Parallel()

	const runcontext = internalPrefix + "runcontext"

	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "the ordinary import",
			source: "package owned\n\nimport \"" + runcontext + "\"\n\nvar _ = runcontext.OperatorCredential(\"t\")\n",
			want:   1,
		},
		{
			name:   "a renamed import",
			source: "package owned\n\nimport rc \"" + runcontext + "\"\n\nvar _ = rc.OperatorCredential(\"t\")\n",
			want:   1,
		},
		{
			name:   "a dot import",
			source: "package owned\n\nimport . \"" + runcontext + "\"\n\nvar _ = OperatorCredential(\"t\")\n",
			want:   1,
		},
		{
			name:   "more than one mint in a file",
			source: "package owned\n\nimport \"" + runcontext + "\"\n\nvar a = runcontext.OperatorCredential(\"t\")\nvar b = runcontext.OperatorCredential(\"u\")\n",
			want:   2,
		},
		{
			name:   "the same name from another package",
			source: "package owned\n\nimport \"example.com/other/runcontext\"\n\nvar _ = runcontext.OperatorCredential(\"t\")\n",
		},
		{
			name:   "a method that merely shares the name",
			source: "package owned\n\nimport \"" + runcontext + "\"\n\nvar _ = runcontext.Workspace()\n\nfunc use(d deps) { d.OperatorCredential() }\n\ntype deps struct{}\n\nfunc (deps) OperatorCredential() {}\n",
		},
		{
			name:   "no reference at all",
			source: "package owned\n\nimport \"" + runcontext + "\"\n\nvar _ = runcontext.Workspace()\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "owned.go", testCase.source, 0)
			require.NoError(t, err)
			require.Len(t, operatorCredentialReferences(file), testCase.want)
		})
	}
}
