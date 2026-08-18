// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeManifestPushTool struct {
	pushes    []string
	failures  int
	digest    string
	remove    bool
	tlsVerify bool
}

func (f *fakeManifestPushTool) PushManifestToRefWithDigest(_ context.Context, authFile string, tlsVerify bool, manifest, destination string, remove bool, _ io.Writer) (string, error) {
	f.pushes = append(f.pushes, authFile+"|"+manifest+"|"+destination)
	f.remove = remove

	f.tlsVerify = tlsVerify
	if f.failures > 0 {
		f.failures--

		return "", errors.New("temporary push failure") //nolint:err113 // test double error.
	}

	return f.digest, nil
}

type fakeManifestPushRegistry struct {
	raw      []byte
	failures int
}

func (f *fakeManifestPushRegistry) Manifest(_ context.Context, _ string) ([]byte, error) {
	if f.failures > 0 {
		f.failures--

		return nil, errors.New("temporary registry read failure") //nolint:err113 // test double error.
	}

	return f.raw, nil
}

// TestPushManifest_ReportsTheDigestTheRegistryHolds is the manifest counterpart
// to TestPushImage_ReportsTheDigestTheRegistryHolds, with the local manifest
// removed afterwards.
func TestPushManifest_ReportsTheDigestTheRegistryHolds(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)
	tool := &fakeManifestPushTool{failures: 1, digest: digest}
	registry := &fakeManifestPushRegistry{failures: 1, raw: raw}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.PushManifest(context.Background(), tool, registry, sink, &log, appcontainer.PushManifestInput{
		LocalManifest: "localhost/app:candidate",
		Destination:   "registry.example/app:staging-v1",
		AuthFile:      "auth.json",
		TLSVerify:     "true",
		RemoveLocal:   true,
		RetryAttempts: 2,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest {
		t.Errorf("digest = %q, want the digest of the manifest the registry returned %q", got.Digest, digest)
	}

	if want := "registry.example/app:staging-v1@" + digest; got.Ref != want {
		t.Errorf("ref = %q, want %q", got.Ref, want)
	}

	if v := sink.Single("digest"); v != digest {
		t.Errorf("output digest = %q, want %q", v, digest)
	}

	if v := sink.Single("ref"); v != got.Ref {
		t.Errorf("output ref = %q, want it to match the returned ref %q", v, got.Ref)
	}

	// The double records auth file, source and destination; counting the
	// pushes said nothing about where they went or what they carried.
	if want := []string{"auth.json|localhost/app:candidate|registry.example/app:staging-v1", "auth.json|localhost/app:candidate|registry.example/app:staging-v1"}; !reflect.DeepEqual(tool.pushes, want) {
		t.Errorf("pushes = %v, want the same push retried once: %v", tool.pushes, want)
	}

	if !tool.tlsVerify {
		t.Error("pushed without TLS verification")
	}

	if !tool.remove {
		t.Error("local manifest was not removed after the push")
	}

	if !strings.Contains(log.String(), "attempt 1/2") {
		t.Fatalf("log missing retry message: %q", log.String())
	}
}

func TestPushManifest_RejectsDigestMismatch(t *testing.T) {
	registry := &fakeManifestPushRegistry{raw: []byte(`{"schemaVersion":2}`)}

	_, err := appcontainer.PushManifest(context.Background(), &fakeManifestPushTool{digest: "sha256:" + strings.Repeat("0", 64)}, registry, fakeoutputsink.New(t), io.Discard, appcontainer.PushManifestInput{
		LocalManifest: "localhost/app:candidate",
		Destination:   "registry.example/app:staging-v1",
		TLSVerify:     "true",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestPushManifest_UsesRegistryDigestWhenDigestFileInvalid(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)

	var log bytes.Buffer

	got, err := appcontainer.PushManifest(context.Background(), &fakeManifestPushTool{digest: ""}, &fakeManifestPushRegistry{raw: raw}, fakeoutputsink.New(t), &log, appcontainer.PushManifestInput{
		LocalManifest: "localhost/app:candidate",
		Destination:   "registry.example/app:staging-v1",
		TLSVerify:     "true",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest {
		t.Fatalf("digest = %q, want %q", got.Digest, digest)
	}

	if !strings.Contains(log.String(), "digest file was empty or invalid") {
		t.Fatalf("log missing invalid digestfile message: %q", log.String())
	}
}

// TestResolvePushedManifestDigest_EmitsOutputsAndRetries covers what the
// resolution publishes and that a registry read is retried. The digest file
// rule itself is covered by
// TestResolvePushedManifestDigest_TreatsTheDigestFileAsAnExpectation.
func TestResolvePushedManifestDigest_EmitsOutputsAndRetries(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)

	digestFile := t.TempDir() + "/manifest.digest"
	if err := os.WriteFile(digestFile, []byte(digest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := &fakeManifestPushRegistry{failures: 1, raw: raw}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.ResolvePushedManifestDigest(context.Background(), registry, sink, &log, appcontainer.PushedManifestDigestInput{
		Ref:           "registry.example/app:staging-v1",
		DigestFile:    digestFile,
		RetryAttempts: 2,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest || got.Ref != "registry.example/app:staging-v1@"+digest {
		t.Fatalf("output = %+v", got)
	}

	if sink.Single("digest") != digest || sink.Single("ref") != got.Ref {
		t.Fatalf("sink digest=%q ref=%q", sink.Single("digest"), sink.Single("ref"))
	}

	if !strings.Contains(log.String(), "attempt 1/2") {
		t.Fatalf("log missing retry message: %q", log.String())
	}
}

// TestResolvePushedManifestDigest_TreatsTheDigestFileAsAnExpectation covers the
// four states the digest file can be in. The digest returned is always the one
// computed from the manifest the registry serves; the file is a cross-check on
// it, not the source.
//
// The quiet case was missing. warnOnInvalidExpected exists to tell "no digest
// file was configured" from "one was configured and could not be used", and
// only the second was tested -- a flag left permanently on would print "digest
// file was empty or invalid" on every ordinary run, which nothing would catch.
func TestResolvePushedManifestDigest_TreatsTheDigestFileAsAnExpectation(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	registryDigest := manifestTestDigest(raw)

	for name, testCase := range map[string]struct {
		fileBody string // "" means no digest file is configured at all
		wantErr  error
		wantWarn bool
	}{
		"no digest file configured":       {fileBody: "", wantWarn: false},
		"digest file agrees":              {fileBody: registryDigest + "\n", wantWarn: false},
		"digest file is not a digest":     {fileBody: "not-a-digest", wantWarn: true},
		"digest file names another image": {fileBody: "sha256:" + strings.Repeat("0", 64), wantErr: errs.ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			in := appcontainer.PushedManifestDigestInput{Ref: "registry.example/app:staging-v1"}

			if testCase.fileBody != "" {
				in.DigestFile = filepath.Join(t.TempDir(), "manifest.digest")
				if err := os.WriteFile(in.DigestFile, []byte(testCase.fileBody), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var log bytes.Buffer

			got, err := appcontainer.ResolvePushedManifestDigest(context.Background(),
				&fakeManifestPushRegistry{raw: raw}, fakeoutputsink.New(t), &log, in)

			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("err = %v, want %v", err, testCase.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Digest != registryDigest {
				t.Errorf("digest = %q, want the registry's %q", got.Digest, registryDigest)
			}

			const warning = "digest file was empty or invalid"
			if warned := strings.Contains(log.String(), warning); warned != testCase.wantWarn {
				t.Errorf("warned = %v, want %v; log = %q", warned, testCase.wantWarn, log.String())
			}
		})
	}
}

func manifestTestDigest(raw []byte) string {
	sum := sha256.Sum256(raw)

	return "sha256:" + hex.EncodeToString(sum[:])
}
