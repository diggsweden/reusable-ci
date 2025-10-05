// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// The target and every link are test-owned. Check bytes and extra sidecars after
// the consumer returns, before TempDir cleanup can hide an unintended write.
func linkedReleaseInput(t *testing.T, name string, parent bool) string {
	t.Helper()
	outside := t.TempDir()
	target := filepath.Join(outside, "nested", name)
	mustWrite(t, target, "outside input")
	t.Cleanup(func() {
		body, err := os.ReadFile(target)
		if err != nil || string(body) != "outside input" {
			t.Errorf("outside input changed: %v", err)
		}

		entries, err := os.ReadDir(filepath.Dir(target))
		if err != nil || len(entries) != 1 {
			t.Errorf("unexpected outside sidecars: %v %v", entries, err)
		}
	})

	if parent {
		if err := os.Symlink(outside, "linked"); err != nil {
			t.Fatal(err)
		}

		return filepath.Join("linked", "nested", name)
	}

	if err := os.MkdirAll("inputs", 0o700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("inputs", name)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestPublishRelease_RejectsLinkedInputsBeforeProvider(t *testing.T) {
	for _, field := range []string{"notes", "asset"} {
		for _, parent := range []bool{false, true} {
			t.Run(field+map[bool]string{false: "/leaf", true: "/parent"}[parent], func(t *testing.T) {
				t.Chdir(t.TempDir())
				mustWrite(t, "safe/notes.md", "notes")
				mustWrite(t, "safe/app.jar", "artifact")
				bad := linkedReleaseInput(t, "selected", parent)

				in := apprelease.PublishReleaseInput{Tag: "v1.0.0", Repository: "owner/app", ReleaseNotesFile: "safe/notes.md", Assets: []string{"safe/app.jar"}}
				if field == "notes" {
					in.ReleaseNotesFile = bad
				} else {
					in.Assets = append(in.Assets, bad)
				}

				provider := fakeprovider.New(t)
				if err := apprelease.PublishRelease(t.Context(), provider, io.Discard, in); !errors.Is(err, errs.ErrMissingInput) {
					t.Errorf("err=%v, want ErrMissingInput", err)
				}

				if len(provider.PublishReleaseCalls()) != 0 {
					t.Error("provider called for a linked input")
				}
			})
		}
	}
}

func TestPublishSelfRuntimeCLI_RejectsLinkedAncestorBeforeProvider(t *testing.T) {
	t.Chdir(t.TempDir())
	outside := t.TempDir()
	writeSelfRuntimeAssets(t, filepath.Join(outside, "dist"), "3.0.0-pre", "")

	if err := os.Symlink(outside, "linked"); err != nil {
		t.Fatal(err)
	}

	publisher := &fakeSelfRuntimePublisher{}

	err := apprelease.PublishSelfRuntimeCLI(t.Context(), publisher, io.Discard, apprelease.PublishSelfRuntimeCLIInput{
		Repository: domainrelease.SelfRuntimeRepository, SourceRef: "refs/heads/main", TargetSHA: strings.Repeat("a", 40), AssetsDir: "linked/dist",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("err=%v, want ErrMissingInput", err)
	}

	if len(publisher.calls) != 0 {
		t.Error("publisher called for a linked ancestor")
	}
}

func TestSign_RejectsLinkedExactInputsBeforeAnySignature(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "parent"}[parent], func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustWrite(t, "safe/app.jar", "artifact")
			mustWrite(t, "checksums.sha256", "checksums")
			bad := linkedReleaseInput(t, "selected.jar", parent)
			signer := &fakeSigner{}

			err := apprelease.SignArtifacts(t.Context(), signer, io.Discard, apprelease.SignInput{Files: []string{"safe/app.jar", bad}, SkipReleaseArtifactsDir: true})
			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err=%v, want ErrValidation", err)
			}

			if len(signer.signed) != 0 {
				t.Errorf("signed before input refusal: %v", signer.signed)
			}
		})
	}
}

