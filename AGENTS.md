# AGENTS.md — hcli

Guide for coding agents (and people) working on this repository. `hcli` is a
Go CLI (cobra) for the Hetzner Cloud API (`hcli cloud ...`, via hcloud-go) and
the Hetzner Robot webservice for dedicated servers (`hcli robot ...`, own
stdlib HTTP client). Module: `github.com/digiogithub/hcli`. User docs:
[README.md](README.md).

## Layout

| Path | Owns |
| --- | --- |
| `main.go` | Entry point: `os.Exit(cmd.Execute())`. Nothing else. |
| `internal/cmd/` | Root command and global flags (`--config`, `--context`, `-o/--output`, `--no-header`, `-y/--yes`, `--debug`), plus `context`, `completion`, `version`. Wires `cloud.NewCommand` and `robot.NewCommand`. |
| `internal/cli/` | `cli.CLI`, the state shared by every command: lazy config/context resolution, `Printer()`, `Confirm()`, `ReadSecret()`, `Debugf()`, `ErrNoCloudToken` / `ErrNoRobotCredentials`. |
| `internal/config/` | Contexts in `$XDG_CONFIG_HOME/hcli/config.toml` (0600) and env overrides (`HCLI_CONFIG`, `HCLI_CONTEXT`, `HCLOUD_TOKEN`, `HCLOUD_ENDPOINT`, `HROBOT_USER`, `HROBOT_PASSWORD`, `HROBOT_ENDPOINT`). |
| `internal/output/` | `output.Printer` (table / json / yaml), `output.Table`, `KeyValues` for describe views. |
| `internal/version/` | `Version`, `Commit`, `Date`, injected with `-ldflags -X`; `Get()` falls back to the module version for `go install`. |
| `internal/cloud/` | `hcli cloud ...`: one file per resource (`server.go`, `volume.go`, ...), shared helpers (generic list/describe/delete/label builders, action waiting), completion name listers. |
| `internal/robot/` | `hcli robot ...` commands. `internal/robot/client/` is the Robot webservice client (form-encoded requests, basic auth, typed responses and error mapping). |
| `scripts/release.sh` | Builds, stages, archives and checksums release artifacts (used by the Makefile and the release workflow). |
| `.github/workflows/` | `ci.yml` (lint, test, cross-build, packaging dry run) and `release.yml` (signed release). |

Keep the boundaries: command packages depend on `cli`, `output`, `config`;
`cli` never imports a command package; the Robot client package knows nothing
about cobra.

## Building, testing, linting

```bash
make build         # ./hcli with version/commit/date ldflags (git describe)
make test          # CGO_ENABLED=1 go test -race ./...
make lint          # golangci-lint v2 (.golangci.yml)
make fmt vet       # gofmt -s -w . / go vet ./...
make cross         # linux, darwin, windows x amd64, arm64 into dist/<os>_<arch>/
make completions   # completions/hcli.{bash,zsh,fish,ps1}
make dist          # release archives + SHA256SUMS exactly as CI builds them (unsigned)
```

Pure Go: everything builds with `CGO_ENABLED=0` (only `-race` needs cgo). Do not
add cgo dependencies; the release cross-compiles all six targets from Linux and
macOS without a C toolchain.

Before finishing a change: `make fmt vet lint test` and `go mod tidy` with no
diff (CI checks it).

## Adding a Cloud resource command

1. New file `internal/cloud/<resource>.go` with a constructor returning the
   `*cobra.Command` group, registered in `cloud.NewCommand`.
2. Get the API client through the package's lazy client accessor (it reads the
   token from `c.Context()` and returns `cli.ErrNoCloudToken` when missing). Never
   read env vars or the config file directly.
3. Reuse the generic helpers for `list`, `describe`, `delete` and label commands
   where they fit; write custom `RunE` only for resource-specific verbs.
4. Output: build an `output.Table` (or `KeyValues` pairs) for the human view and
   pass the **raw hcloud-go schema object** for `-o json|yaml`, so machine output is
   the full API payload: `p.Print(raw, table)`.
