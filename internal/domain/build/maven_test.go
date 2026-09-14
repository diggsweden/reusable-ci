// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestIsSnapshot_RecognisesSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  bool
	}{
		{"snapshot_with_version", "1.2.3-SNAPSHOT", true},
		{"plain_release_is_not", "1.2.3", false}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"snapshot_with_trailing_qualifier_is_not", "0.0.1-SNAPSHOT-rc1", false},
		{"empty_is_not", "", false},
		{"bare_snapshot_marker_is", "-SNAPSHOT", true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, build.IsSnapshot(testCase.given))
		})
	}
}

func TestRenderMavenSummary_SkipTestsTrue_FullMatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 34, 56, 0, time.UTC)

	got := build.RenderMavenSummary(build.MavenSummaryInput{
		BuildType:   "lib",
		GroupID:     "se.digg.example",
		ArtifactID:  "demo",
		Version:     "1.2.3-SNAPSHOT",
		JavaVersion: "25",
		SkipTests:   true,
		IsSnapshot:  true,
	}, now)

	wantLines := []string{
		"## Maven Build Summary 🔨",
		"",
		"- **Type:** lib",
		"- **Artifact:** `se.digg.example:demo:1.2.3-SNAPSHOT`",
		"- **Java:** 25",
		"- **Tests:** ⊘ Skipped",
		"- **Snapshot:** true",
		"",
		"*Build completed at 2026-05-10 12:34:56 UTC*",
	}
	require.Equal(t, strings.Join(wantLines, "\n")+"\n", got)
}

func TestRenderMavenSummary_TestsExecuted_Markers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	got := build.RenderMavenSummary(build.MavenSummaryInput{
		BuildType:   "app", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		GroupID:     "g",
		ArtifactID:  "a",
		Version:     "1.0.0",
		JavaVersion: "21",
		SkipTests:   false,
		IsSnapshot:  false,
	}, now)

	require.Contains(t, got, "- **Tests:** ✓ Executed\n")
	require.Contains(t, got, "- **Snapshot:** false\n")
}

func TestParsePOM_LiteralFields(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>se.digg.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.2.3-SNAPSHOT</version>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "se.digg.example", pom.GroupID)
	require.Equal(t, "demo", pom.ArtifactID)
	require.Equal(t, "1.2.3-SNAPSHOT", pom.Version)
}

func TestParsePOM_InheritsGroupAndVersionFromParent(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?>
<project>
  <parent>
    <groupId>se.digg.platform</groupId>
    <artifactId>platform-bom</artifactId>
    <version>2.0.0</version>
  </parent>
  <artifactId>child-module</artifactId>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "se.digg.platform", pom.GroupID)
	require.Equal(t, "2.0.0", pom.Version)
	require.Equal(t, "child-module", pom.ArtifactID)
}

// TestParsePOM_ChildFieldsWinAndTheParentIsReturnedWhole covers the case the
// inheritance test cannot: both the child and its parent set groupId and
// version, to different values. With only an omitted-child fixture, a parser
// that always preferred the parent passed. The parent record itself was never
// read back either, though it is returned for callers to use.
//
// Values are padded to pin the normalisation, which is trimming and nothing
// else -- in particular the case of a lowercase "-snapshot" qualifier is kept
// as written rather than normalised into the uppercase suffix IsSnapshot looks
// for.
func TestParsePOM_ChildFieldsWinAndTheParentIsReturnedWhole(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?>
<project>
  <parent>
    <groupId> se.digg.platform </groupId>
    <artifactId>platform-bom</artifactId>
    <version>2.0.0</version>
  </parent>
  <groupId>
    se.digg.child
  </groupId>
  <artifactId> child-module </artifactId>
  <version> 1.4.0-snapshot </version>
</project>`)

	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, build.POM{
		GroupID:    "se.digg.child",
		ArtifactID: "child-module",
		Version:    "1.4.0-snapshot",
		Parent: build.POMParent{
			GroupID:    "se.digg.platform",
			ArtifactID: "platform-bom",
			Version:    "2.0.0",
		},
	}, pom)
}

func TestParsePOM_DoesNotResolveProperties(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?>
<project>
  <groupId>g</groupId>
  <artifactId>a</artifactId>
  <version>${revision}</version>
</project>`)
	pom, err := build.ParsePOM(body)
	require.NoError(t, err)
	require.Equal(t, "${revision}", pom.Version)
	require.True(t, build.POMHasUnresolvedProperty(pom.Version))
	require.False(t, build.POMHasUnresolvedProperty(pom.GroupID))
}

