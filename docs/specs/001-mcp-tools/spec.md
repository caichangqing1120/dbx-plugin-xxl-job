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
