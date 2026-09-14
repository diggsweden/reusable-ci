// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestParse_Maven(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: "25"
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := len(c.Artifacts); got != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", got)
	}

	a := c.Artifacts[0] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if a.Name != "my-app" || a.ProjectType != projecttype.Maven || a.BuildType != "application" {
		t.Errorf("artifact mismatch: %+v", a)
	}

	if a.Maven == nil || a.Maven.JavaVersion != "25" {
		t.Errorf("maven.java-version = %+v, want 25", a.Maven)
	}
}

func TestParse_PolyglotWithContainers(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - name: backend
    project-type: maven
    publish-to: [maven-central]
  - name: frontend
    project-type: npm
containers:
  - name: my-app
    from: [backend, frontend]
    container-file: Containerfile
    enable-slsa: true
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(c.Artifacts) != 2 {
		t.Errorf("Artifacts = %d, want 2", len(c.Artifacts))
	}

	if len(c.Containers) != 1 {
		t.Errorf("Containers = %d, want 1", len(c.Containers))
	}

	if !c.Containers[0].EnableSLSAEffective() {
		t.Errorf("EnableSLSA not parsed")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := config.Parse(nil)
	// Every Parse failure classifies as ErrInvalidConfig (EX_CONFIG, 78) --
	// FuzzParseConfig asserts that for arbitrary input, so the named cases
	// state it too rather than settling for "some error".
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v, want substring 'empty'", err)
	}
}

func TestParse_RejectsPresentEmptyBuildTypeWithOmissionAdvice(t *testing.T) {
	t.Parallel()

	for _, value := range []string{`""`, "''", "null", "~", "", "&kind ''", "&kind null"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Parse([]byte("artifacts:\n  - name: first\n    project-type: maven\n    build-type: library\n  - name: later\n    project-type: maven\n    build-type: " + value + "\n"))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Nil(t, cfg, "a later invalid artifact must not yield a partial config")
			require.Contains(t, err.Error(), `artifact "later" (maven)`)
			require.Contains(t, err.Error(), "build-type must not be empty or null")
			require.Contains(t, err.Error(), "omit build-type")
			require.Equal(t, 1, strings.Count(err.Error(), errs.ErrInvalidConfig.Error()))
		})
	}
}

func TestParse_BuildTypeRetainsDecodeCause(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"[application]", "{kind: library}", "&kind [*kind]"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Parse([]byte("artifacts:\n  - name: bad\n    project-type: maven\n    build-type: " + value + "\n"))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Nil(t, cfg)
			require.Contains(t, err.Error(), `artifact "bad" (maven): invalid build-type`)

			var cause *yaml.TypeError
			require.ErrorAs(t, err, &cause)
			require.Contains(t, cause.Error(), "cannot unmarshal")
			require.Equal(t, 1, strings.Count(err.Error(), errs.ErrInvalidConfig.Error()))
		})
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := config.Parse([]byte("not: valid: yaml: at: all: \n  - "))
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestParse_RejectsTrailingYAMLDocument(t *testing.T) {
	t.Parallel()

	_, err := config.Parse([]byte("artifacts: []\n---\ncontainers: []\n"))
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(err.Error(), "trailing YAML document") {
		t.Errorf("err = %v, want it to name the second document", err)
	}
}

