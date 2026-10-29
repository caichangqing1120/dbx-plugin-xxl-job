# XXL-JOB for DBX

DBX workbench for official XXL-JOB Admin. Provides separate `2.3.x` and `3.4.x` API profiles; choose the Admin version when creating a connection. These profiles were checked against official `2.3.0` and `3.4.2` source respectively. The latest official release checked for this project is `3.4.2` (June 19, 2026). Other releases and customized distributions are not assumed compatible without testing.

This is a separate public plugin (`io.github.caichangqing1120.xxljob`), not the internal plugin. It does not require direct database access or modify the XXL-JOB server.

Current plugin version: `0.2.2`.

## 0.2.2 Store Update And CI Provenance

This is the first Store update since **0.1.1**. Neither **0.2.0** nor **0.2.1**
entered the Store, so the change is the 13-tool lifecycle-bound MCP bridge over the
existing XXL-JOB Admin API, not only a metadata fix. Read tools are `xxljob_info`,
`xxljob_groups`, `xxljob_jobs`, `xxljob_jobs_by_group`, `xxljob_logs` and
`xxljob_log_content`; write tools are `xxljob_start` and `xxljob_stop`; full-access
administration tools are `xxljob_trigger`, `xxljob_save_job`, `xxljob_remove_job`,
`xxljob_save_group` and `xxljob_remove_group`. Existing confirmation and read-only
guards remain enforced. Manifest permissions remain empty; upgrading grants no
access. Matching updated DBX Desktop and standalone MCP builds are required.
UI behavior is unchanged.