5. Actions (`poweroff`, `rebuild`, attach, ...) wait for the returned
   `*hcloud.Action`s to finish and report errors from failed actions.
6. Destructive or billable operations (delete, rebuild, reset, poweroff,
   shutdown, change-type, create of paid resources, rescue, ...) call
   `c.Confirm("<what exactly>")` **before** the request. `--yes` skips it; a
   non-terminal stdin without `--yes` is refused by `Confirm`.
7. Completion: set `ValidArgsFunction` / flag completion with a name lister for
   the resource. Completion must **fail silently**: no credentials, network
   error or timeout returns no candidates and
   `cobra.ShellCompDirectiveNoFileComp`, never an error message on the terminal.
   Bound API calls with a short timeout.
8. Tests in `<resource>_test.go`: point the client at an `httptest.Server`
   (`hcloud.WithEndpoint(srv.URL)`) and assert on requests and rendered output
   (table and json). Cover the confirmation path (refused without `--yes`).

## Adding a Robot endpoint

1. Client side, `internal/robot/client/`: one method per endpoint, typed
   request/response structs in `types.go`. Robot returns JSON wrapped in an
   object keyed by the resource (`{"server": {...}}`) and takes
   `application/x-www-form-urlencoded` bodies; follow the existing helpers for
   unwrapping, form encoding and error mapping (`{"error": {"status", "code",
   "message"}}`). Tolerate the API's loose typing (string vs number vs list)
   with the flexible types already defined instead of ad-hoc parsing.
2. Command side, `internal/robot/`: build the client from `c.Context()` (return
   `cli.ErrNoRobotCredentials` when user/password are missing), print with
   `output.Printer` passing the client's typed objects for json/yaml, and call
   `c.Confirm` before resets, rescue activation, failover switches, firewall
   changes, vSwitch changes, rDNS changes and anything else that alters a server.
3. Completion of server numbers/names, IPs, vSwitch IDs: same fail-silent rule
   as Cloud. Robot has a request rate limit per hour on several endpoints;
   keep completion to one cheap list call.
4. Tests: `httptest.Server` with canned Robot responses (including error
   bodies and rate-limit `403`/`429`), never the real webservice.

## Safety rules (product)

- **No purchasing.** `robot order ...` is read-only: server market, products
  and existing transactions. Do not implement `POST /order/...` endpoints.
- **No cancellation endpoints** (`/server/{n}/cancellation` POST/DELETE,
  storagebox/addon cancellation, ...) without an explicit design discussion
  with the maintainers first. `server cancellation` (GET, read-only) is fine.
- Destructive or billable actions always go through `c.Confirm`.
- Tests must never hit real APIs or need real credentials. No live tokens in
  tests, fixtures or examples; use obvious placeholders (`#ws+example`,
  `test-token`).

## Secrets

- Never commit tokens, passwords, `config.toml` files, `.env` files or signing
  material. `.gitignore` covers the usual ones; check `git status` anyway.
- Never print credentials: not in errors, not in `--debug`. Debug output logs
  method, URL and status; the `Authorization` header and form fields such as
  passwords are redacted. hcloud-go's debug writer already redacts the token;
  the Robot client must do the same.
- Secrets are entered through `c.ReadSecret` (no echo) or env vars, never as
  command-line flags.

## Conventions

- Conventional commits, scoped by area: `feat(cloud): ...`, `feat(robot): ...`,
  `fix(cloud): ...`, `fix: ...`, `test: ...`, `refactor: ...`, `ci: ...`,
  `docs: ...`, `chore(deps): ...`. One logical change per commit.
- English for code, comments, help text and docs.
- Command names follow `hcloud` where a Cloud equivalent exists (`server list`,
  `server describe`, `--type`, `--image`, `--location`), so users can switch.
- `list` aliases `ls`, `delete` aliases `rm`. Arguments accept ID or name where
  the API allows both.
- `gofmt -s`, golangci-lint clean. Do not silence linters without a comment
  explaining why.

## Release

Tag-driven and signed (Digio signing scheme, same as Pando / Bitácora):

