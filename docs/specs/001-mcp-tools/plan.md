# xxljob MCP Upgrade Implementation Plan

> Execute with the foundation handoff and Superpowers TDD. Keep all edits inside this isolated worktree; no commits, pushes or releases are implicit.

**Goal:** Expose all current service capabilities safely to the updated DBX MCP host and upgrade to 0.2.0.
**Architecture:** A dedicated `backend/mcp.go` tool registry and adapter calls existing service handlers, preserving their guards. The manifest declares the exact registry and maximum tool access. The UI and service API contracts are unchanged.
**Tech Stack:** Go sidecar; existing plugin-specific frontend and packaging tools.
**Spec:** `spec.md` in this package (user-request scope and existing upstream protocol).

## Global Constraints

All requirements in Spec apply. No remote writes, installation, restarts, grant mutations or credential use. Version 0.2.0; ID/publisher unchanged; manifest has at most 64 tool declarations. Keep detached HEAD and evidence.

## Review Focus

- A model selects a read-labelled wrapper then smuggles a mutation or alternate method/path.
- A stale writable session is reused for a current readonly lifecycle or a different connection identity.
- A sidecar starts during discovery and unexpectedly logs in or creates workloads.
- A generic form/body hides credentials or authorization-affecting values.
- Version bumps or packaging leave initialize metadata inconsistent with the shipped manifest.

## Task 1: Inventory And Bridge (backend/mcp.go, backend/mcp_test.go, backend/main.go)

- [x] Read existing dispatch tables, safety checks, tests and this Spec; record the full exact tool-name/method/access inventory here before implementation.
- [x] Run existing `go test ./...` in backend and applicable frontend baseline. Record unavailable dependencies/failures.
- [x] Write failing behavior tests exercising real dispatch through local HTTP fixtures, identity, readonly, confirmation, error envelopes, discovery parity and zero discovery effects. Run `go test ./...` and confirm expected missing-bridge failures.
- [x] Implement `mcp/tools` / `mcp/call` in backend/mcp.go and wire Handle in backend/main.go. Reuse existing APIs and guards; no arbitrary RPC forwarding. Include explicit portable parameter schemas. Record any existing capability that cannot safely be exposed.
- [x] Run focused tests, then `go test ./...`, `go test -race ./...`, and `go vet ./...`. Expected: all pass.

## Task 2: Declarations, Version And Delivery Evidence (manifest.json, active build metadata, README.md)

- [x] Test exact tool-name parity and max-access mapping between discovery and manifest; then add the mcp contribution and meaningful toolsets.
- [x] Bump active version metadata to 0.2.0 without rewriting historical evidence. Extend README with access levels and user enablement instructions.
- [x] Run the applicable complete frontend tests/build and existing package checks. Verify packaged manifest and sidecar initialize metadata agree.
- [x] Record commands, results, full inventory, limitations and risks in validation.md in this package. Mark completed steps in this plan. Keep the uncommitted diff for review.

## Exact Inventory (Recorded Before Implementation)

13 tools: 6 read, 2 write, 5 full. Tool names and access are fixed below; forms use only the existing handler whitelist, never an arbitrary method/path. `save_job` is full because it can configure task execution/child jobs; `trigger` is full because it executes existing tasks; executor management is administrative.

