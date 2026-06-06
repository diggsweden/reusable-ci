// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

// Per-ecosystem typed configurations for the `config:` block.
//
// One of these is non-nil on each Artifact / PlannedArtifact, matching
// the artefact's project type. The plan JSON exposes them via snake_case
// field names — workflows access them as `matrix.artifact.go.binary_name`
// rather than the legacy `matrix.artifact.config['binary-name']` shape.
//
// Strict YAML decoding is applied at parse time so a typo in
// artifacts.yml (`bogus-aab: true`) fails fast with a clear error
// rather than silently taking the default.
//
// Pointer-bool fields distinguish "unset" (nil) from "explicitly false"
// — the planner needs that distinction for defaults like include-aab,
// which is true unless the user wrote `include-aab: false`.

// MavenConfig holds the typed `config:` block for Maven artifacts.
type MavenConfig struct {
	MavenProfile  string `json:"maven_profile,omitempty"  yaml:"maven-profile,omitempty"`
	JavaVersion   string `json:"java_version,omitempty"   yaml:"java-version,omitempty"`
	SettingsPath  string `json:"settings_path,omitempty"  yaml:"settings-path,omitempty"`
	Profile       string `json:"profile,omitempty"        yaml:"profile,omitempty"`
	AttachPattern string `json:"attach_pattern,omitempty" yaml:"attach-pattern,omitempty"`
}

// NPMConfig holds the typed `config:` block for NPM artifacts.
type NPMConfig struct {
	NodeVersion   string `json:"node_version,omitempty"   yaml:"node-version,omitempty"`
	AttachPattern string `json:"attach_pattern,omitempty" yaml:"attach-pattern,omitempty"`
}

// GradleConfig holds the typed `config:` block for plain Gradle JVM
// artifacts (not Android).
type GradleConfig struct {
	GradleTasks       string `json:"gradle_tasks,omitempty"        yaml:"gradle-tasks,omitempty"`
	GradleVersionFile string `json:"gradle_version_file,omitempty" yaml:"gradle-version-file,omitempty"`
}

// GradleAndroidConfig holds the typed `config:` block for Gradle Android
// artifacts. IncludeAAB is a pointer so the planner can distinguish
// "user omitted it" (default true) from "user explicitly set false".
type GradleAndroidConfig struct {
	BuildModule          string `json:"build_module,omitempty"           yaml:"build-module,omitempty"`
	ProductFlavor        string `json:"product_flavor,omitempty"         yaml:"product-flavor,omitempty"`
	BuildTypes           string `json:"build_types,omitempty"            yaml:"build-types,omitempty"`
	IncludeAAB           *bool  `json:"include_aab,omitempty"            yaml:"include-aab,omitempty"`
	EnableAndroidSigning *bool  `json:"enable_android_signing,omitempty" yaml:"enable-android-signing,omitempty"`

	// Google Play publishing options carried alongside the Android build
	// config — they're per-artifact, not separate ecosystems.
	PackageName              string `json:"package_name,omitempty"                            yaml:"package-name,omitempty"`
	GooglePlayTrack          string `json:"google_play_track,omitempty"                       yaml:"google-play-track,omitempty"`
	GooglePlayStatus         string `json:"google_play_status,omitempty"                      yaml:"google-play-status,omitempty"`
	GooglePlayReleaseName    string `json:"google_play_release_name,omitempty"                yaml:"google-play-release-name,omitempty"`
	GooglePlayUserFraction   string `json:"google_play_user_fraction,omitempty"               yaml:"google-play-user-fraction,omitempty"`
	GooglePlayUpdatePriority string `json:"google_play_update_priority,omitempty"             yaml:"google-play-update-priority,omitempty"`
	GooglePlayChangesNotSent bool   `json:"google_play_changes_not_sent_for_review,omitempty" yaml:"google-play-changes-not-sent-for-review,omitempty"`
	WhatsNewDirectory        string `json:"whats_new_directory,omitempty"                     yaml:"whats-new-directory,omitempty"`
	MappingFile              string `json:"mapping_file,omitempty"                            yaml:"mapping-file,omitempty"`
	DebugSymbols             string `json:"debug_symbols,omitempty"                           yaml:"debug-symbols,omitempty"`
}

