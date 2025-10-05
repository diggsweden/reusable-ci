// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestMobileSecretBoundary_AndroidDestinations(t *testing.T) { //nolint:gocognit // one adversary matrix covers both writers with exact byte/mode/nonmutation assertions.
	t.Parallel()

	for _, file := range []string{"release.keystore", "secrets.properties"} {
		for _, kind := range []string{"fresh", "existing", "leaf link", "parent link", "directory"} {
			t.Run(file+"/"+kind, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				dir := fsys.MkdirAll("destination")
				canary := fsys.WriteFile("outside/canary", []byte("outside bytes"))
				path := filepath.Join(dir, file)

				switch kind {
				case "existing":
					if err := os.WriteFile(path, []byte("old bytes"), 0o644); err != nil { //nolint:gosec // deliberately broad existing-file mode tests replacement hardening.
						t.Fatal(err)
					}
				case "leaf link":
					if err := os.Symlink(canary, path); err != nil {
						t.Fatal(err)
					}
				case "parent link":
					dir = filepath.Join(fsys.Root, "linked")
					if err := os.Symlink(filepath.Dir(canary), dir); err != nil {
						t.Fatal(err)
					}
				case "directory":
					fsys.MkdirAll("destination", file)
				}

				before := ownedTree(t, fsys.Root)

				var out bytes.Buffer

				encoded := base64.StdEncoding.EncodeToString([]byte("new secret bytes"))

				var err error
				if file == "release.keystore" {
					err = appbuild.AndroidDecodeKeystore(&out, &out, appbuild.AndroidDecodeKeystoreInput{Dir: dir, Base64: encoded})
				} else {
					err = appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{Dir: dir, Base64: encoded})
				}

				if kind == "fresh" || kind == "existing" {
					if err != nil {
						t.Fatal(err)
					}

					body, readErr := os.ReadFile(path)

					info, statErr := os.Lstat(path)
					if readErr != nil || statErr != nil || string(body) != "new secret bytes" || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
						t.Fatalf("body=%q info=%v errors=%v/%v", body, info, readErr, statErr)
					}

					if out.Len() == 0 {
						t.Fatal("missing success output")
					}
				} else if !errors.Is(err, errs.ErrValidation) || out.Len() != 0 || !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
					t.Fatalf("err=%v output=%s or changed tree", err, &out)
				}

				if string(fsys.ReadFile("outside/canary")) != "outside bytes" {
					t.Fatal("outside canary changed")
				}
			})
		}
	}
}

func TestMobileSecretBoundary_DecodedRefusalsHaveNoEffects(t *testing.T) {
	for _, encoded := range []string{" \r\n\t", "%%%", strings.Repeat("YQ==", 9<<20)} {
		fsys := xcodeExportFixture(t)
		before := ownedTree(t, fsys.Root)

		var out bytes.Buffer

		sec := &fakeSecurity{}
		good := base64.StdEncoding.EncodeToString([]byte("valid fake material"))

		calls := []func() error{
			func() error {
				return appbuild.AndroidDecodeKeystore(&out, &out, appbuild.AndroidDecodeKeystoreInput{Dir: fsys.Root, Base64: encoded})
			},
			func() error {
				return appbuild.AndroidWriteSecretsProperties(&out, appbuild.AndroidWriteSecretsPropertiesInput{Dir: fsys.Root, Base64: encoded})
			},
			func() error {
				return appbuild.XcodeSetupCodeSigning(t.Context(), sec, &out, appbuild.XcodeSetupCodeSigningInput{CertBase64: good, PPBase64: encoded, KeychainPassword: "fake", TempDir: fsys.Root, ProvisioningProfilesDir: fsys.Root})
			},
			func() error {
				return appbuild.XcodeExportIPA(t.Context(), &fakeXcodeBuild{run: func([]string) { t.Fatal("Xcode on invalid blob") }}, &out, &out, appbuild.XcodeExportIPAInput{ExportOptionsBase64: encoded})
			},
		}
		for _, call := range calls {
			if err := call(); !errors.Is(err, errs.ErrMalformedInput) {
				t.Fatalf("err=%v", err)
			}

			if len(sec.calls) != 0 || out.Len() != 0 || !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
				t.Fatal("invalid secret caused effects")
			}
		}
	}
}

