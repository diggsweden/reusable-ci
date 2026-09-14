// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// forgePackageVersion carries no build tag so the fixture below runs in every
// test run, not only inside an authorized lab run.
func forgePackageVersion(now time.Time) string { return fmt.Sprintf("0.0.%d", now.UnixMilli()) }

func TestPackageVersionFixture_DoesNotWrap(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1700000123456)
	require.Equal(t, "0.0.1700000123456", forgePackageVersion(start))
	require.Equal(t, "0.0.1700001123456", forgePackageVersion(start.Add(1000*time.Second)))
}
