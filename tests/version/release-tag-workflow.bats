#!/usr/bin/env bats
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"

@test "release workflow captures tag state before changes and preserves checkout history" {
  run python3 - "${BATS_TEST_DIRNAME}/../../.github/workflows/version-bump.yml" <<'PY'
import json
import subprocess
import sys

workflow = json.loads(subprocess.check_output(['yq', '-o=json', '.', sys.argv[1]], text=True))
steps = workflow['jobs']['bump-version']['steps']
checkout = next(step for step in steps if step.get('id') == 'checkout-release')
preflight = next(step for step in steps if '/validate/tag-commit.sh' in step.get('run', ''))
push = next(step for step in steps if step.get('uses', '').startswith('stefanzweifel/git-auto-commit-action@'))
move = next(step for step in steps if '/version/move-tag.sh' in step.get('run', ''))

assert checkout['with']['fetch-depth'] == 0
assert push['with']['skip_fetch'] is True
assert push['with']['skip_checkout'] is True
assert not push['with'].get('push_options'), 'Branch pushes must not be forced'
assert not push['with'].get('tag_name'), 'The explicit tag move owns tagging'
assert steps.index(checkout) < steps.index(preflight) < steps.index(push) < steps.index(move)
for step in steps:
    if '/version/bump-version.sh' in step.get('run', ''):
        assert steps.index(preflight) < steps.index(step)
assert preflight['env']['CHECKOUT_SHA'] == '${{ steps.checkout-release.outputs.commit }}'
assert preflight['env']['TAG_NAME'] == move['env']['TAG_NAME'] == '${{ github.ref_name }}'
assert move['env']['RELEASE_BASE_SHA'] == '${{ steps.' + preflight['id'] + '.outputs.release-base-sha }}'
assert move['env']['RELEASE_TAG_OBJECT'] == '${{ steps.' + preflight['id'] + '.outputs.release-tag-object }}'
assert '"$TAG_NAME" "$RELEASE_BASE_SHA" "$RELEASE_TAG_OBJECT"' in move['run']
print('Verified preflight ordering, captured state, explicit tag and full-history preservation.')
PY
  assert_success
}