// TestParse_RejectsUnknownKeysAtEveryLevel pins the strict-decode contract:
// a typo'd key is refused with the key named, never silently dropped — at
// the top level, the artifact level, and (pre-existing behavior) inside the
// per-ecosystem config block.
func TestParse_RejectsUnknownKeysAtEveryLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		doc        string
		key        string
		correctKey string
		context    string
	}{
		{"top_level", "artifactz:\n  - name: x\n    project-type: go\n", "artifactz", "artifacts", "config.Config"},
		{"artifact_level", "artifacts:\n  - name: x\n    project-typ: go\n", "project-typ", "project-type", "artifact entry"},
		{"container_level", "artifacts:\n  - name: x\n    project-type: npm\ncontainers:\n  - name: img\n    containerfilez: Containerfile\n", "containerfilez", "container-file", "config.Container"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Parse([]byte(tc.doc))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Nil(t, cfg)
			require.Equal(t, 1, strings.Count(err.Error(), "field "+tc.key+" not found"))
			require.Equal(t, 1, strings.Count(err.Error(), "not found"))
			require.Contains(t, err.Error(), "artifacts.yml")
			require.Contains(t, err.Error(), tc.context)

			cfg, err = config.Parse([]byte(strings.Replace(tc.doc, tc.key+":", tc.correctKey+":", 1)))
			require.NoError(t, err)
			require.NotNil(t, cfg)
			require.NoError(t, config.Validate(cfg))
		})
	}
}

