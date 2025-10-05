// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

//go:embed release-images.schema.json.tmpl
var releaseImagesSchemaTemplate string

// RenderReleaseImagesJSONSchema renders the ledger's JSON Schema from the
// schema patterns and enums. Consumers (forgejo-ci vendors it next to the
// binary) get portable editor lint and documentation; the strict Go OCI parser
// remains the runtime authority. Byte-stable output means `git diff` highlights
// only genuine contract changes.
func RenderReleaseImagesJSONSchema() string {
	kinds := []string{string(ImageKindRelease), string(ImageKindBase)}

	quoted := make([]string, len(kinds))
	for i, k := range kinds {
		quoted[i] = jsonQuote(k)
	}

	replacer := strings.NewReplacer(
		"{{IMAGE_KINDS}}", strings.Join(quoted, ", "),
		"{{IMAGE_REF_PATTERN}}", jsonQuote(imageRefSchemaRE.String()),
		"{{TAG_REF_PATTERN}}", jsonQuote(tagRefSchemaRE.String()),
		"{{SBOM_PATTERN}}", jsonQuote(sbomRE.String()),
		"{{DIGEST_PATTERN}}", jsonQuote(container.DigestPattern),
		"{{SHA256_HEX_PATTERN}}", jsonQuote(container.SHA256HexPattern),
	)

	return replacer.Replace(releaseImagesSchemaTemplate)
}

// jsonQuote renders s as a JSON string literal (escaping backslashes in the
// regex patterns the schema embeds).
func jsonQuote(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string cannot fail; keep the signature simple.
		panic(err)
	}

	return string(encoded)
}
