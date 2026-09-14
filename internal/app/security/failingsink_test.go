// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"context"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// failingSink is a recording sink whose Set fails for one key, after
// recording nothing for it. Every other key is recorded as usual, so a test
// can see which outputs were written before the failure and that none came
// after.
type failingSink struct {
	*fakeoutputsink.Sink

	failKey string
	err     error
}

func (f *failingSink) Set(ctx context.Context, key, value string) error {
	if key == f.failKey {
		return f.err
	}

	return f.Sink.Set(ctx, key, value)
}