func TestParsePOM_RejectsMalformedXML(t *testing.T) {
	t.Parallel()

	_, err := build.ParsePOM([]byte(`<project><unclosed>`))
	// A pom.xml that will not parse is the adopter's project configuration:
	// EX_CONFIG (78). No caller classifies it on ParsePOM's behalf, so
	// without the sentinel here it reached the operator as EX_SOFTWARE (70),
	// "file a bug".
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, err.Error(), "parse pom.xml")
	// The decoder's own message survives, so the operator sees what is wrong
	// with the XML rather than only that something is.
	require.Contains(t, err.Error(), "XML syntax error")
}

func TestParsePOM_RejectsContentOutsideDocument(t *testing.T) {
	t.Parallel()

	const completePOM = `<project><version>1.2.3</version><groupId>gov.fixture</groupId><artifactId>child</artifactId></project>`
	for _, tc := range []struct{ name, body string }{
		{"broken suffix", completePOM + `<broken`},
		{"second root", completePOM + `<project/>`},
		{"trailing text", completePOM + `unexpected`},
		{"leading text", `unexpected` + completePOM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pom, err := build.ParsePOM([]byte(tc.body))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), "parse pom.xml")
			require.Equal(t, build.POM{}, pom)
		})
	}
}

func TestParsePOM_AcceptsDocumentEnvelopeWithoutSchemaValidation(t *testing.T) {
	t.Parallel()

	const declaration = `<?xml version="1.0" encoding="UTF-8"?>`
	for _, tc := range []struct{ name, prolog, open, close string }{
		{"default namespace", declaration, `<project xmlns="http://maven.apache.org/POM/4.0.0">`, `</project>`},
		{"prefixed namespace", declaration, `<m:project xmlns:m="http://maven.apache.org/POM/4.0.0">`, `</m:project>`},
		{"UTF-8 BOM", "\xef\xbb\xbf" + declaration, `<project xmlns="http://maven.apache.org/POM/4.0.0">`, `</project>`},
		{"no declaration", "", `<project>`, `</project>`},
		{"BOM without declaration", "\xef\xbb\xbf", `<project>`, `</project>`},
		{"prolog DOCTYPE", declaration + `<!-- before DOCTYPE --><?prepare?><!DOCTYPE project>`, `<project>`, `</project>`},
		{"DOCTYPE without declaration", "<!DOCTYPE\tproject>", `<project>`, `</project>`},
		{"opaque DTD", declaration + `<!DOCTYPE project [<!ELEMENT project ANY>]>`, `<project>`, `</project>`},
		{"unresolved external DTD", declaration + `<!DOCTYPE project SYSTEM "https://example.invalid/never-fetch.dtd">`, `<project>`, `</project>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := tc.prolog + `
<!-- before the root -->
<?build mode="ci"?>
<?xml-stylesheet type="text/xsl" href="local.xsl"?>
` + tc.open + `
  <parent><groupId>gov.parent</groupId><artifactId>parent</artifactId><version>${revision}</version></parent>
  <artifactId>child</artifactId>
  <build><plugins><plugin>
    <artifactId>unrelated-plugin</artifactId>
    <configuration>
      <project><nested/></project>
      <custom xmlns="urn:plugin"><value><![CDATA[<unparsed> & text]]></value></custom>
    </configuration>
  </plugin></plugins></build>
` + tc.close + `
<!-- after the root -->
<?finished?>
<?xml-stylesheet type="text/xsl" href="after.xsl"?>
`
			pom, err := build.ParsePOM([]byte(body))
			require.NoError(t, err)
			require.Equal(t, build.POM{
				Version: "${revision}", GroupID: "gov.parent", ArtifactID: "child",
				Parent: build.POMParent{Version: "${revision}", GroupID: "gov.parent", ArtifactID: "parent"},
			}, pom)
		})
	}
}
