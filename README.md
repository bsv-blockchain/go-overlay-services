<div align="center">

# 🌐&nbsp;&nbsp;go-overlay-services

**Custom HTTP server for interacting with [Overlay Services](https://docs.google.com/document/d/1zxGol7X4Zdb599oTg8zIK-lQOiZQgQadOIXvkSDKEfc/edit?pli=1&tab=t.0) on the Bitcoin SV blockchain.**

<br/>

<a href="https://github.com/bsv-blockchain/go-overlay-services/releases"><img src="https://img.shields.io/github/release-pre/bsv-blockchain/go-overlay-services?include_prereleases&style=flat-square&logo=github&color=black" alt="Release"></a>
<a href="https://golang.org/"><img src="https://img.shields.io/github/go-mod/go-version/bsv-blockchain/go-overlay-services?style=flat-square&logo=go&color=00ADD8" alt="Go Version"></a>
<a href="https://github.com/bsv-blockchain/go-overlay-services/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-OpenBSV-blue?style=flat-square" alt="License"></a>

<br/>

<table align="center" border="0">
  <tr>
    <td align="right">
       <code>CI / CD</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://github.com/bsv-blockchain/go-overlay-services/actions"><img src="https://img.shields.io/github/actions/workflow/status/bsv-blockchain/go-overlay-services/fortress.yml?branch=main&label=build&logo=github&style=flat-square" alt="Build"></a>
       <a href="https://github.com/bsv-blockchain/go-overlay-services/actions"><img src="https://img.shields.io/github/last-commit/bsv-blockchain/go-overlay-services?style=flat-square&logo=git&logoColor=white&label=last%20update" alt="Last Commit"></a>
    </td>
    <td align="right">
       &nbsp;&nbsp;&nbsp;&nbsp; <code>Quality</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://goreportcard.com/report/github.com/bsv-blockchain/go-overlay-services"><img src="https://goreportcard.com/badge/github.com/bsv-blockchain/go-overlay-services?style=flat-square" alt="Go Report"></a>
       <a href="https://codecov.io/gh/bsv-blockchain/go-overlay-services"><img src="https://codecov.io/gh/bsv-blockchain/go-overlay-services/branch/main/graph/badge.svg?style=flat-square" alt="Coverage"></a>
    </td>
  </tr>

  <tr>
    <td align="right">
       <code>Security</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://scorecard.dev/viewer/?uri=github.com/bsv-blockchain/go-overlay-services"><img src="https://api.scorecard.dev/projects/github.com/bsv-blockchain/go-overlay-services/badge?style=flat-square" alt="Scorecard"></a>
       <a href=".github/SECURITY.md"><img src="https://img.shields.io/badge/policy-active-success?style=flat-square&logo=security&logoColor=white" alt="Security"></a>
    </td>
    <td align="right">
       &nbsp;&nbsp;&nbsp;&nbsp; <code>Community</code> &nbsp;&nbsp;
    </td>
    <td align="left">
       <a href="https://github.com/bsv-blockchain/go-overlay-services/graphs/contributors"><img src="https://img.shields.io/github/contributors/bsv-blockchain/go-overlay-services?style=flat-square&color=orange" alt="Contributors"></a>
       <a href="https://github.com/sponsors/bsv-blockchain"><img src="https://img.shields.io/badge/sponsor-BSV-181717.svg?logo=github&style=flat-square" alt="Sponsor"></a>
    </td>
  </tr>
</table>

</div>

<br/>
<br/>

<div align="center">

### <code>Project Navigation</code>

</div>

<table align="center">
  <tr>
    <td align="center" width="33%">
       📦&nbsp;<a href="#-installation"><code>Installation</code></a>
    </td>
    <td align="center" width="33%">
       ✨&nbsp;<a href="#-features"><code>Features</code></a>
    </td>
    <td align="center" width="33%">
       📚&nbsp;<a href="#-documentation"><code>Documentation</code></a>
    </td>
  </tr>
  <tr>
    <td align="center">
       🧪&nbsp;<a href="#-examples--tests"><code>Examples&nbsp;&&nbsp;Tests</code></a>
    </td>
    <td align="center">
       ⚡&nbsp;<a href="#-benchmarks"><code>Benchmarks</code></a>
    </td>
    <td align="center">
       🛠️&nbsp;<a href="#-code-standards"><code>Code&nbsp;Standards</code></a>
    </td>
  </tr>
  <tr>
    <td align="center">
       🤖&nbsp;<a href="#-ai-usage--assistant-guidelines"><code>AI&nbsp;Usage</code></a>
    </td>
    <td align="center">
       🤝&nbsp;<a href="#-contributing"><code>Contributing</code></a>
    </td>
    <td align="center">
       👥&nbsp;<a href="#-maintainers"><code>Maintainers</code></a>
    </td>
  </tr>
  <tr>
    <td align="center" colspan="3">
       📝&nbsp;<a href="#-license"><code>License</code></a>
    </td>
  </tr>
</table>
<br/>

## ✨ Features

- **Standalone HTTP Server**
  Operates as a self-contained server with customizable configuration and overlay engine layers.

- **OpenAPI Integration**
  Supports OpenAPI specifications with an interactive Swagger UI for exploring and testing endpoints.

- **Flexible Configuration Formats**
  Allows importing and exporting configuration using common formats such as `.env`, `.yaml`, and `.json`.

- **Real-Time Observability**
  Provides basic real-time observability and performance monitoring out of the box.

<br>

### Middleware & Built-in Components

- **Request Tracing**
  Attaches a unique `request ID` to every incoming request for consistent traceability across logs and systems.

- **Idempotency Support**
  Enables safe request retries by ensuring idempotent behavior for designated endpoints.

- **CORS Handling**
  Manages cross-origin resource sharing (CORS) to support web applications securely.

- **Panic Recovery**
  Catches and logs panics during request handling, with optional stack trace support.

- **Structured Request Logging**
  Logs HTTP requests using a customizable format, including method, path, status, and errors.

- **Health Check Endpoint**
  Exposes an endpoint for health and readiness checks, suitable for orchestration tools.

- **Performance Profiling**
  Integrates `pprof` profiling tools under the `/api/v1` path for runtime diagnostics.

- **Request Body Limits**
  Enforces size limits on `application/octet-stream` payloads to protect against abuse.

- **Bearer Token Authorization**
  Validates Bearer tokens found in the `Authorization` header of incoming HTTP requests and enforces authorization based on OpenAPI security scopes.

<br>

## 📦 Installation

**go-overlay-services** requires a [supported release of Go](https://golang.org/doc/devel/release.html#policy).

<br>

### Running as a Standalone Server

1. **Clone the repository**
   ```shell
   git clone https://github.com/bsv-blockchain/go-overlay-services.git
   cd go-overlay-services
   ```

2. **Create a configuration file**
   ```shell
   cp app-config.example.yaml app-config.yaml
   ```
   Edit `app-config.yaml` to customize your server settings (port, tokens, etc.)

3. **Run the server**
   ```shell
   go run examples/srv/main.go -config app-config.yaml
   ```

4. **Optional: Build a binary**
   ```shell
   go build -o overlay-server examples/srv/main.go
   ./overlay-server -config app-config.yaml
   ```

The server will start on `http://localhost:3000` by default (or the port specified in your config).

<br>

### Using as a Library

To use **go-overlay-services** as a library in your own Go application:

```shell
go get -u github.com/bsv-blockchain/go-overlay-services
```

See the [examples](examples) directory for code examples showing how to:
- **[examples/srv](examples/srv/main.go)** - Run a server with configuration file
- **[examples/custom](examples/custom/main.go)** - Embed the server in your own application
- **[examples/config](examples/config/main.go)** - Generate configuration files programmatically

<br>

## 📚 Documentation

### Supported API Endpoints

| HTTP Method | Endpoint                                           | Description                                          | Protection             |
|-------------|----------------------------------------------------|------------------------------------------------------|------------------------|
| POST        | `/api/v1/admin/startGASPSync`                      | Starts GASP synchronization                          | **Admin only**         |
| POST        | `/api/v1/admin/syncAdvertisements`                 | Synchronizes advertisements                          | **Admin only**         |
| GET         | `/api/v1/getDocumentationForLookupServiceProvider` | Retrieves documentation for Lookup Service Providers | Public                 |
| GET         | `/api/v1/getDocumentationForTopicManager`          | Retrieves documentation for Topic Managers           | Public                 |
| GET         | `/api/v1/listLookupServiceProviders`               | Lists all Lookup Service Providers                   | Public                 |
| GET         | `/api/v1/listTopicManagers`                        | Lists all Topic Managers                             | Public                 |
| POST        | `/api/v1/lookup`                                   | Submits a lookup question                            | Public                 |
| POST        | `/api/v1/requestForeignGASPNode`                   | Requests a foreign GASP node                         | Public                 |
| POST        | `/api/v1/requestSyncResponse`                      | Requests a synchronization response                  | Public                 |
| POST        | `/api/v1/submit`                                   | Submits a transaction                                | Public                 |
| POST        | `/api/v1/arc-ingest`                               | Ingests a Merkle proof                               | **ARC callback token** |

<br>

### Configuration

The server configuration is encapsulated in the `Config` struct with the following fields:

| Field                   | Type            | Description                                                                                         | Default Value                    |
|-------------------------|-----------------|-----------------------------------------------------------------------------------------------------|----------------------------------|
| `AppName`               | `string`        | Name of the application shown in server metadata.                                                   | `"Overlay API v0.0.0"`           |
| `Port`                  | `int`           | TCP port number on which the server listens.                                                        | `3000`                           |
| `Addr`                  | `string`        | Network address the server binds to.                                                                | `"localhost"`                    |
| `ServerHeader`          | `string`        | Value sent in the `Server` HTTP response header.                                                    | `"Overlay API"`                  |
| `AdminBearerToken`      | `string`        | Bearer token required for authentication on admin-only routes.                                      | Random UUID generated by default |
| `OctetStreamLimit`      | `int64`         | Maximum allowed size in bytes for requests with `Content-Type: application/octet-stream`.           | `1GB` (1,073,741,824 bytes)      |
| `ConnectionReadTimeout` | `time.Duration` | Maximum duration to keep an open connection before forcefully closing it.                           | `10 seconds`                     |
| `ARCAPIKey`             | `string`        | API key for ARC service integration.                                                                | Empty string                     |
| `ARCCallbackToken`      | `string`        | Token for authenticating ARC callback requests.                                                     | Random UUID generated by default |

<br>

### Default Configuration

A default configuration, `DefaultConfig`, is provided for local development and testing, with sensible defaults for all fields.

<br>

### Server Options

The HTTP server supports flexible setup via functional options (`ServerOption`), allowing customization during server creation:

| Option                                     | Description                                                                                |
|--------------------------------------------|--------------------------------------------------------------------------------------------|
| `WithMiddleware(fiber.Handler)`            | Adds a Fiber middleware handler to the server's middleware stack.                          |
| `WithEngine(engine.OverlayEngineProvider)` | Sets the overlay engine provider that handles business logic in the server.                |
| `WithAdminBearerToken(string)`             | Overrides the default admin bearer token securing admin routes.                            |
| `WithOctetStreamLimit(int64)`              | Sets a custom limit on octet-stream request body sizes to control memory usage.            |
| `WithARCCallbackToken(string)`             | Sets the ARC callback token used to authenticate ARC callback requests on the HTTP server. |
| `WithARCAPIKey(string)`                    | Sets the ARC API key used for ARC service integration.                                     |
| `WithConfig(Config)`                       | Applies a full configuration struct to initialize the Fiber app with specified settings.   |

<br/>

<details>
<summary><strong><code>Development Build Commands</code></strong></summary>
<br/>

Get the [MAGE-X](https://github.com/mrz1836/mage-x) build tool for development:
```shell script
go install github.com/mrz1836/mage-x/cmd/magex@latest
```

View all build commands

```bash script
magex help
```

</details>

<details>
<summary><strong><code>Development Task Automation</code></strong></summary>
<br/>

This project uses a dedicated **Taskfile.yml** powered by the [`task`](https://taskfile.dev/) CLI to automate common workflows. This centralizes critical operations such as testing, code generation, API documentation bundling, and code linting into a single, easy-to-use interface.

Formalizing these processes ensures:

- **Consistency** across developer environments
- **Automation** of chained commands and validations
- **Efficiency** by reducing manual complexity
- **Reproducibility** in CI/CD and local setups
- **Maintainability** with centralized workflow updates

### Available Tasks

- **`execute-unit-tests`**
  Runs all unit tests with fail-fast, vet checks, and disables caching for fresh results.

- **`oapi-codegen`**
  Generates HTTP server code and models from the OpenAPI spec to keep the API and code in sync.

- **`swagger-doc-gen`**
  Bundles the OpenAPI spec into a single YAML file, ready for validation and documentation tools.

- **`swagger-ui-up`**
  Bundles, validates, and starts Swagger UI with Docker Compose for interactive API exploration.

- **`swagger-ui-down`**
  Stops Swagger UI services and cleans up containers.

- **`swagger-cleanup`**
  Removes generated Swagger files and stops any running Swagger UI containers.

- **`execute-linters`**
  Runs Go linters and applies automatic fixes to maintain code quality.

</details>

<details>
<summary><strong><code>Repository Features</code></strong></summary>
<br/>

* **Continuous Integration on Autopilot** with [GitHub Actions](https://github.com/features/actions) – every push is built, tested, and reported in minutes.
* **Pull‑Request Flow That Merges Itself** thanks to [auto‑merge](.github/workflows/auto-merge-on-approval.yml) and hands‑free [Dependabot auto‑merge](.github/workflows/dependabot-auto-merge.yml).
* **One‑Command Builds** powered by battle‑tested [MAGE-X](https://github.com/mrz1836/mage-x) targets for linting, testing, releases, and more.
* **First‑Class Dependency Management** using native [Go Modules](https://github.com/golang/go/wiki/Modules).
* **Uniform Code Style** via [gofumpt](https://github.com/mvdan/gofumpt) plus zero‑noise linting with [golangci‑lint](https://github.com/golangci/golangci-lint).
* **Confidence‑Boosting Tests** with [testify](https://github.com/stretchr/testify), the Go [race detector](https://blog.golang.org/race-detector), crystal‑clear [HTML coverage](https://blog.golang.org/cover) snapshots, and automatic uploads to [Codecov](https://codecov.io/).
* **Hands‑Free Releases** delivered by [GoReleaser](https://github.com/goreleaser/goreleaser) whenever you create a [new Tag](https://git-scm.com/book/en/v2/Git-Basics-Tagging).
* **Relentless Dependency & Vulnerability Scans** via [Dependabot](https://dependabot.com), [Nancy](https://github.com/sonatype-nexus-community/nancy) and [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
* **Security Posture by Default** with [CodeQL](https://docs.github.com/en/github/finding-security-vulnerabilities-and-errors-in-your-code/about-code-scanning), [OpenSSF Scorecard](https://openssf.org) and secret‑leak detection via [gitleaks](https://github.com/gitleaks/gitleaks).
* **Automatic Syndication** to [pkg.go.dev](https://pkg.go.dev/) on every release for instant godoc visibility.
* **Polished Community Experience** using rich templates for [Issues & PRs](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/configuring-issue-templates-for-your-repository).
* **All the Right Meta Files** (`LICENSE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SUPPORT.md`, `SECURITY.md`) pre‑filled and ready.
* **Code Ownership** clarified through a [CODEOWNERS](.github/CODEOWNERS) file, keeping reviews fast and focused.
* **Zero‑Noise Dev Environments** with tuned editor settings (`.editorconfig`) plus curated *ignore* files for [VS Code](.editorconfig), [Docker](.dockerignore), and [Git](.gitignore).
* **Label Sync Magic**: your repo labels stay in lock‑step with [.github/labels.yml](.github/labels.yml).
* **Friendly First PR Workflow** – newcomers get a warm welcome thanks to a dedicated [workflow](.github/workflows/pull-request-management.yml).
* **Standards‑Compliant Docs** adhering to the [standard‑readme](https://github.com/RichardLitt/standard-readme/blob/master/spec.md) spec.
* **Instant Cloud Workspaces** via [Gitpod](https://gitpod.io/) – spin up a fully configured dev environment with automatic linting and tests.
* **Out‑of‑the‑Box VS Code Happiness** with a preconfigured [Go](https://code.visualstudio.com/docs/languages/go) workspace and [`.vscode`](.vscode) folder with all the right settings.
* **Optional Release Broadcasts** to your community via [Slack](https://slack.com), [Discord](https://discord.com), or [Twitter](https://twitter.com) – plug in your webhook.
* **AI Playbook** – machine‑readable guidelines in [tech conventions](.github/tech-conventions/ai-compliance.md).
* **Go-Pre-commit System** - [High-performance Go-native pre-commit hooks](https://github.com/mrz1836/go-pre-commit) with 17x faster execution—run the same formatting, linting, and tests before every commit, just like CI.
* **Zero Python Dependencies** - Pure Go implementation with environment-based configuration via [.env.base](.github/.env.base).
* **DevContainers for Instant Onboarding** – Launch a ready-to-code environment in seconds with [VS Code DevContainers](https://containers.dev/) and the included [.devcontainer.json](.devcontainer.json) config.

</details>

<details>
<summary><strong><code>Library Deployment</code></strong></summary>
<br/>

This project uses [goreleaser](https://github.com/goreleaser/goreleaser) for streamlined binary and library deployment to GitHub. To get started, install it via:

```bash
brew install goreleaser
```

The release process is defined in the [.goreleaser.yml](.goreleaser.yml) configuration file.


Then create and push a new Git tag using:

```bash
magex version:bump push=true bump=patch branch=master
```

This process ensures consistent, repeatable releases with properly versioned artifacts and citation metadata.

</details>

<details>
<summary><strong><code>Pre-commit Hooks</code></strong></summary>
<br/>

Set up the Go-Pre-commit System to run the same formatting, linting, and tests defined in [AGENTS.md](.github/AGENTS.md) before every commit:

```bash
go install github.com/mrz1836/go-pre-commit/cmd/go-pre-commit@latest
go-pre-commit install
```

The system is configured via [.env.base](.github/.env.base) and can be customized using also using [.env.custom](.github/.env.custom) and provides 17x faster execution than traditional Python-based pre-commit hooks. See the [complete documentation](http://github.com/mrz1836/go-pre-commit) for details.

</details>

<details>
<summary><strong><code>GitHub Workflows</code></strong></summary>
<br/>

### 🎛️ The Workflow Control Center

All GitHub Actions workflows in this repository are powered by a single configuration files – your one-stop shop for tweaking CI/CD behavior without touching a single YAML file! 🎯

**Configuration Files:**
- **[.env.base](.github/.env.base)** – Default configuration that works for most Go projects
- **[.env.custom](.github/.env.custom)** – Optional project-specific overrides

This magical file controls everything from:
- **⚙️ Go version matrix** (test on multiple versions or just one)
- **🏃 Runner selection** (Ubuntu or macOS, your wallet decides)
- **🔬 Feature toggles** (coverage, fuzzing, linting, race detection, benchmarks)
- **🛡️ Security tool versions** (gitleaks, nancy, govulncheck)
- **🤖 Auto-merge behaviors** (how aggressive should the bots be?)
- **🏷️ PR management rules** (size labels, auto-assignment, welcome messages)

<br/>

| Workflow Name                                                                      | Description                                                                                                            |
|------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| [auto-merge-on-approval.yml](.github/workflows/auto-merge-on-approval.yml)         | Automatically merges PRs after approval and all required checks, following strict rules.                               |
| [codeql-analysis.yml](.github/workflows/codeql-analysis.yml)                       | Analyzes code for security vulnerabilities using [GitHub CodeQL](https://codeql.github.com/).                          |
| [dependabot-auto-merge.yml](.github/workflows/dependabot-auto-merge.yml)           | Automatically merges [Dependabot](https://github.com/dependabot) PRs that meet all requirements.                       |
| [fortress.yml](.github/workflows/fortress.yml)                                     | Runs the GoFortress security and testing workflow, including linting, testing, releasing, and vulnerability checks.    |
| [pull-request-management.yml](.github/workflows/pull-request-management.yml)       | Labels PRs by branch prefix, assigns a default user if none is assigned, and welcomes new contributors with a comment. |
| [scorecard.yml](.github/workflows/scorecard.yml)                                   | Runs [OpenSSF](https://openssf.org/) Scorecard to assess supply chain security.                                        |
| [stale.yml](.github/workflows/stale-check.yml)                                     | Warns about (and optionally closes) inactive issues and PRs on a schedule or manual trigger.                           |
| [sync-labels.yml](.github/workflows/sync-labels.yml)                               | Keeps GitHub labels in sync with the declarative manifest at [`.github/labels.yml`](./.github/labels.yml).             |

</details>

<details>
<summary><strong><code>Updating Dependencies</code></strong></summary>
<br/>

To update all dependencies (Go modules, linters, and related tools), run:

```bash
magex deps:update
```

This command ensures all dependencies are brought up to date in a single step, including Go modules and any tools managed by [MAGE-X](https://github.com/mrz1836/mage-x). It is the recommended way to keep your development environment and CI in sync with the latest versions.

</details>

<br/>

## 🧪 Examples & Tests

All unit tests and [examples](examples) run via [GitHub Actions](https://github.com/bsv-blockchain/go-overlay-services/actions) and use [Go version 1.24.x](https://go.dev/doc/go1.24). View the [configuration file](.github/workflows/fortress.yml).

Run all tests (fast):

```bash script
magex test
```

Run all tests with race detector (slower):
```bash script
magex test:race
```

<br/>

## ⚡ Benchmarks

Run the Go benchmarks:

```bash script
magex bench
```

<br/>

## 🛠️ Code Standards
Read more about this Go project's [code standards](.github/CODE_STANDARDS.md).

<br/>

## 🤖 AI Usage & Assistant Guidelines
Read the [AI Usage & Assistant Guidelines](.github/tech-conventions/ai-compliance.md) for details on how AI is used in this project and how to interact with AI assistants.

<br/>

## 👥 Maintainers
| [<img src="https://github.com/icellan.png" height="50" alt="Siggi" />](https://github.com/icellan) | [<img src="https://github.com/galt-tr.png" height="50" alt="Galt" />](https://github.com/galt-tr) | [<img src="https://github.com/mrz1836.png" height="50" alt="MrZ" />](https://github.com/mrz1836) |
|:--------------------------------------------------------------------------------------------------:|:-------------------------------------------------------------------------------------------------:|:------------------------------------------------------------------------------------------------:|
|                                [Siggi](https://github.com/icellan)                                 |                                [Dylan](https://github.com/galt-tr)                                 |                                [MrZ](https://github.com/mrz1836)                                 |

<br/>

## 🤝 Contributing
View the [contributing guidelines](.github/CONTRIBUTING.md) and please follow the [code of conduct](.github/CODE_OF_CONDUCT.md).

### How can I help?
All kinds of contributions are welcome :raised_hands:!
The most basic way to show your support is to star :star2: the project, or to raise issues :speech_balloon:.

[![Stars](https://img.shields.io/github/stars/bsv-blockchain/go-overlay-services?label=Please%20like%20us&style=social&v=1)](https://github.com/bsv-blockchain/go-overlay-services/stargazers)

<br/>

## 📝 License

[![License](https://img.shields.io/badge/license-OpenBSV-blue?style=flat&logo=springsecurity&logoColor=white)](LICENSE)
