# XXL-JOB 0.2.1 Store Compatibility Validation

Date: 2026-10-29. Scope: public author release metadata compatibility only. Reuses this MCP work package; the 0.2.0 release notes and original validation receipts remain unchanged.

## Actual Contracts And RED/GREEN

Reference: `dbx-store` commit `5b8ab0045086a9838c35b730c5a37df02632f870`, `scripts/sync-release-candidate.mjs`, `scripts/validate.mjs` and `schemas/plugin-candidate.schema.json`.

The author release envelope and Store catalog candidate are different formats. The synchronizer reads `release.plugin` and `release.artifacts`; listing metadata has an allowlist that excludes `mcpTools`. The previous generator instead emitted a flat Store-shaped object and active listing metadata included that unsupported badge.

- Baseline: existing frontend (10 tests/1 files) and backend suites passed before production edits.
- RED: `DBX_STORE_ROOT=/path/to/dbx-store node --test scripts/test-release-contract.mjs` failed both focused cases against the unchanged production generator/listing. The actual importer reported `release-candidates.json has no plugin identity` and, independently with a hand-authored valid release envelope, `Unsupported store metadata field 'mcpTools'`.
- GREEN: the same regression passed after changing the generator to `plugin` plus `artifacts` and removing unsupported listing metadata. No external candidate-shape conversion is used.
- The regression stubs only the costly compiler/ZIP subprocesses and remote JSON fetch. It executes the real package script and unchanged Store importer/validator in temporary workspaces. The separate real-package case imports generated `dist/release-candidates.json` unchanged and checks each target against actual local package SHA256 and size.
- Final real-package regression: 3 passed, 0 failed, 0 skipped.

## Full Local Verification

All commands completed successfully on the final production source, with `GOMAXPROCS=2 GOFLAGS=-p=2`. Dependencies were installed using the existing lockfile via `npm ci --ignore-scripts --no-audit --no-fund`.

| Command/check | Result |
| --- | --- |
| `npm test` | PASS: 10 frontend tests, 1 test files |
| `go -C backend test -count=1 -v ./...` | PASS: 59 top-level tests, 101 including subtests |
| `go -C backend test -count=1 -race ./...` | PASS |
| `go -C backend vet ./...` | PASS |
| `go -C backend/sdk test -count=1 ./...` | PASS: 3 SDK tests |
| `go -C backend/sdk test -count=1 -race ./...` | PASS |
| `go -C backend/sdk vet ./...` | PASS |
| `npm run package:all` | PASS: Vue/TypeScript production build plus six independent target packages and verifier checks |
| `go run scripts/verify-package.go dist/io.github.caichangqing1120.xxljob-0.2.1-darwin-arm64.dbxp --handshake` | PASS: native packaged initialize ID/version `0.2.1`, protocol 1 |
| `DBX_STORE_ROOT=/path/to/dbx-store DBX_RELEASE_CANDIDATE=dist/release-candidates.json npm run test:release` | PASS: 3 tests, no skips; actual sync and Store candidate validation |
| Actual Store JSON schema, Ajv2020 | PASS: synchronized candidate with six targets; Store validator separately enforces HTTPS URLs |
| Current DBX host manifest schema on source and six archived manifests | PASS: all seven schema validations; all 13 MCP declarations and access levels unchanged |
| `shasum -a 256 -c SHA256SUMS-v0.2.1.txt` in `dist/` | PASS: six artifact checksums |
| `git diff --check` | PASS |

Each package verifier checks archive entries, target executable path, embedded binary architecture, internal checksums, whole-package digest and metadata size. Targets: `darwin-arm64`, `darwin-x64`, `windows-arm64`, `windows-x64`, `linux-arm64`, `linux-x64`.

The manifest differs from the preceding source only in version. Backend runtime code differs only in the initialize-version constant. All MCP service behavior, APIs, credentials handling, UI code and user grants remain unchanged. The obsolete metadata-badge-only Go assertion in XXL-JOB is replaced by the actual Store consumer integration tests; manifest/discovery parity coverage remains.

## Outputs And Boundaries

- Author release envelope: `dist/release-candidates.json` with `plugin.version: "0.2.1"` and six `artifacts`.
- Packages: `dist/io.github.caichangqing1120.xxljob-0.2.1-<target>.dbxp`, six corresponding `.artifact.json` files, and `dist/SHA256SUMS-v0.2.1.txt`.
- Test logs and disposable Store import/schema evidence are maintained outside the repository; they are not release assets or source dependencies.

No real services, credentials, DBX profile/configuration, grant mutations, installation, signing, commits or remote writes were used by this worker. Only the native macOS ARM64 sidecar was executed; other targets are cross-built and architecture-checked, not OS runtime acceptance. This verifies local author metadata/package compatibility, not published-asset availability, official Store signing or desktop/production acceptance.
