# xxljob MCP Tools Upgrade

## Scope And Authorization

The user requests exposing all existing plugin capabilities under the updated DBX MCP rules and upgrading plugin versions. The user explicitly includes the private XXL-JOB, BD Job and BD Gateway plugins. SSH is excluded. Implementation is authorized; remote push, publication, installation, restarts and changes to user grants are not authorized.

## Authoritative Protocol

Reference: DBX upstream `8c2bbe35b`, `plugins/README.md` section `mcp`, `plugins/manifest.schema.json`, and plugin-development documentation. This package adapts existing capabilities, not new service APIs.

- Implement side-effect-free `mcp/tools` returning `{"tools":[{"name","description","inputSchema","annotations"}]}`.
- Implement `mcp/call` accepting `{"tool","arguments","lifecycle"}` and returning MCP CallToolResult `{"content":[{"type":"text","text":"..."}],"isError":false}`; execution errors must be represented consistently and never resemble success.
- Add exactly one manifest `mcp` contribution with `external_tools: true`, `ai_tools: true`, meaningful `toolsets`, and `tools` declarations keyed exactly by the discovered tool name. At most 64 entries. Declare `access` as `read` (no state change), `write` (ordinary changes), or `full` (destructive, arbitrary execution or bulk/admin changes); each tool's highest reachable effect controls its level. No annotations-only exposure.
- Expose existing service capabilities with accurate schemas; do not add raw arbitrary RPC, URL, credential, connection-lifecycle, install or host-control tools. Existing generic request adapters need enforced method/path classification so a read tool cannot mutate state.
- Host binds identity using `lifecycle.connection.id`; reject mismatched argument identities. Use lifecycle credentials and transformed runtime endpoint, never model-supplied credentials. Support external calls on saved connections without requiring an already-open UI session. Do not overwrite or disconnect unrelated existing sessions.
- Discovery must not authenticate, connect to services, write files or create workloads. For every execution, enforce connection read-only policy plus all existing plugin mutation/confirmation/validation guards. A readonly lifecycle must not inherit writable cached session permissions.
- Preserve existing API paths, parameters, auth, redaction, two-phase confirmation, idempotency and UI behavior. Keep all current capabilities even on non-main source branches.
- Tool output is untrusted data, bounded and secret-redacted. Reuse existing transport timeouts and size limits. Reject malformed arguments and unknown tools before network effects.
- Preserve user-controlled grants: installing/upgrading never grants external access automatically. Document settings enablement and matching updated DBX/MCP requirements.
- Upgrade active manifest, backend initialize metadata, package.json/package-lock.json and active build metadata consistently to `0.2.0`. Do not rewrite historical release receipts, tagged assets or old versioned notes. Keep publisher and plugin ID unchanged.

## Acceptance

Baseline tests run before edits. New tests are observed failing before bridge implementation, then passing. Cover manifest/discovery parity and access hints, every supported tool or endpoint dispatch, side-effect-free discovery, external lifecycle calls, missing/mismatched identity, read-only mutation denial, existing confirmation guards, malformed/unknown input, error envelopes and secret handling. Run complete Go tests, race and vet, applicable frontend tests/build, and package checks. Use only local fake HTTP services, never real credentials or service writes. Report exact verification gaps without claiming deployment or acceptance.

## Delivery Authorization (2026-10-28)

The user explicitly requests local verification, publication of the new version, and source-code push. This authorizes immutable v0.2.0 author releases in each repository's existing public/private channel, a tag-only source prerequisite, and then fast-forward updates to the existing source branches. It does not authorize production service writes, changing the user's real MCP grants, official Store signing, unrelated branch merges or overwriting prior assets.


## Store Compatibility Patch 0.2.1 (2026-10-29)

The user authorizes correcting public author release metadata and publishing a new immutable `0.2.1` patch; do not modify historical `0.2.0` assets or receipts. Preserve the plugin ID/publisher, all MCP declarations/access levels, APIs and UI behavior. Bump only active manifest, backend initialize and package/lock versions. Store listing metadata must contain only fields allowed by `dbx-store` sync; MCP support stays declared in the packaged manifest, not an unsupported `.dbx-store.json` badge.

The active `package:all` generator must emit `release-candidates.json` as `{plugin: {id, publisher, version, name, description, permissions}, artifacts: [{target, url, sha256, size}]}`. The Store synchronizer consumes this author release envelope and creates its separate flat catalog candidate; these are different contracts. Keep all six platform artifacts, per-artifact metadata and SHA256 checksum file. Import unmodified generated output through the actual Store synchronizer, then validate its catalog candidate with the actual Store validator/schema. Local fixtures only; no services, credentials, installation or grant changes.

## Maintainer Review Response 0.2.2 (2026-10-29)

The active version is `0.2.2`; keep 0.2.0/0.2.1 tags, assets, versioned notes and
receipts immutable. Neither version entered the Store. Store-facing notes must
describe all 13 lifecycle-bound MCP tools since Store 0.1.1, including their
six `read`, two `write` and five `full` classifications, not only the
author-envelope/unsupported-`mcpTools` metadata repair. Use the exact reviewer
wording retained in `../../releases/v0.2.2.md`. Pin listing source to
`https://github.com/caichangqing1120/dbx-plugin-xxl-job/tree/v0.2.2`.

Preserve all manifest contributions, empty manifest permissions, plugin
ID/publisher, API/UI behavior and existing lifecycle, confirmation and read-only
guards. Upgrading grants no access. Matching updated DBX Desktop and standalone
MCP builds remain required. Preserve the Store-importable author envelope and
all six target artifacts rather than introducing a new release schema.

Build in clean CI through the official reusable workflow pinned at
`417c1bfc28ea8669dfbc2e1525f54e3b68285b1c`, with `install-plugin-cli: false`
and Go 1.26.3; retain the existing backend `replace ... => ./sdk`. Run
`scripts/package-ci.py` with `RELEASE_TAG=v0.2.2` and `DBX_PLUGIN_TARGET`.
Full packaged executable VCS provenance is mandatory: tag-matching
`vcs.revision`, `vcs.modified=false`, complete build info and a per-target
provenance record beside package/artifact metadata. Fail packaging if provenance
is missing or mismatched. Verify all six architectures, initialize
identity/version, final hashes/sizes and actual Store import/validation.

Fresh CI execution, downloaded-byte/provenance verification, publication and Store
review/signing remain pending delivery gates until separately evidenced. This
source subtask does not authorize task execution, service calls, Desktop installs,
grant changes or official signing; neither source edits nor historical receipts
establish those outcomes.