func TestGPGSignPackages_RejectsLinkedInputsBeforeKeyOperations(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "parent"}[parent], func(t *testing.T) {
			t.Chdir(t.TempDir())
			bad := linkedReleaseInput(t, "package.deb", parent)
			signer := &fakeGPGPackageSigner{listed: "fpr:::::::::ABCDEF:"}

			_, err := apprelease.GPGSignPackages(t.Context(), signer, io.Discard, apprelease.GPGSignPackagesInput{Dir: filepath.Dir(bad), PrivateKey: testPGPKeyArmor, Fingerprint: "ABCDEF", Passphrase: "secret"})
			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err=%v, want ErrValidation", err)
			}

			if signer.imports != 0 || signer.lists != 0 || len(signer.signed) != 0 {
				t.Error("key operations ran before path refusal")
			}
		})
	}
}

func TestChecksums_RejectsLinkedInputsInEveryDiscoveryBranch(t *testing.T) {
	for _, branch := range []string{"release", "attach", "container", "workdir"} {
		for _, parent := range []bool{false, true} {
			t.Run(branch+map[bool]string{false: "/leaf", true: "/parent"}[parent], func(t *testing.T) {
				t.Chdir(t.TempDir())

				name := "app-sbom.spdx.json"
				if branch == "container" {
					name = "app-analyzed-container-sbom.spdx.json"
				}

				bad := linkedReleaseInput(t, name, parent)
				in := apprelease.ChecksumsInput{OutputFile: "checksums.txt", ReleaseArtifactsDir: "absent-release", SBOMDir: "absent-sboms", WorkingDir: "absent-work"}

				switch branch {
				case "release":
					in.ReleaseArtifactsDir = filepath.Dir(bad)
				case "attach":
					in.AttachArtifacts = bad
				case "container":
					in.SBOMDir = filepath.Dir(bad)
				case "workdir":
					in.WorkingDir = filepath.Dir(bad)
				}

				count, err := apprelease.Checksums(io.Discard, in)
				if !errors.Is(err, errs.ErrValidation) || count != 0 {
					t.Errorf("count=%d err=%v, want zero and ErrValidation", count, err)
				}
			})
		}
	}
}

func TestCreateSBOMZip_RejectsLinkedInputsBeforeArchiveCreation(t *testing.T) {
	for _, branch := range []string{"workdir", "container"} {
		for _, parent := range []bool{false, true} {
			t.Run(branch+map[bool]string{false: "/leaf", true: "/parent"}[parent], func(t *testing.T) {
				t.Chdir(t.TempDir())

				name := "app-sbom.spdx.json"
				if branch == "container" {
					name = "app-analyzed-container-sbom.spdx.json"
				}

				bad := linkedReleaseInput(t, name, parent)

				in := apprelease.SBOMZipInput{ProjectName: "app", Version: "1.0.0", WorkingDir: "absent-work", SBOMDir: "absent-sboms"}
				if branch == "workdir" {
					in.WorkingDir = filepath.Dir(bad)
				} else {
					in.SBOMDir = filepath.Dir(bad)
				}

				_, err := apprelease.CreateSBOMZip(context.Background(), nil, in, io.Discard)
				if !errors.Is(err, errs.ErrValidation) {
					t.Errorf("err=%v, want ErrValidation", err)
				}

				if _, statErr := os.Stat("app-1.0.0-sboms.zip"); !errors.Is(statErr, os.ErrNotExist) {
					t.Error("archive created before refusal")
				}
			})
		}
	}
}