| Tool | Existing handler | Access | Service endpoint |
|---|---|---|---|
| `xxljob_info` | `xxljob/info` | read | Local session metadata (XXL refreshes access via GET /) |
| `xxljob_groups` | `xxljob/groups` | read | XXL: GET / plus POST /jobgroup/pageList; BD: GET /jobgroup |
| `xxljob_jobs` | `xxljob/jobs` | read | POST /jobinfo/pageList |
| `xxljob_logs` | `xxljob/logs` | read | POST /joblog/pageList |
| `xxljob_log_content` | `xxljob/logContent` | read | POST /joblog/logDetailCat |
| `xxljob_jobs_by_group` | `xxljob/jobsByGroup` | read | POST /joblog/getJobsByGroup; public 3.4: POST /jobinfo/pageList |
| `xxljob_start` | `xxljob/start` | write | POST /jobinfo/start |
| `xxljob_stop` | `xxljob/stop` | write | POST /jobinfo/stop |
| `xxljob_trigger` | `xxljob/trigger` | full | POST /jobinfo/trigger (existing task execution) |
| `xxljob_save_job` | `xxljob/saveJob` | full | POST /jobinfo/add or /jobinfo/update; public 3.4 create: /jobinfo/insert |
| `xxljob_remove_job` | `xxljob/removeJob` | full | POST /jobinfo/remove; public 3.4: /jobinfo/delete |
| `xxljob_save_group` | `xxljob/saveGroup` | full | POST /jobgroup/save or /jobgroup/update; public 3.4 create: /jobgroup/insert |
| `xxljob_remove_group` | `xxljob/removeGroup` | full | POST /jobgroup/remove; public 3.4: /jobgroup/delete |

Lifecycle calls use a separate bounded cache, not UI sessions. Fingerprints include the complete host lifecycle (including credentials, config, read-only flags and runtime). Preserve cached log provenance across related calls; never auto-retry expired or unknown-outcome operations. Reject unknown form fields and malformed arguments before authentication. Connection management is deliberately not exposed.

Pre-flight: Task 1 registry names, schemas and access are the sole input to Task 2 manifest parity tests; no conflicting shared interfaces. Existing spec approval and implementation authorization are inherited from the user-request scope. The canonical plan/spec are this work package; no legacy helper ledger is reused. This plan records execution progress directly; helper writes/commits/cleanup are not used.

## Execution Record

- Baseline backend and frontend suites passed before implementation; dependencies installed locally with `npm ci --ignore-scripts --no-audit --no-fund`.
- RED observed before production bridge: `mcp/tools` and `mcp/call` returned unsupported-operation errors, malformed requests lacked CallToolResult envelopes.
- Additional RED-to-GREEN cycles covered lifecycle-password retention, labelled/access-key output redaction, rotated cookies (XXL), exact group schema minima, host-disconnect cache cleanup, public active Store tools metadata.
- Registry/manifest parity, typed schemas, lifecycle saved-connection behavior, all existing dispatch endpoints and create/update branches verified. Existing UI handlers and authentication/authorization/confirmation paths remain in use.
- Full command evidence and scope limitations are recorded in `validation.md`; no commits, pushes, installation, releases, restarts, credential use or grants changes performed.


## Store Compatibility Patch 0.2.1

**Spec:** `spec.md`, Store Compatibility Patch 0.2.1 (2026-10-29). Existing historical upgrade tasks remain intact. The user-approved corrective patch is executed only in the current isolated worktree; commits/publication are handled separately.

- [x] Confirm clean detached baseline and passing existing frontend/backend suites.
- [x] Add `scripts/test-release-contract.mjs`: run the active generator with compiler-only fixture subprocesses, import its unmodified JSON through actual Store sync, and independently test active listing metadata. Observe both failures before implementation.
- [x] Correct `scripts/package.mjs` author envelope and `.dbx-store.json` unsupported metadata; bump active versions and update version-aware tests/verifier.
- [x] Run focused regression GREEN; complete frontend, backend, SDK, race, vet and production build checks. Build/verify all six targets with existing scripts and handshake the native sidecar.
- [x] Import the real generated release without conversion using actual Store sync/validator, validate its schema and checksum/size parity; record patch-specific repo-relative notes and validation. No remote writes from this execution worker.

Review focus: flat author-vs-Store candidate confusion, unsupported metadata fields, altered MCP contributions, missing architecture assets, and stale initialize/package versions. The regression exercises the real importer/validator rather than reproducing the output builder's expectations. No execution helpers or legacy ledgers are used.
