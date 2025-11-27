<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Workflow Architecture and Patterns

## Pull Request Workflow Architecture

```mermaid
graph TD
    A[Pull Request Created/Updated] --> B[pullrequest-orchestrator.yml]
    B --> C[commit-lint]
    B --> D[license-lint]
    B --> E[dependency-review]
    B --> F[megalint]
    B --> G[publiccode-lint]
    B --> J[just-mise-lint]
    C --> H[lint-status]
    D --> H
    E --> H
    F --> H
    G --> H
    J --> H
    H --> I[Project test.yml]

    style A fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style B fill:#e69875,stroke:#9d5c41,color:#2b3339
    style H fill:#83c092,stroke:#5c856a,color:#2b3339
    style I fill:#d699b6,stroke:#93647c,color:#2b3339
```

### Release Workflow Architecture

```mermaid
graph TD
    A[Tag Push: v*.*.* ] --> B[release-orchestrator.yml]
    B --> C[Parse artifacts.yml]
    C --> D[release-prerequisites.yml]
    D --> E[version-bump.yml - Matrix]

    E --> F[build-maven.yml - Matrix]
    E --> G[build-npm.yml - Matrix]
    E --> H[build-gradle.yml - Matrix]

    F --> I[publish-github.yml - Matrix]
    G --> I
    H --> I

    F --> J[publish-maven-central.yml - Matrix]

    I --> K[publish-container.yml - Matrix]

    K --> L[release-github.yml]
    J --> L

    L --> M[Release Summary]

    style A fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style B fill:#e69875,stroke:#9d5c41,color:#2b3339
    style C fill:#e67e80,stroke:#9d4f50,color:#2b3339
    style D fill:#e67e80,stroke:#9d4f50,color:#2b3339
    style E fill:#d699b6,stroke:#93647c,color:#2b3339
    style F fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style G fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style H fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style I fill:#83c092,stroke:#5c856a,color:#2b3339
    style J fill:#83c092,stroke:#5c856a,color:#2b3339
    style K fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style L fill:#83c092,stroke:#5c856a,color:#2b3339
    style M fill:#7fbbb3,stroke:#5a8a82,color:#2b3339
```

### Component Interaction Flow

```mermaid
graph LR
    A[Project Workflow] --> B[Orchestrator]
    B --> C[Validator]
    B --> D[Publisher]
    B --> E[Builder]
    B --> F[Release Creator]

    C -.validates.-> G[(Secrets)]
    C -.checks.-> H[(Version)]

    D -.uploads.-> I[(Maven Central)]
    D -.uploads.-> J[(GitHub Packages)]
    D -.uploads.-> K[(NPM Registry)]

    E -.builds.-> L[(Container Image)]
    E -.generates.-> M[(SBOM)]
    E -.scans.-> N[(Vulnerabilities)]

    F -.creates.-> O[(GitHub Release)]
    F -.signs.-> P[(GPG Signatures)]

    style B fill:#e69875,stroke:#9d5c41,color:#2b3339
    style C fill:#e67e80,stroke:#9d4f50,color:#2b3339
    style D fill:#83c092,stroke:#5c856a,color:#2b3339
    style E fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style F fill:#d699b6,stroke:#93647c,color:#2b3339
```

### Workflow Execution Patterns

#### Pattern 1: Maven Library

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Maven]
    D --> E[Publish to Maven Central]
    E --> F[GitHub Release Created]

    style A fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style D fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style E fill:#83c092,stroke:#5c856a,color:#2b3339
    style F fill:#d699b6,stroke:#93647c,color:#2b3339
```

#### Pattern 2: Maven/NPM Application with Container

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Maven/NPM]
    D --> E[Publish to GitHub]
    E --> F[Build Container]
    F --> G[Create GitHub Release]

    style A fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style D fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style E fill:#83c092,stroke:#5c856a,color:#2b3339
    style F fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style G fill:#d699b6,stroke:#93647c,color:#2b3339
```

#### Pattern 3: Multi-Registry Publishing

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Once]
    D --> E1[Publish to GitHub]
    D --> E2[Publish to Maven Central]
    E1 --> F[Create Release]
    E2 --> F

    style A fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style D fill:#a7c080,stroke:#5c6a4a,color:#2b3339
    style E1 fill:#83c092,stroke:#5c856a,color:#2b3339
    style E2 fill:#83c092,stroke:#5c856a,color:#2b3339
    style F fill:#d699b6,stroke:#93647c,color:#2b3339
```

---