func TestAssembly_RejectsUnsafeFieldsBeforeProvider(t *testing.T) {
	for _, field := range []string{"asset", "sbom", "checksums", "zip"} {
		for _, shape := range []string{"absolute", "parent traversal", "leaf link", "parent link"} {
			t.Run(field+"/"+shape, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				mustWrite(t, "safe/app.jar", "artifact")
				mustWrite(t, "safe/sbom.json", "{}")
				mustWrite(t, "safe/checksums", "checksums")
				mustWrite(t, "safe/sboms.zip", "zip fixture")

				var bad string

				switch shape {
				case "leaf link", "parent link":
					bad = linkedReleaseInput(t, "selected", shape == "parent link")
				default:
					bad = filepath.Join(t.TempDir(), "outside")
					mustWrite(t, bad, "outside input")

					if shape == "parent traversal" {
						var err error

						bad, err = filepath.Rel(root, bad)
						if err != nil {
							t.Fatal(err)
						}
					}
				}

				asm := domainrelease.Assembly{Version: domainrelease.AssemblyVersion, Assets: []domainrelease.AssemblyFile{{Path: "safe/app.jar", Name: "app.jar"}}, SBOMs: []domainrelease.AssemblyFile{{Path: "safe/sbom.json", Name: "sbom.json"}}, ChecksumFile: "safe/checksums", SBOMZipFile: "safe/sboms.zip"}

				switch field {
				case "asset":
					asm.Assets[0].Path = bad
				case "sbom":
					asm.SBOMs[0].Path = bad
				case "checksums":
					asm.ChecksumFile = bad
				case "zip":
					asm.SBOMZipFile = bad
				}

				writeAssembly(t, "assembly.json", asm)
				provider := fakeprovider.New(t)

				err := apprelease.CreateRelease(t.Context(), provider, &fakeFS{}, io.Discard, apprelease.CreateReleaseInput{Tag: "v1.0.0", Repository: "owner/app", AssemblyFile: "assembly.json"})
				if !errors.Is(err, errs.ErrValidation) {
					t.Errorf("err=%v, want ErrValidation", err)
				}

				if len(provider.CreateReleaseCalls()) != 0 {
					t.Error("provider called for an unsafe manifest")
				}
			})
		}
	}
}

func TestAssembly_AllConsumersValidateBeforeActions(t *testing.T) {
	for _, action := range []string{"sign", "checksum", "zip"} {
		t.Run(action, func(t *testing.T) {
			t.Chdir(t.TempDir())
			bad := linkedReleaseInput(t, "app.jar", true)
			mustWrite(t, "safe/sbom.json", "{}")
			mustWrite(t, "safe/checksums", "old checksums")
			mustWrite(t, "safe/sboms.zip", "old zip")
			writeAssembly(t, "assembly.json", domainrelease.Assembly{Version: domainrelease.AssemblyVersion, Assets: []domainrelease.AssemblyFile{{Path: bad, Name: "app.jar"}}, SBOMs: []domainrelease.AssemblyFile{{Path: "safe/sbom.json", Name: "sbom.json"}}, ChecksumFile: "safe/checksums", SBOMZipFile: "safe/sboms.zip"})

			signer := &fakeSigner{}

			var err error

			switch action {
			case "sign":
				err = apprelease.SignArtifacts(t.Context(), signer, io.Discard, apprelease.SignInput{AssemblyFile: "assembly.json"})
			case "checksum":
				_, err = apprelease.Checksums(io.Discard, apprelease.ChecksumsInput{AssemblyFile: "assembly.json"})
			case "zip":
				_, err = apprelease.CreateSBOMZip(t.Context(), nil, apprelease.SBOMZipInput{AssemblyFile: "assembly.json"}, io.Discard)
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err=%v, want ErrValidation", err)
			}

			if len(signer.signed) != 0 {
				t.Error("signer ran before refusal")
			}

			if readFile(t, "safe/checksums") != "old checksums" || readFile(t, "safe/sboms.zip") != "old zip" {
				t.Error("output changed before refusal")
			}
		})
	}
}

func TestAssemble_RejectsExternalSourcesAndLinkedStagingParents(t *testing.T) {
	for _, field := range []string{"release inputs", "SBOM inputs", "staging parent"} {
		t.Run(field, func(t *testing.T) {
			t.Chdir(t.TempDir())
			outside := t.TempDir()
			canary := filepath.Join(outside, "staged", "assets", "keep")
			mustWrite(t, canary, "unchanged")
			in := apprelease.AssembleInput{ProjectName: "app", Version: "1.0.0", ConfigPlanJSON: releaseAssemblyConfigJSON(t), ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t)}

			switch field {
			case "release inputs":
				in.ReleaseArtifactsDir = outside
			case "SBOM inputs":
				in.SBOMDir = outside
			case "staging parent":
				if err := os.Symlink(outside, "linked"); err != nil {
					t.Fatal(err)
				}

				in.ReleaseFilesDir = "linked/staged"
			}

			if _, err := apprelease.Assemble(io.Discard, in); !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err=%v, want ErrValidation", err)
			}

			if readFile(t, canary) != "unchanged" {
				t.Error("outside staging state changed")
			}
		})
	}
}