func TestParse_PopulatesTypedSubStructForEachEcosystem(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: my-svc
    project-type: go
    config:
      build-mode: container-first
      binary-name: my-svc
      platforms: linux/amd64,linux/arm64
      skip-tests: true
  - name: my-lib
    project-type: maven
    build-type: library
    config:
      java-version: "21"
      maven-profile: central-release
  - name: my-app
    project-type: gradle-android
    config:
      build-module: app
      build-types: release
      include-aab: false
  - name: my-web
    project-type: npm
    config:
      node-version: "24"
      attach-pattern: packages/web/*.tgz
  - name: my-jvm
    project-type: gradle
    config:
      java-version: "25"
      gradle-tasks: clean assemble
      gradle-version-file: gradle/release.properties
  - name: my-ios
    project-type: xcode-ios
    config:
      xcode-version: "26"
      scheme: ReleaseApp
      enable-code-signing: false
      use-xcodegen: true
  - name: my-cli
    project-type: cargo
    config:
      binary-name: release-cli
      platforms: linux/arm64
      skip-tests: true
      build-mode: container-first
`)

	c, err := config.Parse(body) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	require.Len(t, c.Artifacts, 7)

	require.NotNil(t, c.Artifacts[0].Go)
	require.Equal(t, "my-svc", c.Artifacts[0].Go.BinaryName)
	require.Equal(t, "linux/amd64,linux/arm64", c.Artifacts[0].Go.Platforms)
	require.Equal(t, config.GoBuildModeContainerFirst, c.Artifacts[0].Go.BuildMode)
	require.True(t, c.Artifacts[0].Go.SkipTests)

	require.NotNil(t, c.Artifacts[1].Maven)
	require.Equal(t, "21", c.Artifacts[1].Maven.JavaVersion)
	require.Equal(t, "central-release", c.Artifacts[1].Maven.MavenProfile)

	require.NotNil(t, c.Artifacts[2].GradleAndroid)
	require.Equal(t, "app", c.Artifacts[2].GradleAndroid.BuildModule)
	require.Equal(t, "release", c.Artifacts[2].GradleAndroid.BuildTypes)
	require.NotNil(t, c.Artifacts[2].GradleAndroid.IncludeAAB)
	require.False(t, *c.Artifacts[2].GradleAndroid.IncludeAAB)

	require.Equal(t, &config.NPMConfig{NodeVersion: "24", AttachPattern: "packages/web/*.tgz"}, c.Artifacts[3].NPM)
	require.Equal(t, &config.GradleConfig{
		JavaVersion: "25", GradleTasks: "clean assemble", GradleVersionFile: "gradle/release.properties",
	}, c.Artifacts[4].Gradle)

	signing := false
	require.Equal(t, &config.XcodeIOSConfig{
		XcodeVersion: "26", Scheme: "ReleaseApp", EnableCodeSigning: &signing, UseXcodegen: true,
	}, c.Artifacts[5].XcodeIOS)
	require.Equal(t, &config.CargoConfig{
		BinaryName: "release-cli", Platforms: "linux/arm64", SkipTests: true, BuildMode: config.CargoBuildModeContainerFirst,
	}, c.Artifacts[6].Cargo)
}

func TestParse_PreservesRequireAuthorization(t *testing.T) {
	t.Parallel()

	cfg, err := config.Parse([]byte(`artifacts:
  - name: ungated
    project-type: maven
    require-authorization: false
  - name: gated
    project-type: maven
    require-authorization: true
  - name: omitted
    project-type: maven
`))
	require.NoError(t, err)
	require.Len(t, cfg.Artifacts, 3)
	require.False(t, cfg.Artifacts[0].RequireAuthorization)
	require.True(t, cfg.Artifacts[1].RequireAuthorization)
	require.False(t, cfg.Artifacts[2].RequireAuthorization)
}

func TestParse_RejectsTypoInEcosystemConfig(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: my-app
    project-type: gradle-android
    config:
      build-module: app
      bogus-aab: false
`)

	_, err := config.Parse(body)
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(err.Error(), `"my-app"`) {
		t.Errorf("expected artifact name in error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "gradle-android") {
		t.Errorf("expected ecosystem in error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "bogus-aab") {
		t.Errorf("expected the offending key in error, got: %v", err)
	}
}

func TestParse_RejectsWrongTypedField(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: bad
    project-type: go
    config:
      skip-tests: "not a bool"
`)

	_, err := config.Parse(body)
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	// The message locates the mistake by artifact, ecosystem and offending
	// value. It does NOT name the key (yaml.v3 reports "line 1" instead),
	// which is worth knowing: the operator gets the value they typed rather
	// than the field it belongs to.
	for _, want := range []string{`"bad"`, "go", "not a bool"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

// TestParse_ClassifiesInvalidConfigExactlyOnce covers the wrapping discipline
// on the nested paths, where three layers each add context.
//
// Every layer used to re-attach errs.ErrInvalidConfig, so the line an operator
// reads ended "...: invalid configuration: invalid configuration". The
// sentinel is attached by whichever layer first knows the meaning; the ones
// above add context with a single %w. errors.Is is indifferent to the
// difference, which is exactly why it needs its own assertion.
func TestParse_ClassifiesInvalidConfigExactlyOnce(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{
		"ecosystem config": "artifacts:\n  - name: bad\n    project-type: go\n    config:\n      skip-tests: \"not a bool\"\n",
		"artifact entry":   "artifacts:\n  - name: x\n    project-typ: go\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Parse([]byte(doc))
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if got := strings.Count(err.Error(), errs.ErrInvalidConfig.Error()); got != 1 {
				t.Errorf("%q appears %d times in the message, want 1:\n%v",
					errs.ErrInvalidConfig.Error(), got, err)
			}
		})
	}
}

func TestParse_RejectsConfigOnMetaArtifact(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: changelog-only
    project-type: meta
    config:
      anything: nope
`)

	_, err := config.Parse(body)
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(err.Error(), "meta") {
		t.Errorf("expected project-type in error, got: %v", err)
	}
}

func TestArtifact_AccessorsHaveSafeDefaults(t *testing.T) {
	t.Parallel()
	// Fabricated Artifact (no Parse). Accessors apply documented
	// defaults when the typed sub-struct is absent or its pointer-bool
	// is nil.
	a := config.Artifact{}
	require.True(t, a.IncludeAAB())
	require.True(t, a.EnableCodeSigning())
	require.Equal(t, "debug,release", a.AndroidBuildTypes())

	// Explicit-false overrides default.
	f := false
	a2 := config.Artifact{GradleAndroid: &config.GradleAndroidConfig{IncludeAAB: &f}}
	require.False(t, a2.IncludeAAB())

	a3 := config.Artifact{XcodeIOS: &config.XcodeIOSConfig{EnableCodeSigning: &f}}
	require.False(t, a3.EnableCodeSigning())
}

func TestParse_ResolvesAliasesThatPointOutsideTheArtifactEntry(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - name: api
    project-type: maven
    config: &jvm
      java-version: "21"
  - name: worker
    project-type: maven
    config: *jvm
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	require.NoError(t, err)
	require.Len(t, c.Artifacts, 2)
	require.NotNil(t, c.Artifacts[1].Maven)
	require.Equal(t, "21", c.Artifacts[1].Maven.JavaVersion)
}

func TestParse_ResolvesMergeKeysBetweenArtifactEntries(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - &base
    name: api
    project-type: maven
    build-type: library
  - <<: *base
    name: worker
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	require.NoError(t, err)
	require.Len(t, c.Artifacts, 2)
	require.Equal(t, "worker", c.Artifacts[1].Name)
	require.Equal(t, projecttype.Maven, c.Artifacts[1].ProjectType)
	require.Equal(t, config.BuildTypeLibrary, c.Artifacts[1].BuildType)
}

func TestParse_AliasedConfigIsStillDecodedStrictly(t *testing.T) {
	t.Parallel()

	// The aliased block is valid for maven but carries a key npm does not
	// know; resolving the alias must not relax the per-ecosystem check.
	in := []byte(`
artifacts:
  - name: api
    project-type: maven
    config: &jvm
      java-version: "21"
  - name: web
    project-type: npm
    config: *jvm
`)

	_, err := config.Parse(in)
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, err.Error(), `artifact "web" (npm)`)
	require.Contains(t, err.Error(), "java-version")
}

func TestParse_CommentOnlyInputIsReportedAsNoDocument(t *testing.T) {
	t.Parallel()

	_, err := config.Parse([]byte("# artifacts.yml — nothing configured yet\n\n"))
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, err.Error(), "no YAML document")
	require.NotContains(t, err.Error(), "unknown or wrong-typed key")
}

func TestParse_RejectsRecursiveAliases(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{
		"artifact": "artifacts:\n  - &entry\n    name: app\n    project-type: maven\n    config: *entry\n",
		"config":   "artifacts:\n  - name: app\n    project-type: maven\n    config: &cfg\n      java-version: *cfg\n",
		"merge":    "artifacts:\n  - &entry\n    <<: *entry\n    name: app\n    project-type: maven\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := config.Parse([]byte(doc))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Nil(t, cfg)
		})
	}
}

func TestParse_PreservesRepeatedAndRedefinedAnchors(t *testing.T) {
	t.Parallel()

	doc := []byte(`artifacts:
  - name: first
    project-type: maven
    config: &jvm
      java-version: &value 21.0
      maven-profile: *value
      settings-path: *value
  - name: second
    project-type: maven
    config: *jvm
  - name: third
    project-type: maven
    config:
      java-version: &value "25.0"
      maven-profile: *value
      settings-path: *value
`)
	cfg, err := config.Parse(doc)
	require.NoError(t, err)
	require.Len(t, cfg.Artifacts, 3)

	for i, version := range []string{"21.0", "21.0", "25.0"} {
		require.Equal(t, &config.MavenConfig{JavaVersion: version, MavenProfile: version, SettingsPath: version}, cfg.Artifacts[i].Maven)
	}
}

func TestParse_ResolvesOverlappingExternalAnchors(t *testing.T) {
	t.Parallel()

	doc := []byte(`artifacts:
  - name: first
    project-type: maven
    config: &jvm
      java-version: &version "21"
      maven-profile: &profile central
  - name: second
    project-type: maven
    config:
      settings-path: *version
      <<: *jvm
      maven-profile: *profile
`)
	cfg, err := config.Parse(doc)
	require.NoError(t, err)
	require.Len(t, cfg.Artifacts, 2)
	require.Equal(t, &config.MavenConfig{JavaVersion: "21", MavenProfile: "central", SettingsPath: "21"}, cfg.Artifacts[1].Maven)
}

func TestParse_RetainsAliasExpansionGuard(t *testing.T) {
	t.Parallel()

	var doc strings.Builder
	doc.WriteString("artifacts:\n  - &a0 {name: app, project-type: maven}\n")

	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&doc, "  - &a%d {<<: [*a%d, *a%d], name: app%d}\n", i, i-1, i-1, i)
	}

	cfg, err := config.Parse([]byte(doc.String()))
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, err.Error(), "excessive aliasing")
	require.Nil(t, cfg)
}

// TestParse_ExplicitBooleanChoicesSurviveParsing covers the tri-state policy
// flags: set true, set false, and omitted.
//
// These fields are *bool precisely so "the operator turned this off" is
// distinguishable from "the operator said nothing", and the Effective helpers
// default a nil pointer to ON. That makes the omitted and explicit-true cases
// look identical from the outside — so the only assertion the suite had,
// `if !EnableSLSAEffective()`, passes just as well when parsing drops the field
// entirely and leaves the pointer nil.
//
// Both directions of that are real. A dropped `enable-slsa: false` generates
// provenance for a container the operator excluded; a dropped
// `enable-scan: false` runs a scan that was deliberately disabled. The pointer
// itself is checked alongside the effective value, because that is what
// separates "parsed as false" from "not parsed at all".
func TestParse_ExplicitBooleanChoicesSurviveParsing(t *testing.T) {
	t.Parallel()

	parseContainer := func(t *testing.T, flags string) config.Container {
		t.Helper()

		in := []byte(`
artifacts:
  - name: backend
    project-type: maven
containers:
  - name: my-app
    from: [backend]
    container-file: Containerfile
` + flags)

		c, err := config.Parse(in)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		if len(c.Containers) != 1 {
			t.Fatalf("Containers = %d, want 1", len(c.Containers))
		}

		return c.Containers[0]
	}

	t.Run("explicit false is preserved", func(t *testing.T) {
		t.Parallel()

		ct := parseContainer(t, "    enable-slsa: false\n    enable-scan: false\n")

		for _, f := range []struct {
			name string
			ptr  *bool
			eff  bool
		}{
			{"enable-slsa", ct.EnableSLSA, ct.EnableSLSAEffective()},
			{"enable-scan", ct.EnableScan, ct.EnableScanEffective()},
		} {
			if f.ptr == nil {
				t.Errorf("%s: pointer is nil, so the explicit false was dropped and the default turns it back on", f.name)

				continue
			}

			if *f.ptr {
				t.Errorf("%s: parsed as true, want false", f.name)
			}

			if f.eff {
				t.Errorf("%s: effective value is true despite an explicit false", f.name)
			}
		}
	})

	t.Run("explicit true is preserved", func(t *testing.T) {
		t.Parallel()

		ct := parseContainer(t, "    enable-slsa: true\n    enable-scan: true\n")

		for _, f := range []struct {
			name string
			ptr  *bool
			eff  bool
		}{
			{"enable-slsa", ct.EnableSLSA, ct.EnableSLSAEffective()},
			{"enable-scan", ct.EnableScan, ct.EnableScanEffective()},
		} {
			if f.ptr == nil || !*f.ptr || !f.eff {
				t.Errorf("%s: explicit true not preserved (ptr=%v, effective=%v)", f.name, f.ptr, f.eff)
			}
		}
	})

	t.Run("omitted leaves the pointer nil and defaults to on", func(t *testing.T) {
		t.Parallel()

		ct := parseContainer(t, "")

		if ct.EnableSLSA != nil || ct.EnableScan != nil {
			t.Errorf("omitted flags produced non-nil pointers (slsa=%v scan=%v); "+
				"the tri-state collapses and a later explicit false becomes indistinguishable",
				ct.EnableSLSA, ct.EnableScan)
		}

		if !ct.EnableSLSAEffective() || !ct.EnableScanEffective() {
			t.Error("omitted flags must default to on")
		}
	})
}