// XcodeIOSConfig holds the typed `config:` block for Xcode iOS artifacts.
// EnableCodeSigning is a pointer to distinguish unset from explicit false
// (default is true when omitted, matching the previous bash behaviour).
type XcodeIOSConfig struct {
	XcodeVersion      string `json:"xcode_version,omitempty"       yaml:"xcode-version,omitempty"`
	Scheme            string `json:"scheme,omitempty"              yaml:"scheme,omitempty"`
	Workspace         string `json:"workspace,omitempty"           yaml:"workspace,omitempty"`
	Project           string `json:"project,omitempty"             yaml:"project,omitempty"`
	Configuration     string `json:"configuration,omitempty"       yaml:"configuration,omitempty"`
	EnableCodeSigning *bool  `json:"enable_code_signing,omitempty" yaml:"enable-code-signing,omitempty"`
	ExportOptionsVar  string `json:"export_options_var,omitempty"  yaml:"export-options-var,omitempty"`
	UseXcodegen       bool   `json:"use_xcodegen,omitempty"        yaml:"use-xcodegen,omitempty"`
	XcodegenSpec      string `json:"xcodegen_spec,omitempty"       yaml:"xcodegen-spec,omitempty"`
	MacOSVersion      string `json:"macos_version,omitempty"       yaml:"macos-version,omitempty"`
	BuildNumber       string `json:"build_number,omitempty"        yaml:"build-number,omitempty"`
	SubmitForReview   bool   `json:"submit_for_review,omitempty"   yaml:"submit-for-review,omitempty"`
	SkipValidation    bool   `json:"skip_validation,omitempty"     yaml:"skip-validation,omitempty"`
}

// GoConfig holds the typed `config:` block for Go artifacts.
type GoConfig struct {
	MainPackage string      `json:"main_package,omitempty" yaml:"main-package,omitempty"`
	BinaryName  string      `json:"binary_name,omitempty"  yaml:"binary-name,omitempty"`
	Platforms   string      `json:"platforms,omitempty"    yaml:"platforms,omitempty"`
	BuildTags   string      `json:"build_tags,omitempty"   yaml:"build-tags,omitempty"`
	LDFlags     string      `json:"ldflags,omitempty"      yaml:"ldflags,omitempty"`
	SkipTests   bool        `json:"skip_tests,omitempty"   yaml:"skip-tests,omitempty"`
	BuildMode   GoBuildMode `json:"build_mode,omitempty"   yaml:"build-mode,omitempty"`
}

// CargoConfig holds the typed `config:` block for Cargo artifacts.
// BinaryName / Platforms / SkipTests mirror the Go shape so adopters can
// reason about the two ecosystems uniformly. BuildMode is required at
// validation time (same contract as GoConfig.BuildMode).
type CargoConfig struct {
	BinaryName string         `json:"binary_name,omitempty" yaml:"binary-name,omitempty"`
	Platforms  string         `json:"platforms,omitempty"   yaml:"platforms,omitempty"`
	SkipTests  bool           `json:"skip_tests,omitempty"  yaml:"skip-tests,omitempty"`
	BuildMode  CargoBuildMode `json:"build_mode,omitempty"  yaml:"build-mode,omitempty"`
}

// PythonConfig holds the typed `config:` block for Python artifacts.
// Empty today; same rationale as CargoConfig.
type PythonConfig struct{}

// Artifact-level accessors. These read directly from the typed
// sub-struct (populated by Parse with strict YAML decoding) and apply
// the ecosystem's default rule.

// IncludeAAB returns true unless the user explicitly set
// `include-aab: false`. Default: true.
func (a *Artifact) IncludeAAB() bool {
	if a == nil || a.GradleAndroid == nil || a.GradleAndroid.IncludeAAB == nil {
		return true
	}

	return *a.GradleAndroid.IncludeAAB
}

// AndroidBuildTypes returns the comma-separated build types (e.g.
// "debug,release"). Default: "debug,release".
func (a *Artifact) AndroidBuildTypes() string {
	if a == nil || a.GradleAndroid == nil || a.GradleAndroid.BuildTypes == "" {
		return "debug,release"
	}

	return a.GradleAndroid.BuildTypes
}

// EnableCodeSigning returns true unless the user explicitly set
// `enable-code-signing: false`. Default: true.
func (a *Artifact) EnableCodeSigning() bool {
	if a == nil || a.XcodeIOS == nil || a.XcodeIOS.EnableCodeSigning == nil {
		return true
	}

	return *a.XcodeIOS.EnableCodeSigning
}

// EnableSLSAEffective returns true unless the user explicitly set
// `enable-slsa: false` on this container. Default: true.
func (c *Container) EnableSLSAEffective() bool {
	if c == nil || c.EnableSLSA == nil {
		return true
	}

	return *c.EnableSLSA
}

// EnableScanEffective returns true unless the user explicitly set
// `enable-scan: false` on this container. Default: true.
func (c *Container) EnableScanEffective() bool {
	if c == nil || c.EnableScan == nil {
		return true
	}

	return *c.EnableScan
}