```bash
git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z    # vX.Y.Z-rc.N => pre-release
```

`.github/workflows/release.yml` job graph:

```
linux-windows (ubuntu) ──► windows-sign (windows-latest, env: release) ──┐
        │                                                                 ├──► release (ubuntu)
        └──────────────────────────────── dist-linux ─────────────────────┤
macos (macos-latest, signed + notarized) ─────── dist-macos ──────────────┘
```

- `linux-windows`: `scripts/release.sh stage` for linux/{amd64,arm64} and
  windows/{amd64,arm64} (CGO off, `-s -w`, version/commit/date ldflags),
  completion scripts generated from the linux/amd64 host binary, archives.
  Linux -> `dist-linux`; Windows zips -> **`unsigned-windows`**.
- `windows-sign`: `azure/login` (OIDC, vars `AZURE_CLIENT_ID`,
  `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`) + `azure/trusted-signing-action`
  (vars `AZURE_SIGNING_ENDPOINT`, `AZURE_SIGNING_ACCOUNT`,
  `AZURE_SIGNING_CERT_PROFILE`; SHA256 digests; timestamp
  `http://timestamp.acs.microsoft.com`), `Get-AuthenticodeSignature` must be
  `Valid`, re-zip -> `dist-windows`.
- `macos`: `digiogithub/ci-actions` `macos-signing-keychain` (secret
  `MACOS_SIGNING_BUNDLE`) -> `macos-codesign` (hardened runtime + timestamp) ->
  `macos-notarize` (zip of a bare Mach-O: submit-only, no staple) -> archive ->
  `macos-keychain-cleanup` (`if: always()`) -> `dist-macos`.
- `release`: downloads `pattern: dist-*` only, checks the six archives exist,
  writes `SHA256SUMS`, publishes with `digiogithub/ci-actions/github-release@v1`
  (notes since the previous tag, pre-release when the tag has a `-`).
- Re-cut an existing tag: *Actions > Release > Run workflow* with `tag`.

Invariants — do not break them when editing the workflow:

1. Only artifacts named `dist-*` reach the release job. Unsigned intermediates
   use any other name (`unsigned-windows`), so an unsigned binary can never be
   published.
2. Windows signing runs on `windows-latest`, in `environment: release`, with
   `permissions: id-token: write`, and fails unless the signature is `Valid`.
   The federated credential only matches that environment.
3. macOS binaries are codesigned with the hardened runtime and a timestamp and
   notarized; the keychain is always cleaned up. `timeout-minutes: 120`.
4. Anything that rewrites the binary (UPX, strip) must run **before** signing.
   UPX is currently not used.
5. Actions are pinned to tags (`digiogithub/ci-actions/...@v1`, `actions/*@v4`).
6. A tag run uses the workflow from the tagged commit: commit workflow changes
   before tagging.

One-off repository setup (already shared by all Digio repos, nothing new to
create in Azure or Apple): the Digio release-signing procedure
(`digio-release-signing` skill, `scripts/setup-repo.sh digiogithub/hcli`) adds
two federated credentials to the shared Entra app, the `release` environment,
the six `AZURE_*` repository variables and the `MACOS_SIGNING_BUNDLE` secret.
Its default mode is a dry run; `--apply` / `--secret` perform the writes and
need a maintainer's explicit go-ahead. Agents never run the write mode, never
read, print or decrypt the signing bundle, and never create new signing
resources.

Setup status: applied on 2026-11-02 (credentials `hcli-release-environment` and
`hcli-release-environment-immutable`, environment `release`, six `AZURE_*`
variables, secret `MACOS_SIGNING_BUNDLE`). First signed pre-release
`v0.1.0-rc.1` (run 41531045391): both Windows `.exe` `Valid` /
`CN=Digio Soluciones Digitales SL`; both darwin binaries signed with
`Developer ID Application: DIGIO Soluciones Digitales S.L.N.E. (LRE923483J)`
and notarization `Accepted` (bare Mach-O: submitted as zip, not stapled).
