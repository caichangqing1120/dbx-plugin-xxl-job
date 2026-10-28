# MCP Upgrade Validation

This is the initial implementation-stage snapshot. The subsequently authorized release-stage validation is recorded in `release-validation.md`; historical no-push/no-publication statements below describe the initial stage.

Date: 2026-10-28. Scope: `xxljob` only; source baseline `ebbc62c` (detached worktree). Spec: `spec.md`; implementation progress and exact inventory: `plan.md`.

## Delivered Contract

- Plugin `io.github.caichangqing1120.xxljob`, publisher `caichangqing1120`, active version **0.2.0** in manifest, backend initialize metadata, npm package and lock root, and package verifier. Existing engine floor unchanged; updated matching DBX Desktop/standalone MCP protocol verified against upstream `8c2bbe35b` is required, not inferred from a numeric release mapping.
- Exactly **13 tools**: **6 read, 2 write, 5 full**, split into `observe`, `scheduling`, `admin`. Manifest and discovery names/access/hints match. AI/external contribution flags are enabled, but no user permission or grant is altered.
- Flat typed JSON schemas and strict runtime allowlists prevent arbitrary methods/paths, nested raw forms, credentials, identity and policy overrides. Every mutation requires explicit `confirmed: true`, current lifecycle read-only checks, and original handler validation/authorization. Lifecycle fingerprint includes credentials/config/runtime/read-only; cached sessions never replace UI sessions.
- Dedicated serialized 32-entry MCP cache supports saved connections and log provenance. Same-identity host reconnect/disconnect releases its cache without touching unrelated identities. Unknown outcomes are never replayed automatically. Existing transport timeouts/4 MiB body limit remain; MCP request/result ceiling is 1 MiB. Results are JSON text in CallToolResult, error outcomes set `isError: true`.
- Existing backend executor/user/log permissions are retained; the dedicated cache preserves log-list provenance. Login passwords clear after authentication and are not stored in cache; credential/config/runtime changes invalidate authentication. Cookie values are redacted including rotation; expired sessions do not silently reauthenticate.
- Public active `.dbx-store.json` and generated `dist/release-candidates.json` set `mcpTools: true`; historical release notes/receipts are untouched. Public Admin 2.3 and 3.4 paths, pagination, inserts/deletes and plural `ids[]` wire forms are fixture-tested.

## Commands And Results

Environment: Go 1.26.3 (`darwin/amd64`), Node 22.22.3, npm 10.9.8; installed packaging CLI `dbx-plugin 0.1.9`. The native package target is `darwin-arm64` (the Node host architecture). No globally installed dependency was changed.

| Command / check | Result |
|---|---|
| Baseline `(cd backend && go test ./...)` | PASS before bridge edits |
| Baseline `npm ci --ignore-scripts --no-audit --no-fund && npm test` | PASS, 10 frontend tests |
| New failing `go test ./...` before implementation | Expected RED: unsupported `mcp/tools` / `mcp/call`, malformed error envelope |
| `(cd backend && go test -count=1 ./...)` | PASS, includes 20 MCP test functions and all prior backend tests |
| `(cd backend && go test -race -count=1 ./...)` | PASS |
| `(cd backend && go vet ./...)` | PASS |
| `(cd backend/sdk && go test -count=1 ./... && go test -race -count=1 ./... && go vet ./...)` | PASS, vendored SDK unchanged |
| `npm test` | PASS, 10 tests |
| `npm run build` | PASS, TypeScript/Vue checking and single-file Vite production build |
| `npm run package:all` | PASS, 6 unsigned packages: darwin/windows/linux x arm64/x64 |
| `go run scripts/verify-package.go dist/io.github.caichangqing1120.xxljob-0.2.0-darwin-arm64.dbxp --handshake` | PASS, native protocol1 initialize ID/version0.2.0 |
| Exact upstream `manifest.schema.json` / JSON Schema Draft202012 validation | PASS, source manifest and all 6 shipped manifests |
| Native packaged sidecar stdio initialize -> mcp/tools -> invalid-lifecycle mcp/call | PASS, initialize0.2.0, 13 exact tools, portable valid schemas, proper error result |
| `git diff --check` | PASS |

Package verification covers architecture (Mach-O/PE/ELF), exact archive file allowlist, executable permissions/entrypoint, per-file SHA256, artifact checksum/size and metadata. Package output stays in ignored `dist/`; raw command logs stay outside tracked documentation.

## Behavioral Evidence

20 MCP test functions cover side-effect-free discovery, exact manifest parity/access/hints, all 13 tools and save create/update paths, saved-connection execution, malformed JSON and argument types, missing/mismatched IDs, readonly at both lifecycle policy locations after a writable cache exists, explicit confirmation, no invalid-call network effects, UI session isolation, configuration/credential/runtime fingerprint changes as applicable, bounded cache eviction and concurrent calls, redacted success/error content, oversized result denial, unknown-write outcome/no replay and host-disconnect cleanup.

Observed RED-to-GREEN refinements (in addition to the missing bridge): cache retained lifecycle password (XXL), `access_key` leaked, jobGroup schema minima were too broad, rotated cookie leaked (XXL), host disconnect retained matching cache, Store listing omitted MCP badge. These failures were reproduced before their production fixes. Full baseline and new suites are green after fixes.

## Limits And Non-Actions

- Only local synthetic HTTP fixtures were used. No real credentials, production services, actual scheduling or real service write was performed. Fixture writes verify request dispatch, not live business acceptance.
- No commit, push, publish, Store PR, plugin install, DBX restart, grants change or browser session was performed. Worktrees and uncommitted source/evidence are preserved.
- All six package targets pass static verification; only the native packaged sidecar ran. Other operating systems, desktop UI/AI/external MCP end-to-end acceptance, service-specific deployment compatibility and sustained sessions remain unverified.
- Secret redaction covers known host passwords, session cookies, sensitive JSON keys and common labelled/Bearer forms; arbitrary unlabeled application log secrets cannot be identified reliably. Outputs remain untrusted data. Query narrowly and do not request sensitive logs.
- The MCP cache serializes calls and is bounded, intentionally prioritizing permission/log-provenance safety over concurrent throughput. Cache eviction or lifecycle changes may require re-querying logs. Oversized output errors do not undo completed mutations.

## Independent Review Fix

- A reviewer reproduced credentials leaking from JSON-string job parameters, quoted log labels and URLs (including encoded sensitive query names). Public success/error-boundary regressions failed before the fix. The adapter now recursively redacts embedded JSON objects/arrays, masks quoted credential labels, removes URL userinfo and compares decoded sensitive query names. Fresh full-suite/race/vet and regenerated candidates validate the fix; no second independent review is claimed. Opaque unlabeled application secrets remain outside a universal DLP guarantee.
