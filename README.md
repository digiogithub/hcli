# hcli

One command line for **Hetzner Cloud** and **Hetzner Robot** (dedicated servers).

`hcli` is a single static binary that talks to two Hetzner APIs:

- `hcli cloud ...` — the [Hetzner Cloud API](https://docs.hetzner.cloud/) (`api.hetzner.cloud`), through the official [hcloud-go](https://github.com/hetznercloud/hcloud-go) library.
- `hcli robot ...` — the [Robot webservice](https://robot.hetzner.com/doc/webservice/en.html) (`robot-ws.your-server.de`) for dedicated servers, IPs, subnets, failover, firewalls, vSwitches and more.

Credentials for both live together in named **contexts**, so one `hcli context use customer-x` switches a Cloud project and a Robot account at once.

> Status: early development (v0.x). Flags and output may still change; `hcli <group> --help` is the reference.

## Features

- **Cloud and Robot in one tool** — the official `hcloud` CLI only covers Cloud; dedicated servers usually mean curl against the Robot webservice. `hcli` covers both with the same flags, output formats and contexts.
- **Contexts** — named credential sets in a `0600` TOML file, plus environment overrides compatible with `hcloud` (`HCLOUD_TOKEN`).
- **Output for humans and scripts** — aligned tables by default, `-o json` / `-o yaml` return the full API object.
- **Dynamic shell completion** — bash, zsh, fish and PowerShell, completing resource names (servers, volumes, networks, IPs, contexts...) live from the API.
- **Safe by default** — destructive or billable actions ask for confirmation (`--yes` to skip); no purchasing or cancellation through Robot.
- **Signed binaries** — macOS builds are signed with a Developer ID and notarized by Apple; Windows builds are Authenticode-signed.

## Installation

### Release downloads

Download the archive for your platform from the [latest release](https://github.com/digiogithub/hcli/releases/latest):

| Platform | Archive |
| --- | --- |
| Linux x86-64 / ARM64 | `hcli_<version>_linux_amd64.tar.gz` / `hcli_<version>_linux_arm64.tar.gz` |
| macOS Intel / Apple Silicon | `hcli_<version>_darwin_amd64.tar.gz` / `hcli_<version>_darwin_arm64.tar.gz` |
| Windows x86-64 / ARM64 | `hcli_<version>_windows_amd64.zip` / `hcli_<version>_windows_arm64.zip` |

Each archive contains the `hcli` binary, `LICENSE`, this `README.md` and ready-made completion scripts in `completions/`.

Linux and macOS:

```bash
VERSION=1.0.0                      # without the leading "v"
OS=linux ARCH=amd64                # or darwin / arm64
BASE=https://github.com/digiogithub/hcli/releases/download/v$VERSION
FILE=hcli_${VERSION}_${OS}_${ARCH}.tar.gz

curl -fsSLO "$BASE/$FILE" -O "$BASE/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS      # macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
tar -xzf "$FILE"
sudo install -m 0755 "hcli_${VERSION}_${OS}_${ARCH}/hcli" /usr/local/bin/hcli
hcli version
```

Windows (PowerShell):

```powershell
$Version = "1.0.0"; $Arch = "amd64"   # or arm64
$Base = "https://github.com/digiogithub/hcli/releases/download/v$Version"
$File = "hcli_${Version}_windows_${Arch}.zip"
Invoke-WebRequest "$Base/$File" -OutFile $File
Invoke-WebRequest "$Base/SHA256SUMS" -OutFile SHA256SUMS
$expected = (Select-String -Path SHA256SUMS -Pattern ([regex]::Escape($File))).Line.Split(' ')[0]
if ((Get-FileHash $File -Algorithm SHA256).Hash.ToLower() -ne $expected) { throw "checksum mismatch" }
Expand-Archive $File -DestinationPath .
Get-AuthenticodeSignature ".\hcli_${Version}_windows_${Arch}\hcli.exe"   # Status: Valid
```

Then move `hcli.exe` to a folder on your `PATH`.

About the signatures:

- **macOS**: the binaries are signed with the Digio Developer ID (hardened runtime) and notarized. A bare command-line binary cannot carry a stapled ticket, so Gatekeeper verifies the notarization online the first time it runs. `curl`/`tar` downloads are not quarantined anyway; check with `codesign -dv --verbose=2 $(which hcli)`.
- **Windows**: `hcli.exe` is Authenticode-signed by *Digio Soluciones Digitales SL* (Azure Artifact Signing, RFC 3161 timestamp). SmartScreen may still warn on brand-new releases until the download builds reputation.
- **Linux**: integrity is covered by `SHA256SUMS`.

### With Go

```bash
go install github.com/digiogithub/hcli@latest
```

Requires the Go version from `go.mod`. `hcli version` reports the module version; commit and build date are only filled in by release builds.

### From source

```bash
git clone https://github.com/digiogithub/hcli && cd hcli
make build          # ./hcli
make install        # into $(go env GOPATH)/bin
```

## Credentials

### Hetzner Cloud: API token (one per project)

Cloud tokens are scoped to **one project**. In the [Cloud Console](https://console.hetzner.cloud/) open the project, then **Security > API tokens > Generate API token**. Choose *Read* for inventory/monitoring use, *Read & Write* to manage resources. The token is shown only once.

### Hetzner Robot: webservice user (one per account)

Dedicated servers use a separate **webservice user**, not your Robot login. In [Robot](https://robot.hetzner.com/) go to **Settings > Webservice and app settings**, create the webservice user and set its password. The user name looks like `#ws+XXXXXXXX`. It has access to every server of the account.

## Contexts and environment

A context bundles a Cloud token and/or a Robot webservice user. Secrets are prompted without echo (or read from stdin when piped); they are never passed as flags, so they do not end up in your shell history.

```bash
hcli context create digio --cloud                              # asks for the Cloud token
hcli context create digio --robot --robot-user '#ws+abc123'    # asks for the Robot password
hcli context create customer-x --cloud --robot --robot-user '#ws+def456' --activate

hcli context list
hcli context use customer-x
hcli context active
hcli --context digio cloud server list      # one-off override
hcli context delete old-project
```

Contexts are stored in `$XDG_CONFIG_HOME/hcli/config.toml` (the OS config directory: `~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows), written with mode `0600`.

Environment variables take precedence over the stored context:

| Variable | Meaning |
| --- | --- |
| `HCLI_CONFIG` | path of the config file |
| `HCLI_CONTEXT` | context to use (same as `--context`) |
| `HCLOUD_TOKEN` | Cloud API token (same name as the official `hcloud` CLI) |
| `HCLOUD_ENDPOINT` | Cloud API endpoint (default `https://api.hetzner.cloud/v1`) |
| `HROBOT_USER` / `HROBOT_PASSWORD` | Robot webservice credentials |
| `HROBOT_ENDPOINT` | Robot endpoint (default `https://robot-ws.your-server.de`) |

Context selection order: `--context`, then `HCLI_CONTEXT`, then the active context. A CI job can skip the config file entirely and use only the environment.

## Shell completion

Completion is dynamic: after `hcli cloud server describe <TAB>` the server names of the active context are fetched from the API. Lookups that fail (no credentials, no network) simply return no suggestions.

```bash
# Bash (needs bash-completion)
hcli completion bash > ~/.local/share/bash-completion/completions/hcli

# Zsh (make sure "autoload -U compinit; compinit" runs in ~/.zshrc)
hcli completion zsh > "${fpath[1]}/_hcli"

# Fish
hcli completion fish > ~/.config/fish/completions/hcli.fish
```

```powershell
# PowerShell: add this line to $PROFILE
hcli completion powershell | Out-String | Invoke-Expression
```

The release archives ship the same scripts in `completions/` (`hcli.bash`, `hcli.zsh`, `hcli.fish`, `hcli.ps1`).

## Usage

### Hetzner Cloud

```bash
hcli cloud server list
hcli cloud server describe web-01 -o yaml
hcli cloud server create --name web-02 --type cx22 --image ubuntu-24.04 --location fsn1 --ssh-key ops
hcli cloud server reboot web-01
hcli cloud server enable-rescue web-01 --ssh-key ops
hcli cloud server create-image web-01 --description "before upgrade"
hcli cloud server ssh web-01
hcli cloud server delete web-02            # asks for confirmation

hcli cloud server-type list
hcli cloud volume create --name data --size 50 --server web-01
hcli cloud firewall list
hcli cloud floating-ip assign <ip> web-01
hcli cloud pricing
```

Resources: `server` (list, describe, create, delete, poweron, poweroff, reboot, reset, shutdown, rebuild, change-type, enable-rescue, disable-rescue, create-image, ip, ssh, ...), `server-type`, `image`, `location`, `iso`, `volume`, `network`, `firewall`, `floating-ip`, `primary-ip`, `load-balancer`, `ssh-key`, `certificate`, `placement-group` and `pricing`.

### Hetzner Robot (dedicated servers)

```bash
hcli robot server list
hcli robot server describe 123456
hcli robot server rename 123456 db-01
hcli robot server cancellation 123456           # read-only

hcli robot rescue enable 123456 --os linux --ssh-key ops
hcli robot reset execute 123456 --type hw       # asks for confirmation
hcli robot wol 123456
hcli robot ip list
hcli robot rdns set 203.0.113.10 db-01.example.com
hcli robot failover list
hcli robot firewall describe 123456
hcli robot vswitch list
hcli robot traffic 203.0.113.10 --from 2026-10-01 --to 2026-10-31
hcli robot order server-market list --min-ram 64
```

Areas: `server` (list, describe, rename, cancellation), `reset` (options, execute), `rescue`, `linux` (installation on next boot), `wol`, `ip`, `subnet`, `rdns`, `failover`, `traffic`, `firewall` (and templates), `vswitch`, `ssh-key` and `order` (`server-market`, `server`, `transaction`: **read-only**, no purchasing). Servers can be given by number, IP or name.

Storage Boxes are not in the Robot webservice any more: Hetzner moved them to the Hetzner API (`api.hetzner.com`) in 2025, so `hcli robot` does not cover them.

The exact flags of every command are in `hcli <command> --help`.

### Output formats

| Flag | Result |
| --- | --- |
| *(default)* `-o table` | aligned columns; `--no-header` drops the header line |
| `-o json` | the raw API object(s), indented |
| `-o yaml` | the same data as YAML |

```bash
hcli cloud server list -o json | jq -r '.[] | select(.status=="running") | .name'
hcli robot server list --no-header | awk '{print $1}'
```

`--debug` logs the API requests (method, URL, status) to stderr. Credentials and `Authorization` headers are never printed.

## Safety

- Destructive or billable actions (delete, rebuild, reset, power off, create, change-type, rescue, failover switch, ...) show what they are about to do and ask `Continue? [y/N]`.
- `--yes` / `-y` skips the prompt for scripts. Without a terminal and without `--yes`, `hcli` refuses instead of guessing.
- Robot: **no purchasing and no cancellations**. `order` commands only browse the server market, products and existing transactions; `server cancellation` only reads the cancellation state. Ordering and cancelling stay in the Robot web interface on purpose.
- Secrets never go on the command line: they are prompted, piped or read from the environment.

## Comparison with `hcloud`

| | `hcloud` (official) | `hcli` |
| --- | --- | --- |
| Hetzner Cloud | yes, complete | yes (core resources) |
| Robot / dedicated servers | no | yes |
| Credentials | contexts with one Cloud token | contexts with a Cloud token **and** a Robot user |
| Output | table, json, yaml (via `-o`) | table, json, yaml |
| Completion | static + dynamic | dynamic resource names |

If you only use Hetzner Cloud, `hcloud` is the reference tool and covers every API corner. `hcli` exists for teams that run Cloud projects **and** dedicated servers and want one tool and one set of contexts for both. `HCLOUD_TOKEN` works the same in both.

## Development

Requirements: the Go version in `go.mod`, `make`, and [golangci-lint](https://golangci-lint.run/) v2 for `make lint`.

```bash
make build        # ./hcli with version/commit/date from git
make test         # go test -race ./...
make lint         # golangci-lint run
make fmt vet
make cross        # the six release targets into dist/<os>_<arch>/
make completions  # completion scripts into completions/
make dist         # release archives + SHA256SUMS (unsigned), like the CI
make clean
```

Tests never hit the real Hetzner APIs: they run against `httptest` servers. See [AGENTS.md](AGENTS.md) for the code layout and how to add commands.

CI (`.github/workflows/ci.yml`) runs gofmt, `go vet`, golangci-lint, `go test -race` on Linux, macOS and Windows, and cross-builds all six targets.

## Release process

Releases are tag-driven:

```bash
git tag -a v1.2.0 -m "v1.2.0"
git push origin v1.2.0
```

`.github/workflows/release.yml` then builds the Linux and Windows binaries on Ubuntu, signs the Windows `.exe` on a Windows runner with Azure Artifact Signing (OIDC, environment `release`), builds, signs and notarizes the macOS binaries on a macOS runner, and publishes a GitHub release with every archive, `SHA256SUMS` and the changes since the previous tag. Tags with a suffix (`v1.2.0-rc.1`) are published as pre-releases. A release can be re-cut from the *Actions* tab (`workflow_dispatch` with the tag).

The signing material is shared across Digio projects and set up once per repository (Azure federated credentials, the `release` environment, `AZURE_*` variables and the `MACOS_SIGNING_BUNDLE` secret) with the Digio release-signing procedure. Details in [AGENTS.md](AGENTS.md#release).

## License

[MIT](LICENSE) — Copyright (c) 2026 Digio Soluciones Digitales SL.
