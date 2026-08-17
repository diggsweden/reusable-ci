# Example: stage-result aggregation on GitLab CI

How reusable-ci aggregates a multi-job stage's outcome on GitLab — the GitLab
counterpart of GitHub feeding `toJson(needs)` to `report stage-result`. The
aggregation itself is the *same* shared command on every forge; only how each
job's outcome reaches it differs, because **a GitLab job cannot read a sibling
job's status**.

## The flow

1. **Each job records its own outcome.** In `after_script` — where
   `$CI_JOB_STATUS` holds the final `success`/`failed`/`canceled` — the job runs:

   ```sh
   reusable-ci report job-result --name "$CI_JOB_NAME" --status "$CI_JOB_STATUS"
   ```

   which writes `$CI_RESULTS_DIR/jobs/<job>.json`. The job keeps `.ci-results/jobs/`
   as an artifact (`when: always`, so failed jobs still publish their record).

2. **The summary job aggregates.** It `needs:` the quality jobs (so GitLab pulls
   their record artifacts into the workspace) and runs:

   ```sh
   reusable-ci report stage-result   # collects $CI_RESULTS_DIR/jobs/*.json
   ```

   With no `--result` and no `--job-results`, `stage-result` collects the per-job
   records and aggregates them against `STAGE_PLAN_JSON` into
   `<stage>-result.json`.

## Why this matches GitHub without forking the logic

`report stage-result` runs one shared aggregator (`ResolveTargetResults`) with one
fail-closed rule (`NormalizeJobStatus`). Two *capability-shaped* input adapters
feed it:

| Forge | How sibling outcomes are obtained | What feeds `stage-result` |
|-------|-----------------------------------|---------------------------|
| GitHub / Forgejo | children are `uses:` jobs → `needs.<job>.result` | `--job-results` ← `toJson(needs)` |
| GitLab | no sibling access → each job self-records | collected `jobs/*.json` records |

So this is **not** a GitLab-only construction: it's one of the two native feeds
into the shared core. A job that crashes or is cancelled before its
`after_script` leaves no record; against the plan that target is **fail-closed to
failure**, so the stage cannot be declared a success behind a missing job.

## Rendering a panel

`stage-result` writes the typed `<stage>-result.json` manifest. The
human-readable panel comes from `report pr` / `report release`, which read that
manifest — the same way the GitHub orchestrators do. Add such a step to the
summary job to surface the panel via `$CI_SUMMARY_FILE`.

## Relation to the PR example

[`examples/gitlab/pullrequest/`](../pullrequest/) wires the PR quality
gate (the pipeline status is the gate). This example adds the **aggregated
summary** on top of that pattern; a real PR pipeline combines the two.
