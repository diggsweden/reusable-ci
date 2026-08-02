// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeImagePushTool struct {
	pushes    []string
	failures  int
	digest    string
	tlsVerify bool
}

func (f *fakeImagePushTool) PushImageToRefWithDigest(_ context.Context, authFile string, tlsVerify bool, image, destination string, _ io.Writer) (string, error) {
	f.pushes = append(f.pushes, authFile+"|"+image+"|"+destination)

	f.tlsVerify = tlsVerify
	if f.failures > 0 {
		f.failures--

		return "", errors.New("temporary push failure") //nolint:err113 // test double error.
	}

	return f.digest, nil
}

func TestPushImage_VerifiesRegistryDigestAndEmitsOutputs(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)
	tool := &fakeImagePushTool{failures: 1, digest: digest}
	registry := &fakeManifestPushRegistry{failures: 1, raw: raw}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.PushImage(context.Background(), tool, registry, sink, &log, appcontainer.PushImageInput{
		LocalImage:    "localhost/app:arch",
		Destination:   "registry.example/app:staging-amd64",
		AuthFile:      "auth.json",
		TLSVerify:     "true",
		RetryAttempts: 2,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest || got.Ref != "registry.example/app:staging-amd64@"+digest {
		t.Fatalf("output = %+v", got)
	}

	if sink.Single("digest") != digest || sink.Single("ref") != got.Ref {
		t.Fatalf("sink digest=%q ref=%q", sink.Single("digest"), sink.Single("ref"))
	}

	if len(tool.pushes) != 2 {
		t.Fatalf("pushes = %v, want retry", tool.pushes)
	}

	if !tool.tlsVerify {
		t.Fatalf("tls=%v, want true", tool.tlsVerify)
	}

	if !strings.Contains(log.String(), "attempt 1/2") {
		t.Fatalf("log missing retry message: %q", log.String())
	}
}

func TestPushImage_RejectsDigestMismatch(t *testing.T) {
	registry := &fakeManifestPushRegistry{raw: []byte(`{"schemaVersion":2}`)}

	_, err := appcontainer.PushImage(context.Background(), &fakeImagePushTool{digest: "sha256:" + strings.Repeat("0", 64)}, registry, fakeoutputsink.New(t), io.Discard, appcontainer.PushImageInput{
		LocalImage:  "localhost/app:arch",
		Destination: "registry.example/app:staging-amd64",
		TLSVerify:   "true",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestPushImage_UsesRegistryDigestWhenDigestFileInvalid(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)

	var log bytes.Buffer

	got, err := appcontainer.PushImage(context.Background(), &fakeImagePushTool{digest: ""}, &fakeManifestPushRegistry{raw: raw}, fakeoutputsink.New(t), &log, appcontainer.PushImageInput{
		LocalImage:  "localhost/app:arch",
		Destination: "registry.example/app:staging-amd64",
		TLSVerify:   "true",
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
