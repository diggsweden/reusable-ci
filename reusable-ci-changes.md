# Required changes in diggsweden/reusable-ci

File: `.github/workflows/build-gradle-app.yml`

## 1. Full git history for version code computation

Change `fetch-depth` from `1` to `0` in the checkout step so that
`git rev-list --count origin/main` returns the correct commit count.

```yaml
- name: Checkout repository
  uses: actions/checkout@...
  with:
    ref: ${{ inputs.branch }}
    fetch-depth: 0
```

## 2. Decode secrets.properties

Add a decode step before "Run Gradle build". This follows the same pattern
already used for the Android keystore. If the caller has stored a
base64-encoded `secrets.properties` as the `SECRETS_PROPERTIES` repo secret,
it will be decoded and written to the working directory before Gradle runs.
Gradle reads it automatically — no secret names need to be hardcoded here.

```yaml
- name: Decode secrets properties
  working-directory: ${{ inputs['working-directory'] }}
  env:
    SECRETS_PROPERTIES: ${{ secrets.SECRETS_PROPERTIES }}
  run: |
    if [ -n "$SECRETS_PROPERTIES" ]; then
      printf "%s" "$SECRETS_PROPERTIES" | base64 -d > secrets.properties
    fi
```

## Caller setup

Generate the secret from your local `secrets.properties` and store it as
`SECRETS_PROPERTIES` in the GitHub repo settings:

```sh
base64 -i secrets.properties | pbcopy
```
