# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.4.3] - 2025-12-12

### Added

- Add experimental play store support
- Add bats shellcheck exceptions
- Add initial bats test suite
- Add initial bats test suite

### Fixed

- Dont break on printf

## [2.4.2] - 2025-12-08

### Added

- Add default exclude lint jobs
- Add more artifact search paths for sbom

### Changed

- Pass lint
- Improve justfile


## [2.4.1] - 2025-12-01

### Changed

- Dont add extra target to mvn artifact
- Set profile value


## [2.4.0] - 2025-12-01

### Added

- Add self and meta jobs for release
- Add idiomatic package check

### Changed

- Use devbase-justkit and fix lints
- Improve npm ci
- Set xconfig version
- Set license header to recommended style
- Rename build and publish pipes
- Extract bash scripts
- Improve bash code
- Improve bash code
- Improve headers
- Improve release maintenace
- Echo to printf
- Improve inline bash reading
- Dedupe mvn opts
- Standardsize scripts checkout
- Set default to main where was v1
- Pin versions of subscripts
- Clean up status scripts
- Update dependency anchore/syft to v1.38.0 (#60)
- Update mise tools (#59)
- Update github actions (#58)

### Fixed

- Dont run pr on commit to main
- Dont add empty checksum to release
- Pass artifact name to sbom zip
- Use maven name for jar artifact
- No container sbom try for libs
- Propagate summary env var
- Clear up ternary expressions
- Default java 25 and node 24
- Separate artifact from container
- Correct references branch
- Improve token handling
- Improve script strucutres
- Improve sbom generation
- Upload maven dev artifacts
- Minor optimizations
- Set githistory full only when needed
- Bump to latest lts for node and jvm

### Removed

- Remove e for debug
- Remove leading dashs printfs
- Remove token refactor residue
- Delete existing release draft


## [2.3.8] - 2025-11-25

### Changed

- Improve artifact naming

## [2.3.7] - 2025-11-25

### Changed

- Set macos-26 runner as default for xcode

## [2.3.6] - 2025-11-25

### Fixed

- Skip package plugin validation during archive

## [2.3.5] - 2025-11-25

### Changed

- Restore destination

## [2.3.4] - 2025-11-25

### Removed

- Remove archive dest

## [2.3.3] - 2025-11-25

### Fixed

- Dont enable skiptest for ios build arc

## [2.3.2] - 2025-11-25

### Added

- Add tag check for relase flow
- Add ios tool bump agvtool support

### Changed

- Update mise tools (#55)
- Update github actions (#54)

## [2.3.1] - 2025-11-20

### Added

- Add initial support for ios

### Fixed

- Fix swift lint
- Fix swift lint

## [2.3.0] - 2025-11-19

### Changed

- Expose android keystore path as env variable

## [2.2.5] - 2025-11-18

### Added

- Add env vars check to mise install

### Changed

- Update mise tools (#49)
- Update github actions (#48)
- Update docker/metadata-action action to v5.9.0 (#46)
- Update dependency anchore/syft to v1.37.0 (#45)
- Update dependency ubi:rvben/rumdl to v0.0.171 (#47)

### Fixed

- Update auto find poms
- Correct git glob pom handle

## [2.2.4] - 2025-11-10

### Added

- Add mise rate limit token, reuse install

### Changed

- Update github actions (#44)
- Update dependency ubi:rvben/rumdl to v0.0.170 (#43)

## [2.2.3] - 2025-11-06

### Fixed

- Build npm tarball name

## [2.2.2] - 2025-11-06

### Fixed

- Fix npm assets upload
- Fix github packages name

## [2.2.1] - 2025-11-05

### Changed

- Adjust default mvn pom deploy

## [2.2.0] - 2025-11-05

### Added

- Add mvn module support

### Changed

- Update mise tools (#39)
- Update github artifact actions
- Update github actions (#38)
- Update dependency anchore/syft to v1.36.0 (#37)

## [2.1.1] - 2025-10-31

### Changed

- Formatting + gitleaks ignore for examples

### Fixed

- Fix ref

## [2.1.0] - 2025-10-31

### Changed

- Handle linter metadata
- Improve shell target
- Improve just lint support
- Update mise tools (#35)
- Update github actions (#34)
- Update dependency anchore/syft to v1.34.2 (#33)
- Update github actions (#28)
- Update actions/setup-node action to v6
- Update dependency anchore/syft to v1.34.1 (#31)
- Update mise tools (#29)
- Use default branch

### Fixed

- Fix linting
- Always refer main

## [2.0.0] - 2025-10-19

### Changed

- Initial commit

[2.4.3]: https://github.com/diggsweden/reusable-ci/compare/v2.4.2..v2.4.3
[2.4.2]: https://github.com/diggsweden/reusable-ci/compare/v2.4.1..v2.4.2
[2.4.1]: https://github.com/diggsweden/reusable-ci/compare/v2.4.0..v2.4.1
[2.4.0]: https://github.com/diggsweden/reusable-ci/compare/v2.3.8..v2.4.0
[2.3.8]: https://github.com/diggsweden/reusable-ci/compare/v2.3.7..v2.3.8
[2.3.7]: https://github.com/diggsweden/reusable-ci/compare/v2.3.6..v2.3.7
[2.3.6]: https://github.com/diggsweden/reusable-ci/compare/v2.3.5..v2.3.6
[2.3.5]: https://github.com/diggsweden/reusable-ci/compare/v2.3.4..v2.3.5
[2.3.4]: https://github.com/diggsweden/reusable-ci/compare/v2.3.3..v2.3.4
[2.3.3]: https://github.com/diggsweden/reusable-ci/compare/v2.3.2..v2.3.3
[2.3.2]: https://github.com/diggsweden/reusable-ci/compare/v2.3.1..v2.3.2
[2.3.1]: https://github.com/diggsweden/reusable-ci/compare/v2.3.0..v2.3.1
[2.3.0]: https://github.com/diggsweden/reusable-ci/compare/v2.2.5..v2.3.0
[2.2.5]: https://github.com/diggsweden/reusable-ci/compare/v2.2.4..v2.2.5
[2.2.4]: https://github.com/diggsweden/reusable-ci/compare/v2.2.3..v2.2.4
[2.2.3]: https://github.com/diggsweden/reusable-ci/compare/v2.2.2..v2.2.3
[2.2.2]: https://github.com/diggsweden/reusable-ci/compare/v2.2.1..v2.2.2
[2.2.1]: https://github.com/diggsweden/reusable-ci/compare/v2.2.0..v2.2.1
[2.2.0]: https://github.com/diggsweden/reusable-ci/compare/v2.1.1..v2.2.0
[2.1.1]: https://github.com/diggsweden/reusable-ci/compare/v2.1.0..v2.1.1
[2.1.0]: https://github.com/diggsweden/reusable-ci/compare/v2.0.0..v2.1.0

<!-- generated by git-cliff -->
