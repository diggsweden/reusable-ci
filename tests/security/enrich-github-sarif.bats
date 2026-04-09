#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup
  export SARIF_FILE="$TEST_DIR/results.sarif"
}

teardown() {
  common_teardown
}

@test "adds partialFingerprints when missing" {
  create_test_file_heredoc "$SARIF_FILE" <<'EOF'
{
  "version": "2.1.0",
  "runs": [
    {
      "results": [
        {
          "ruleId": "rules.test",
          "fingerprints": {
            "matchBasedId/v1": "stable-fingerprint"
          },
          "message": {
            "text": "test finding"
          },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "src/app.js"
                },
                "region": {
                  "startLine": 10
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
EOF

  run_script "security/enrich-github-sarif.sh"

  assert_success
  assert_file_contains "$SARIF_FILE" '"partialFingerprints"'
  assert_file_contains "$SARIF_FILE" '"primaryLocationLineHash": "stable-fingerprint"'
}

@test "skips enrichment when SARIF file is missing" {
  export SARIF_FILE="$TEST_DIR/missing.sarif"

  run_script "security/enrich-github-sarif.sh"

  assert_success
  assert_stderr_contains "skipping GitHub enrichment"
}