func TestMobileArtifactBoundary_RejectsLinksAndWrongTypes(t *testing.T) { //nolint:gocognit // both discovery entry points face the same independent file-type adversaries.
	for _, android := range []bool{true, false} {
		for _, kind := range []string{"valid", "leaf link", "parent link", "directory app", "file archive"} {
			t.Run(strings.Join([]string{map[bool]string{true: "android", false: "xcode"}[android], kind}, "/"), func(t *testing.T) {
				fsys := testfs.NewReal(t)
				fsys.Chdir()

				directory, ext := "build", ".ipa"
				if android {
					directory, ext = "app/build/outputs", ".apk"
				}

				fsys.MkdirAll(directory)
				fsys.WriteFile(directory+"/a-good"+ext, []byte("application"))
				canary := fsys.WriteFile("outside-canary", []byte("outside bytes"))

				switch kind {
				case "leaf link":
					if err := os.Symlink(canary, filepath.Join(fsys.Root, directory, "z-bad"+ext)); err != nil {
						t.Fatal(err)
					}
				case "parent link":
					if err := os.Symlink(fsys.Root, filepath.Join(fsys.Root, directory, "nested")); err != nil {
						t.Fatal(err)
					}
				case "directory app":
					fsys.MkdirAll(directory, "z-bad"+ext)
				case "file archive":
					if android {
						return
					}

					fsys.WriteFile(directory+"/z-bad.xcarchive", []byte("not a directory"))
				}

				before := ownedTree(t, fsys.Root)

				var (
					out bytes.Buffer
					err error
				)
				if android {
					err = appbuild.AndroidListArtifacts(&out, appbuild.AndroidListArtifactsInput{Root: fsys.Root, BuildModule: "app"})
				} else {
					err = appbuild.XcodeListBuiltArtifacts(&out)
				}

				if kind == "valid" {
					if err != nil || !strings.Contains(out.String(), "a-good"+ext) {
						t.Fatalf("err=%v output=%s", err, &out)
					}
				} else if !errors.Is(err, errs.ErrValidation) || out.Len() != 0 {
					t.Fatalf("err=%v output=%s", err, &out)
				}

				if !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
					t.Fatal("discovery changed tree")
				}
			})
		}
	}
}

func TestXcodeIdentityBoundary_RefusesBeforeEffects(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.MkdirAll("App.xcodeproj")
	fsys.MkdirAll("App.xcworkspace")
	fsys.WriteFile("File.xcodeproj", []byte("not a project directory"))

	for _, pair := range [][2]string{{"", ""}, {"App.xcworkspace", "App.xcodeproj"}, {"", "missing.xcodeproj"}, {"", "File.xcodeproj"}, {"   ", " "}} {
		before := ownedTree(t, fsys.Root)
		ops := &fakeXcodeBuild{}

		var out bytes.Buffer

		err := appbuild.XcodeArchive(t.Context(), ops, &out, &out, appbuild.XcodeArchiveInput{Workspace: pair[0], Project: pair[1], Scheme: "Scheme", Configuration: "Release", Destination: "generic/platform=iOS"})
		if err == nil || len(ops.calls) != 0 || out.Len() != 0 || !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
			t.Fatalf("pair=%v err=%v calls=%v output=%s", pair, err, ops.calls, &out)
		}
	}
}

func TestXcodeExportBoundary_CleansOnDependencyFailure(t *testing.T) {
	fsys := xcodeExportFixture(t)
	cause := errors.New("export sentinel") //nolint:err113 // unique dependency error identity, not a production error category.

	var path string

	ops := &fakeXcodeBuild{err: cause, run: func(args []string) {
		path = args[6]
		if filepath.Dir(filepath.Dir(path)) != fsys.Path("TMPDIR") {
			t.Fatalf("export plist escaped fixture temp root: %s", path)
		}

		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}}

	err := appbuild.XcodeExportIPA(t.Context(), ops, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{ExportOptionsBase64: "cGxpc3Q="})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "-exportArchive") {
		t.Fatalf("err=%v", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist survived: %v", err)
	}

	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging survived: %v", err)
	}
}

func TestXcodeExportBoundary_OSRootAlias(t *testing.T) {
	fsys := xcodeExportFixture(t)
	root := fsys.MkdirAll("actual-temp")

	alias := fsys.Path("os-temp-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMPDIR", alias)

	ops := &fakeXcodeBuild{run: func(args []string) {
		if !strings.HasPrefix(args[6], root+string(filepath.Separator)) {
			t.Fatalf("noncanonical temporary path: %s", args[6])
		}
	}}
	if err := appbuild.XcodeExportIPA(t.Context(), ops, io.Discard, io.Discard, appbuild.XcodeExportIPAInput{ExportOptionsBase64: "cGxpc3Q="}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || len(ops.calls) != 1 {
		t.Fatalf("temporary entries=%v err=%v calls=%v", entries, err, ops.calls)
	}
}
