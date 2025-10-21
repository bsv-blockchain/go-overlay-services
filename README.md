# 🚀 Overlay Services

This standalone HTTP server provides a customizable interface for interacting with [**Overlay Services**](https://docs.google.com/document/d/1zxGol7X4Zdb599oTg8zIK-lQOiZQgQadOIXvkSDKEfc/edit?pli=1&tab=t.0) built on top of the Bitcoin SV blockchain.

## 📋 Table of Contents

- [✨ Features](#features)
- [🔧 Middleware & Built-in Components](#middleware--built-in-components)
- [🛠️ Supported API Endpoints](#supported-api-endpoints)
- [⚙️ Configuration](#configuration)
  - [⚙️ Default Configuration](#default-configuration)
  - [🧩 Server Options](#server-options)
- [🛠️ Development Task Automation](#development-task-automation)
  - [🔑 Available Tasks](#available-tasks)
  - [💡 Usage Examples](#usage-examples)
- [📚 Code Snippet Examples](#code-snippet-examples)
- [🤝 Support & Contacts](#support--contacts)
- [📜 License](#license)

## Features

- **Standalone HTTP Server**
  Operates as a self-contained server with customizable configuration and overlay engine layers.

- **📖 OpenAPI Integration**
  Supports OpenAPI specifications with an interactive Swagger UI for exploring and testing endpoints.

- **🗂️ Flexible Configuration Formats**
  Allows importing and exporting configuration using common formats such as `.env`, `.yaml`, and `.json`.

- **📊 Real-Time Observability**
  Provides basic real-time observability and performance monitoring out of the box.

## Middleware & Built-in Components

- **🔎 Request Tracing**
  Attaches a unique `request ID` to every incoming request for consistent traceability across logs and systems.

- **🔄 Idempotency Support**
  Enables safe request retries by ensuring idempotent behavior for designated endpoints.

- **🌐 CORS Handling**
  Manages cross-origin resource sharing (CORS) to support web applications securely.

- **🚨 Panic Recovery**
  Catches and logs panics during request handling, with optional stack trace support.

- **📝 Structured Request Logging**
  Logs HTTP requests using a customizable format, including method, path, status, and errors.

- **❤️ Health Check Endpoint**
  Exposes an endpoint for health and readiness checks, suitable for orchestration tools.

- **📈 Performance Profiling**
  Integrates `pprof` profiling tools under the `/api/v1` path for runtime diagnostics.

- **📦 Request Body Limits**
  Enforces size limits on `application/octet-stream` payloads to protect against abuse.

- **🔐 Bearer Token Authorization**
  Validates Bearer tokens found in the `Authorization` header of incoming HTTP requests and enforces authorization based on OpenAPI security scopes.

## Supported API Endpoints

| HTTP Method | Endpoint                                      | Description                                           | Protection          |
|-------------|-----------------------------------------------|-------------------------------------------------------|---------------------|
| POST        | `/api/v1/admin/startGASPSync`                 | Starts GASP synchronization                           | **Admin only**      |
| POST        | `/api/v1/admin/syncAdvertisements`            | Synchronizes advertisements                           | **Admin only**      |
| GET         | `/api/v1/getDocumentationForLookupServiceProvider` | Retrieves documentation for Lookup Service Providers | Public              |
| GET         | `/api/v1/getDocumentationForTopicManager`     | Retrieves documentation for Topic Managers            | Public              |
| GET         | `/api/v1/listLookupServiceProviders`          | Lists all Lookup Service Providers                    | Public              |
| GET         | `/api/v1/listTopicManagers`                   | Lists all Topic Managers                              | Public              |
| POST        | `/api/v1/lookup`                              | Submits a lookup question                             | Public              |
| POST        | `/api/v1/requestForeignGASPNode`              | Requests a foreign GASP node                          | Public              |
| POST        | `/api/v1/requestSyncResponse`                 | Requests a synchronization response                   | Public              |
| POST        | `/api/v1/submit`                              | Submits a transaction                                 | Public              |
| POST        | `/api/v1/arc-ingest`                          | Ingests a Merkle proof                                | **ARC callback token** |

## Configuration

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

### Default Configuration

A default configuration, `DefaultConfig`, is provided for local development and testing, with sensible defaults for all fields.

### Server Options

The HTTP server supports flexible setup via functional options (`ServerOption`), allowing customization during server creation:

| Option                                | Description                                                                                       |
|--------------------------------------|---------------------------------------------------------------------------------------------------|
| `WithMiddleware(fiber.Handler)`      | Adds a Fiber middleware handler to the server's middleware stack.                                |
| `WithEngine(engine.OverlayEngineProvider)` | Sets the overlay engine provider that handles business logic in the server.                 |
| `WithAdminBearerToken(string)`       | Overrides the default admin bearer token securing admin routes.                                  |
| `WithOctetStreamLimit(int64)`        | Sets a custom limit on octet-stream request body sizes to control memory usage.                   |
| `WithARCCallbackToken(string)`       | Sets the ARC callback token used to authenticate ARC callback requests on the HTTP server.        |
| `WithARCAPIKey(string)`              | Sets the ARC API key used for ARC service integration.                                            |
| `WithConfig(Config)`                 | Applies a full configuration struct to initialize the Fiber app with specified settings.          |

## Development Task Automation

This project uses a dedicated **Taskfile.yml** powered by the [`task`](https://taskfile.dev/) CLI to automate common workflows. This centralizes critical operations such as testing, code generation, API documentation bundling, and code linting into a single, easy-to-use interface.

Formalizing these processes ensures:

- ✅ **Consistency** across developer environments
- ⚙️ **Automation** of chained commands and validations
- ⏱️ **Efficiency** by reducing manual complexity
- 🔁 **Reproducibility** in CI/CD and local setups
- 🧹 **Maintainability** with centralized workflow updates

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

### Usage Examples

- Run all unit tests: ```task execute-unit-tests```

### Code Snippet Examples

All the proposed examples are available in the [examples directory](./examples/).


## Support & Contacts

For questions, bug reports, or feature requests, please open an issue on GitHub.

## License

The license for the code in this repository is the Open BSV License. Refer to [LICENSE.txt](./LICENSE) for the license text.
Thank you for being a part of the BSV Blockchain Libraries Project. Let's build the future of BSV Blockchain together! 🚀🔥
