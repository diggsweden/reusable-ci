#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

if [[ -z "${CERTIFICATE_BASE64:-}" ]]; then
  printf "::error::CERTIFICATE_BASE64 secret not found but enable-code-signing is true\n"
  exit 1
fi
if [[ -z "${PROVISIONING_PROFILE_BASE64:-}" ]]; then
  printf "::error::PROVISIONING_PROFILE_BASE64 secret not found but enable-code-signing is true\n"
  exit 1
fi
if [[ -z "${KEYCHAIN_PASSWORD:-}" ]]; then
  printf "::error::KEYCHAIN_PASSWORD secret not found but enable-code-signing is true\n"
  exit 1
fi

CERTIFICATE_PATH="$RUNNER_TEMP/certificate.p12"
PP_PATH="$RUNNER_TEMP/pp.mobileprovision"
KEYCHAIN_PATH="$RUNNER_TEMP/app-signing.keychain-db"

printf "%s" "$CERTIFICATE_BASE64" | base64 --decode -o "$CERTIFICATE_PATH"
printf "%s" "$PROVISIONING_PROFILE_BASE64" | base64 --decode -o "$PP_PATH"

security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"

security import "$CERTIFICATE_PATH" -P "${CERTIFICATE_PASSPHRASE:-}" -A -t cert -f pkcs12 -k "$KEYCHAIN_PATH"
security set-key-partition-list -S apple-tool:,apple: -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security list-keychain -d user -s "$KEYCHAIN_PATH"

mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
cp "$PP_PATH" ~/Library/MobileDevice/Provisioning\ Profiles

printf "✓ Code signing configured successfully\n"
