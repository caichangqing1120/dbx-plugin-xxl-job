# 0.2.0 Release Validation - 2026-10-28

## Fresh Local Checks

All seven plugin backend suites, race suites and vet checks pass on the final adapter source (274 top-level / 639 including subtests per normal/race run). Six applicable frontend test/build pipelines pass (360 tests total), as do Apollo's 28 release fixtures and all existing SDK checks. Complete upstream Schema and independent ZIP/checksum/embedded-architecture checks pass for 41 packages and 80 exact tool declarations.

## Installed Standalone MCP Host Integration

The installed `dbx-mcp 0.4.134` was exercised through its real stdio interface with a disposable `DBX_DATA_DIR`, synthetic saved connections/credentials and local loopback HTTP fixtures. Exact native ARM64 candidate contents were unpacked into the isolated development registry. This is development registration, not package-installer/signature acceptance.

- Registered plugins with no grants: zero plugin tools, no service traffic.
- Read ceiling: 47 tools; write ceiling: 55; full ceiling with approved tools: 80.
- New/unapproved mutations remain hidden; runtime names/hints match all declarations.
- All seven plugins successfully execute representative readonly service requests through the host's actual saved-config/lifecycle dispatch and packaged sidecars; recognizable credentials are redacted.
- Cross-plugin connection choices are denied for all seven. Global and saved-connection readonly policies deny mutation paths before fixture HTTP traffic; scoped-connection denial and immediate grant revocation also pass.
- No fixture mutation endpoint was reached. All test hosts, sidecars and loopback servers were stopped; only disposable test grants were used. The user's real profile, connections, grants and Desktop process were not modified.

## Publication Boundary

The user explicitly authorizes local verification, immutable author publication and source push. Publication uses a source-commit tag prerequisite before Release creation, followed by fast-forward updates to each pre-existing implementation branch. Public plugins use GitHub author releases; private plugins remain on private GitLab. Official DBX Store signing/listing and production-service or desktop-installer acceptance are separate and not claimed.

Published candidate bytes must exactly match the locally verified assets. Read every uploaded asset back and compare its SHA256/size; never overwrite an existing differing tag or asset. Published releases must contain the complete platform/metadata/checksum set before becoming visible.
