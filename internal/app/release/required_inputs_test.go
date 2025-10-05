// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// advertisingSigner advertises the given extensions and records every call.
type advertisingSigner struct {
	extensions []string
	signErr    error
	signed     []string
}

func (s *advertisingSigner) Extensions() []string { return s.extensions }

func (s *advertisingSigner) SignFile(_ context.Context, file string) error {
	s.signed = append(s.signed, file)

	if s.signErr != nil {
		return s.signErr
	}

	return os.WriteFile(file+s.extensions[0], []byte("sig"), 0o600)
}

var errSignerBackend = errors.New("signer backend refused") //nolint:err113 // fixture sentinel.

// TestGPGSignPackages_EachRequiredInputIsRefusedAlone removes one required
// input at a time from an otherwise valid request over a real package: each is
// a usage error, and the signer is never asked to import, list or sign.
func TestGPGSignPackages_EachRequiredInputIsRefusedAlone(t *testing.T) {
	valid := apprelease.GPGSignPackagesInput{PrivateKey: testPGPKeyArmor, Fingerprint: "ABCDEF", Passphrase: "secret"}

	for name, mutate := range map[string]func(*apprelease.GPGSignPackagesInput){
		"private key": func(in *apprelease.GPGSignPackagesInput) { in.PrivateKey = "" },
		"fingerprint": func(in *apprelease.GPGSignPackagesInput) { in.Fingerprint = "" },
		"passphrase":  func(in *apprelease.GPGSignPackagesInput) { in.Passphrase = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustWrite(t, "dist/app.deb", "package")

			in := valid
			mutate(&in)

			signer := &fakeGPGPackageSigner{listed: "fpr:::::::::ABCDEF:"}

			_, err := apprelease.GPGSignPackages(context.Background(), signer, &bytes.Buffer{}, in)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			if signer.imports != 0 || signer.lists != 0 || len(signer.signed) != 0 {
				t.Errorf("signer used for a refused request: %+v", signer)
			}
		})
	}

	t.Run("nil signer", func(t *testing.T) {
		if _, err := apprelease.GPGSignPackages(context.Background(), nil, &bytes.Buffer{}, valid); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})
}

// TestSigners_MissingOrMisadvertisingSignerIsRefusedBeforeAnyEffect gives each
// signing entry point a missing signer or one whose advertised sidecar
// extensions are unusable. SignArtifacts and a signed SBOM ZIP from discovery
// or from an assembly refuse before signing anything, and the ZIP is not
// written: a signed SBOM ZIP with no signer used to write the ZIP and then
// refuse it as a validation failure.
func TestSigners_MissingOrMisadvertisingSignerIsRefusedBeforeAnyEffect(t *testing.T) {
	signers := map[string]struct {
		signer apprelease.Signer
		want   error
	}{
		"no signer":             {nil, errs.ErrUsage},
		"no extension":          {&advertisingSigner{}, errs.ErrInvalidConfig},
		"extension without dot": {&advertisingSigner{extensions: []string{"asc"}}, errs.ErrInvalidConfig},
		"bare dot":              {&advertisingSigner{extensions: []string{"."}}, errs.ErrInvalidConfig},
		"path separator":        {&advertisingSigner{extensions: []string{".asc/../x"}}, errs.ErrInvalidConfig},
		"line break":            {&advertisingSigner{extensions: []string{".asc\n"}}, errs.ErrInvalidConfig},
		"duplicate":             {&advertisingSigner{extensions: []string{".asc", ".asc"}}, errs.ErrInvalidConfig},
	}

	entries := map[string]func(signer apprelease.Signer) error{
		"sign artifacts": func(signer apprelease.Signer) error {
			return apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{Files: []string{"dist/app.tgz"}})
		},
		"sbom zip from discovery": func(signer apprelease.Signer) error {
			_, err := apprelease.CreateSBOMZip(context.Background(), signer, apprelease.SBOMZipInput{ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: true}, &bytes.Buffer{})

			return err
		},
		"sbom zip from assembly": func(signer apprelease.Signer) error {
			_, err := apprelease.CreateSBOMZip(context.Background(), signer, apprelease.SBOMZipInput{AssemblyFile: "assembly.json", SignArtifacts: true}, &bytes.Buffer{})

			return err
		},
	}

	for entry, run := range entries {
		for name, tc := range signers {
			t.Run(entry+"/"+name, func(t *testing.T) {
				t.Chdir(t.TempDir())
				mustWrite(t, "dist/app.tgz", "asset")
				mustWrite(t, "myapp-pom-sbom.spdx.json", "{}")
				mustWrite(t, "assembly.json", "not read before the signer is checked")

				if err := run(tc.signer); !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}

				if recorder, ok := tc.signer.(*advertisingSigner); ok && len(recorder.signed) != 0 {
					t.Errorf("signed %v with an unusable signer", recorder.signed)
				}

				for _, zip := range []string{"myapp-1.2.3-sboms.zip", "unknown-sboms.zip"} {
					if _, err := os.Lstat(zip); !os.IsNotExist(err) {
						t.Errorf("%s written for a refused signing request (%v)", zip, err)
					}
				}
			})
		}
	}
}