The release also corrects the author metadata envelope and removes the unsupported
`mcpTools` Store field. Source is pinned to
[`v0.2.2`](https://github.com/caichangqing1120/dbx-plugin-xxl-job/tree/v0.2.2).
Clean CI using the official reusable workflow and mandatory full VCS provenance
verification by the release helper must precede publication and Store resubmission.
Those gates are pending; these source changes do not establish a successful CI run,
verified release bytes or Store signing. See [0.2.2 notes](docs/releases/v0.2.2.md).
Earlier versioned notes, validation receipts, tags and assets remain unchanged.

## Connect

Requires DBX `0.6.14` or newer, Host API `1`. Enter the Admin IP/domain, port, username and password. New connections select `3.4.x` and `/` by default, matching the official 3.4.2 configuration. For stock `2.3.x`, select that version and change the application path to `/xxl-job-admin`. If the Admin is deployed at a custom path, enter that path instead. HTTPS and DBX SSH/proxy/HTTP tunnels are supported. HTTP transmits the login password in clear text: use it only on a trusted private network.

The plugin authenticates to the selected Admin version and keeps the session cookie in memory. Redirects are never followed; expired sessions require reconnecting. Existing connections without a version selector retain the legacy `2.3.x` profile. For a new connection, the default is `3.4.x`.

- View, filter and paginate jobs and scheduling logs; inspect execution logs.
- Create/update BEAN jobs, start/stop/trigger/delete a job after confirmation.
- Admin users can manage executor groups. Non-admin users see only their authorized groups; writes are checked again by the backend.
- Read-only connections prevent all changes. Ambiguous write results are not retried.

The version profiles differ: `2.3.x` uses `/login`, legacy flat pagination and singular `id`; `3.4.x` uses `/auth/doLogin`, `Response<PageModel>` and `ids[]` for some writes. The plugin never probes a write endpoint to infer a version. An unsupported version is rejected before login. Version selection is not a guarantee for forks or untested deployments.

## Build and verify

Requires Node.js 22+, Go 1.22+ and `@dbx-app/plugin-cli@0.1.9`.

```sh
npm ci --ignore-scripts
npm test
npm run build
go -C backend test -race ./...
go -C backend vet ./...
npm exec --yes --package=@dbx-app/plugin-cli@0.1.9 -- npm run package:all
go run scripts/verify-package.go dist/io.github.caichangqing1120.xxljob-0.2.2-darwin-arm64.dbxp --handshake
```

`package:all` builds independent native sidecars for macOS, Windows and Linux, both ARM64 and x64. It emits six unsigned `.dbxp` candidates, their `.artifact.json` metadata, `SHA256SUMS-v0.2.2.txt` and `release-candidates.json`. Cross-platform packages are architecture and checksum checked, not claimed as tested in a DBX desktop on every OS. These unsigned packages are review candidates: normal marketplace installation requires DBX Store review and signing.

Local fixture testing uses synthetic data only (`node scripts/fixture-server.mjs`); no real Admin instance or production task was used. Do not enter a real password into the development host because `.dbx-dev` stores its fixture credentials in plaintext.

## Marketplace

Source and unsigned candidates are published to the author's GitHub Release. A separate candidate PR to [`t8y2/dbx-store`](https://github.com/t8y2/dbx-store) is required; DBX maintainers review and sign the exact candidate bytes before the plugin can appear in the official catalog. Creating a public repository or Release does not itself list or sign the plugin. No signing private key belongs in this repository.

The third-party notices in `assets/` and the vendored DBX Go SDK license in `backend/sdk/` must accompany every package. The plugin's own source is MIT-licensed; see `LICENSE`.

## MCP Tools (0.2.0)

The plugin declares 13 exact tools for both the built-in AI surface and external `dbx` MCP server. It requires matching DBX Desktop and standalone MCP builds containing the plugin tool-access protocol verified against upstream `8c2bbe35b`; the existing engine floor is not a claim that every older build understands the new contribution.

- `read` (6): connection metadata, authorized executors, jobs, scheduling logs, execution log content, jobs-by-authorized-executor options.
- `write` (2): start and stop scheduling for an existing job.
- `full` (5): execute an existing job, create/edit job configuration, delete a job, create/edit executors, delete executors. Execution and administrator capabilities are deliberately not labelled ordinary writes.

Enable the installed plugin in Plugin Center for AI tools. For external tools, use **Settings -> MCP -> Capabilities** to authorize this plugin, choose an access ceiling, and opt into individual tools; use **Permissions** to restrict connections and configure high-risk call confirmation. Installation or upgrade never grants access or changes the user's grants. New/elevated non-read tools remain subject to the host's update-acceptance policy. Built-in AI additionally requires an open matching connection; external calls may use saved connections without an open workbench.

Every call uses only `lifecycle.connection.id`, host-resolved credentials and the host runtime endpoint. Optional `connectionId` must match. Tool arguments are flat, explicitly typed existing service fields, plus an explicit boolean `confirmed` for every mutation; a host approval never silently sets this flag. Unknown fields, methods, model credentials, connection-policy overrides and arbitrary RPC/URLs are rejected before authentication. Connection read-only and existing service authorization/confirmation/validation remain enforced on every call, even with cached sessions.

Discovery performs no login or network access. Execution uses an isolated, serialized, bounded 32-connection MCP cache (not the UI session map), fingerprinted by identity/configuration/credentials/read-only/runtime; host reconnect/disconnect releases only that identity. Passwords are cleared after login and are not retained in the cache. XXL execution-log requests retain and verify prior log-list provenance. Expired sessions and ambiguous writes are not automatically retried; verify service state before any deliberate retry.

Results are untrusted data, never instructions. They preserve existing 4 MiB transport limits and use a 1 MiB MCP request/result ceiling, key-based secret redaction, host-password/session-cookie replacement, and common labelled credential/Bearer redaction. These cannot recognize every arbitrary secret embedded in unlabeled application log text: avoid requesting sensitive logs. Oversized results return an error and never imply a mutation was rolled back.

Exact tool names, handlers, access groups and local verification evidence are in `docs/specs/001-mcp-tools/plan.md` and `docs/specs/001-mcp-tools/validation.md`. No live credentials or production service writes are used for this upgrade.

## Historical Version 0.2.1 Store Compatibility Patch

[Release notes](docs/releases/v0.2.1.md) | [Patch validation](docs/specs/001-mcp-tools/store-compatibility-validation.md).
The packaging script now emits the author release envelope expected by DBX Store:
`{plugin: {id, publisher, version, name, description, permissions}, artifacts: [{target, url, sha256, size}]}`.
This is not the Store's flat catalog candidate. `.dbx-store.json` uses only supported
listing fields; MCP capability/access declarations remain unchanged in `manifest.json`.
All six unsigned packages and their checksum/metadata files are still generated.
The 0.2.0 release and its historical validation are preserved, not overwritten.

Run the actual Store importer and validator regression against a Store checkout:

```sh
DBX_STORE_ROOT=/path/to/dbx-store npm run test:release
DBX_STORE_ROOT=/path/to/dbx-store DBX_RELEASE_CANDIDATE=dist/release-candidates.json npm run test:release
```

The first command substitutes compiler output with public fixtures but runs the real
release generator and Store scripts. The second also imports the actual built release
without converting its shape and compares all six artifacts to local package bytes.
These checks do not grant MCP access, install plugins, sign packages or establish live
service/desktop acceptance. Publication and official Store review are separate steps.

## Version 0.2.0 Release

[Author release](https://github.com/caichangqing1120/dbx-plugin-xxl-job/releases/tag/v0.2.0) | [Release notes](docs/releases/v0.2.0.md). Exact packages and checksums are immutable under `v0.2.0`; official signing and production acceptance are separate.

For the historical 0.2.0 release, candidate CI was explicitly dispatched with an existing immutable `tag`. The 0.2.2 release instead requires the clean official reusable workflow and provenance verification described above; prior assets must not be overwritten.

Fresh isolated `dbx-mcp 0.4.134` host discovery, access ceilings, readonly execution/denial and revocation validation: [`release-validation.md`](docs/specs/001-mcp-tools/release-validation.md). This is not Desktop installer/signature or production-service acceptance.
