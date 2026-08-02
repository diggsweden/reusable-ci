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

func TestPushManifest_VerifiesRegistryDigestAndEmitsOutputs(t *testing.T) {
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

	if got.Digest != digest || got.Ref != "registry.example/app:staging-v1@"+digest {
		t.Fatalf("output = %+v", got)
	}

	if sink.Single("digest") != digest || sink.Single("ref") != got.Ref {
		t.Fatalf("sink digest=%q ref=%q", sink.Single("digest"), sink.Single("ref"))
	}

	if len(tool.pushes) != 2 {
		t.Fatalf("pushes = %v, want retry", tool.pushes)
	}

	if !tool.remove || !tool.tlsVerify {
		t.Fatalf("remove=%v tls=%v, want true true", tool.remove, tool.tlsVerify)
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

func TestResolvePushedManifestDigest_VerifiesDigestFileAndEmitsOutputs(t *testing.T) {
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

func TestResolvePushedManifestDigest_RejectsDigestMismatch(t *testing.T) {
	digestFile := t.TempDir() + "/manifest.digest"
	if err := os.WriteFile(digestFile, []byte("sha256:"+strings.Repeat("0", 64)), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := appcontainer.ResolvePushedManifestDigest(context.Background(), &fakeManifestPushRegistry{raw: []byte(`{"schemaVersion":2}`)}, fakeoutputsink.New(t), io.Discard, appcontainer.PushedManifestDigestInput{
		Ref:        "registry.example/app:staging-v1",
		DigestFile: digestFile,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestResolvePushedManifestDigest_UsesRegistryDigestWhenDigestFileInvalid(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)

	digestFile := t.TempDir() + "/manifest.digest"
	if err := os.WriteFile(digestFile, []byte("not-a-digest"), 0o600); err != nil {
		t.Fatal(err)
	}

	var log bytes.Buffer

	got, err := appcontainer.ResolvePushedManifestDigest(context.Background(), &fakeManifestPushRegistry{raw: raw}, fakeoutputsink.New(t), &log, appcontainer.PushedManifestDigestInput{
		Ref:        "registry.example/app:staging-v1",
		DigestFile: digestFile,
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

func manifestTestDigest(raw []byte) string {
	sum := sha256.Sum256(raw)

	return "sha256:" + hex.EncodeToString(sum[:])
}