// TestSigners_BackendFailureKeepsItsCause checks the downstream diagnostic: a
// signer backend refusal reaches the caller as the backend's own error from
// both SignArtifacts and a signed SBOM ZIP, rather than as a missing sidecar.
func TestSigners_BackendFailureKeepsItsCause(t *testing.T) {
	t.Run("sign artifacts", func(t *testing.T) {
		t.Chdir(t.TempDir())
		mustWrite(t, "dist/app.tgz", "asset")

		signer := &advertisingSigner{extensions: []string{".asc"}, signErr: errSignerBackend}

		err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{Files: []string{"dist/app.tgz"}, SkipReleaseArtifactsDir: true, SkipChecksumsFile: true})
		if !errors.Is(err, errSignerBackend) || errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want the backend error alone", err)
		}
	})

	t.Run("sbom zip", func(t *testing.T) {
		t.Chdir(t.TempDir())
		mustWrite(t, "myapp-pom-sbom.spdx.json", "{}")

		signer := &advertisingSigner{extensions: []string{".asc"}, signErr: errSignerBackend}

		res, err := apprelease.CreateSBOMZip(context.Background(), signer, apprelease.SBOMZipInput{ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: true}, &bytes.Buffer{})
		if !errors.Is(err, errSignerBackend) || res == nil || res.Signed {
			t.Fatalf("result = %+v err = %v, want an unsigned result carrying the backend error", res, err)
		}
	})
}

// TestPublishRelease_NilPublisherIsRefusedBeforeOutput refuses a missing
// publisher as a usage error before anything is printed; it used to print the
// publishing line and panic.
func TestPublishRelease_NilPublisherIsRefusedBeforeOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "dist/release-notes.md", "notes\n")
	mustWrite(t, "dist/asset.tgz", "asset\n")

	var out bytes.Buffer

	err := apprelease.PublishRelease(context.Background(), nil, &out, apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"dist/asset.tgz"}})
	if !errors.Is(err, errs.ErrUsage) || out.Len() != 0 {
		t.Fatalf("err = %v out = %q, want ErrUsage and no output", err, out.String())
	}

	for name, in := range map[string]apprelease.PublishReleaseInput{
		"blank repository": {Tag: "v1.2.3", Repository: " ", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"dist/asset.tgz"}},
		"blank notes file": {Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "\t", Assets: []string{"dist/asset.tgz"}},
	} {
		t.Run(name, func(t *testing.T) {
			prov := fakeprovider.New(t)
			if err := apprelease.PublishRelease(context.Background(), prov, &bytes.Buffer{}, in); !errors.Is(err, errs.ErrUsage) || len(prov.PublishReleaseCalls()) != 0 {
				t.Fatalf("err = %v calls = %d, want ErrUsage and no provider call", err, len(prov.PublishReleaseCalls()))
			}
		})
	}
}

// TestNewCosignSigner_NilAdapterIsRefused completes the constructor's refusals.
func TestNewCosignSigner_NilAdapterIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := apprelease.NewCosignSigner(nil, apprelease.CosignSignerInput{Method: domainrelease.SignMethodSigstore}, nil); !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}
