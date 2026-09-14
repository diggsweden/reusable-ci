// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestDocumentationBoundary_ParsesOnlyCompleteGuardRows(t *testing.T) {
	t.Parallel()

	const table = "| Package | Guards | Reads |\n|---|---|---|\n| `internal/syncguard` | drift | files |\n| `internal/ci2guard` | policy | source |\n"

	names, err := guardPackagesNamedIn("Prose `internal/fakeguard`\n\n" + table)
	require.NoError(t, err)
	require.Equal(t, []string{"syncguard", "ci2guard"}, names)

	for _, doc := range []string{"`internal/syncguard`", strings.Replace(table, "drift", "", 1), table + "| `internal/syncguard` | drift | files |\n", table + "\n" + table} {
		_, err := guardPackagesNamedIn(doc)
		require.Error(t, err)
	}
}

func TestDocumentationBoundary_SectionsAreBidirectional(t *testing.T) {
	t.Parallel()

	const good = "<!-- schema-values:sign.method -->\n`gpg`, `sigstore`, `kms`\n<!-- /schema-values:sign.method -->"

	values, err := documentedValues(good, "sign.method")
	require.NoError(t, err)
	require.Equal(t, []string{"gpg", "sigstore", "kms"}, values)

	_, err = documentedValues("Mention `gpg`, `sigstore`, `kms` elsewhere", "sign.method")
	require.Error(t, err)
	_, err = documentedValues(good+good, "sign.method")
	require.Error(t, err)
	values, err = documentedValues(strings.Replace(good, "`kms`", "`kms`, `stale`", 1), "sign.method")
	require.NoError(t, err)
	require.NotEqual(t, []string{"gpg", "sigstore", "kms"}, values)
}

func TestGeneratedDifferenceBoundary_IsBoundedAndActionable(t *testing.T) {
	t.Parallel()

	actual := []byte(strings.Repeat("x", 100000) + "a")
	want := []byte(strings.Repeat("x", 100000) + "b")

	message := generatedDifference("docs/example", "just refresh", actual, want)
	for _, expected := range []string{"docs/example", "just refresh", "100001 bytes", "sha256=", "byte 100000", "actual context:", "expected context:"} {
		require.Contains(t, message, expected)
	}

	require.Less(t, len(message), 1000)
	require.Empty(t, generatedDifference("same", "refresh", want, want))
	require.NotEmpty(t, generatedDifference("empty", "refresh", nil, want))
}
