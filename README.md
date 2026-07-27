# AGX

**English** | [简体中文](README.zh-CN.md)

> Agent Extensions — a cross-platform global extension manager for AI coding agents.

AGX uses a unified catalog, lockfile, content store, agent adapters, and transactional installation flow to manage and distribute user-level global extensions across multiple AI coding agents.

```text
Skills + Plugins + MCP Servers + Instructions
                        │
                        ▼
                       AGX
                        │
            ┌────────────────────┐
            ▼          ▼          ▼
          Codex    Claude Code   OpenCode / Pi
```

## Project status

AGX is currently in the early design and implementation stage. Its CLI, catalog schema, and installation workflow are not yet stable. This README describes the intended architecture; it does not imply that every feature is already available.

The implemented scope currently includes Skills, Codex/Pi/OpenCode global Instructions, a local Catalog registry, Profiles, explicit local multi-Catalog composition, a content-addressed Store, `local` and `git` Skill sources, Codex, Claude Code, Pi, and OpenCode adapters, copy-based installation, overlays, and the `init`, `catalog`, `store`, `list`, `lock`, `plan`, `apply`, `status`, `rollback`, `repair`, `diff`, `audit`, `approve`, `update`, and `doctor` commands. Plugins, MCP servers, Claude global Instructions, remote Catalog fetching, and Catalog Git synchronization remain part of the target model but are not yet implemented.

The current prototype can initialize, register, select, load, and lock a local Catalog, check and selectively accept source updates, apply deterministic overlays, compare locked and candidate Skill content, run static risk audits, store digest-bound local approvals, install local or approved Git-backed Skills, manage a marked section in Codex's global `AGENTS.md`, inspect the active generation, and restore an earlier snapshot. Content outside the AGX Instructions markers is preserved during update, removal, and rollback, and edits outside that block are not treated as managed drift. Overlays currently support deterministic `SKILL.md` prepend/append content and disabling named scripts; unsupported rename and target-private metadata are rejected explicitly. Git Skills are unreviewed by default: `plan` and `apply` reject them until `agx approve` records approval for the exact commit, content digest, overlay digest, Adapter security version, and policy digest. Changing any bound value—including accepting an update—invalidates approval. Status and doctor detect unfinished transactions, while `agx repair` safely completes their compensation rollback when recorded content digests still match. Unknown existing Skill targets remain conflicts unless `--adopt` is used with exactly matching content; externally modified managed content is never silently overwritten. Generations created before rollback snapshots were introduced cannot be restored. GitHub Actions verifies native tests and builds on Linux, macOS, and Windows, with race detection and vetting on Linux.

Local Skill, overlay, and Instructions source paths may be relative to the Catalog, absolute, or rooted at the current user's home with `~/`. For example, `skills/review`, `~/agent-skills/review`, and `/opt/agent-skills/review` are valid on Unix-like systems; Windows also accepts native absolute paths such as `C:\agent-skills\review`. AGX does not expand environment variables or other users' homes such as `~alice`. Git source `path` values remain relative to the repository root. Prefer `~/` over an absolute path when the same Catalog will be used on multiple machines, because the original path expression is preserved in `agx.lock`.

`agx init --name personal` creates a non-destructive empty `agx.yaml` together with `skills/`, `overlays/`, and `instructions/`. It refuses to overwrite an existing Catalog. Empty Catalogs can be listed and locked normally; when an empty Catalog would remove managed resources, `plan` and `apply` require the explicit `--allow-empty` flag.

Local Catalogs can be registered and selected from any directory:

```bash
agx catalog add personal --path ~/my-agx
agx catalog add work --path ~/work/agx.yaml
agx catalog list
agx catalog use work
agx catalog remove personal
```

The first registered Catalog becomes active automatically. `remove` only removes the registration and never deletes the Catalog file. Commands that accept `--catalog` resolve their Catalog in this order: an explicit `--catalog`, `agx.yaml` in the current directory, then the active registered Catalog. `agx init` is intentionally different and continues to create `./agx.yaml` unless `--catalog` is supplied. Registry state is stored in the operating system's user configuration directory under `agx/catalogs.yaml`; set the absolute `AGX_CONFIG_HOME` path to override that location. The registry currently accepts local files or directories only. One active Catalog is used for default lookup, while `--catalogs` explicitly composes registered local Catalogs. Remote Catalog repositories are not fetched yet.

Profiles select a deployment subset without changing what the lockfile records. A non-empty `skills.include` starts from the named Skills; when `include` is omitted, all Catalog Skills are selected. `skills.exclude` is then removed, and `targets` is intersected with each Skill's enabled targets. Profile names and references are validated when the Catalog is loaded.

```yaml
profiles:
  work:
    skills:
      include:
        - code-review
        - infrastructure
      exclude:
        - blog-writing
    targets:
      - codex
      - claude
```

Use `agx list --profile work`, `agx plan --profile work`, and `agx apply --profile work`. `agx lock` continues to lock the complete Catalog so switching Profiles does not discard source resolution. Applying a Profile makes its selected set the complete desired installation and removes previously managed targets that are no longer selected. If a Profile selects no installable resources while managed resources exist, `plan` and `apply` require `--allow-empty`. Created generations and `agx status` record the selected Profile.

Multiple registered local Catalogs can be composed explicitly without changing the normal single-Catalog lookup behavior. Each Catalog keeps its own adjacent `agx.lock`; lock them separately, then pass a comma-separated registered name list to deployment commands:

```bash
agx catalog add personal --path ~/my-agx
agx catalog add work --path ~/work/agx.yaml
agx lock --catalog ~/my-agx/agx.yaml
agx lock --catalog ~/work/agx.yaml

agx list --catalogs personal,work
agx plan --catalogs personal,work
agx apply --catalogs personal,work
```

Composed resources always use qualified names such as `personal/code-review` and `work/deploy`. Catalog order does not allow one resource to overwrite another. If two Skills would map to the same Agent target directory, planning fails with an explicit conflict. A Profile can select across Catalogs with qualified references; its own Catalog can still use short references:

```yaml
profiles:
  work:
    skills:
      include:
        - code-review
        - work/deploy
```

With multiple Catalogs, a short Profile name is accepted only when it is unambiguous; otherwise use a qualified name such as `personal/work`. Composed generations store a deterministic digest of all participating Catalogs and lockfiles, and `agx status` reports their sorted names.

Global Instructions for Codex, Pi, and OpenCode are declared as ordered Markdown fragments. AGX locks their exact contents, concatenates enabled sets across composed Catalogs deterministically, and manages only a marked block in each target file:

```yaml
instructions:
  common:
    sources:
      - instructions/common.md
      - instructions/coding.md
    targets:
      codex: {}
      pi: {}
      opencode: {}
```

The managed paths are `$CODEX_HOME/AGENTS.md` (default `~/.codex/AGENTS.md`), `$PI_CODING_AGENT_DIR/AGENTS.md` (default `~/.pi/agent/AGENTS.md`), and `$XDG_CONFIG_HOME/opencode/AGENTS.md` (default `~/.config/opencode/AGENTS.md`).

If a non-empty `$CODEX_HOME/AGENTS.override.md` exists, `plan`, `apply`, and `doctor` report a conflict because Codex gives it precedence over `AGENTS.md`. Codex loads global guidance when a run starts, so restart the Codex session after applying changed Instructions. Profile `targets` filter Instructions as well as Skills; Instructions sets are otherwise included automatically from the selected Catalogs. Updates, removal, and rollback preserve all content outside `<!-- BEGIN AGX MANAGED INSTRUCTIONS -->` and `<!-- END AGX MANAGED INSTRUCTIONS -->`.

`agx lock` stores the exact raw Skill and Overlay directories under their SHA-256 digests. Locked review, planning, approval, and installation verify and materialize those immutable objects instead of repeatedly fetching Git repositories or depending on a local source directory that may later disappear. Candidate review and update checks still resolve the live source. A changed live local source or Overlay remains lock drift and requires `agx lock`; a missing source can use its verified Store object. Corrupt objects are rejected rather than silently repaired. The Store defaults to `store/` under the AGX state directory and can be relocated with an absolute `AGX_STORE_HOME` path. Automatic garbage collection is not implemented yet.

Store maintenance is explicit and reports before mutating data:

```bash
agx store status
agx store verify
agx store gc --dry-run
agx store gc
agx store gc --prune-stale --force
```

Every successful lockfile write and accepted update records the lockfile's content digests as a Store reference. GC removes only objects not referenced by those records. References whose lockfile is temporarily unavailable are retained by default; `--prune-stale` removes them explicitly. If no live lock references remain, `--force` is required before deleting objects. Automatic background garbage collection is not implemented.

The current overlay format is declared by adding `overlay: overlays/review` to a Skill and placing this manifest at `overlays/review/overlay.yaml`:

```yaml
apiVersion: agx.dev/v1alpha1
kind: Overlay

content:
  prepend: prepend.md
  append: append.md

disableScripts:
  - scripts/upload-result.sh
```

The referenced Markdown is applied to `SKILL.md`, and disabled scripts are removed from the rendered installation artifact. Before writing `agx.lock`, AGX validates the Overlay manifest, its referenced files, and that the Overlay can be applied to the resolved Skill. The original source digest and overlay digest remain separate in `agx.lock`; review and deployment operate on the rendered result.

## Why AGX

AI coding agents use different global directories, configuration formats, and extension mechanisms. Maintaining each agent independently leads to:

- Skills and instructions drifting apart over time.
- Third-party extension sources and versions becoming difficult to trace.
- Plugin and MCP updates lacking a consistent diff and review process.
- Environments that cannot be reproduced reliably on another machine or operating system.
- Agent-native configuration, personal content, third-party dependencies, and generated files becoming mixed together.

AGX expresses these global extensions as a lockable, reviewable, and reversible desired state, then projects that state into each agent through an adapter.

## Scope

AGX is intended to manage:

- **Skills** from local paths, public Git repositories, or private Git repositories.
- **Plugins** installed and validated by agent-specific adapters.
- **MCP servers** declared once and merged semantically into each agent's global configuration.
- **Instructions** rendered into user-level entry points such as `AGENTS.md` and `CLAUDE.md`.
- **Profiles** that select extension sets for different contexts.

AGX is not intended to manage:

- Agent accounts, API tokens, SSH private keys, or other secret values.
- Default models, themes, UI preferences, or conversation history.
- General-purpose dotfiles, shell, Git, or editor configuration.
- Project-level business dependencies or general development environment packages.

## Core model

```text
Catalog
  ↓ resolve
Lockfile
  ↓ fetch
Content-addressed Store
  ↓ overlay + adapter
Generation
  ↓ transactional install
Agent global directories and managed configuration fields
```

- **Catalog** describes the desired global extension state.
- **Lockfile** resolves Git branches and tags to immutable commits and records content digests.
- **Store** keeps verified immutable Skill and Overlay directories addressed by content hash.
- **Adapter** handles agent-specific paths, capabilities, rendering, and configuration merging.
- **Generation** records a complete deployment for status checks and rollback.

## Target catalog example

> The schema below shows the target model and includes fields that the current Milestone 1 parser does not accept yet. The currently executable subset is defined by `schemas/catalog.schema.json`.

```yaml
apiVersion: agx.dev/v1alpha1
kind: Catalog

metadata:
  name: personal

skills:
  code-review:
    source:
      type: local
      path: skills/code-review
    targets:
      codex: {}
      claude: {}
      pi: {}
      opencode: {}

  frontend-design:
    source:
      type: git
      repository: https://github.com/example/agent-skills.git
      revision: main
      path: skills/frontend-design
    targets:
      codex: {}
      claude: {}

plugins:
  example-plugin:
    source:
      type: git
      repository: https://github.com/example/claude-plugin.git
      revision: v1.0.0
    targets:
      claude: {}

mcpServers:
  github:
    transport: stdio
    command:
      executable: github-mcp-server
    environment:
      GITHUB_TOKEN:
        from: env
        name: GITHUB_TOKEN
    targets:
      codex: {}
      claude: {}

instructions:
  personal:
    sources:
      - instructions/common.md
      - instructions/coding.md
      - instructions/safety.md
    targets:
      codex: {}
      pi: {}
      opencode: {}

profiles:
  default:
    skills:
      include:
        - code-review
        - frontend-design
    targets:
      - codex
```

## Intended workflow

```bash
# Initialize a personal local catalog
agx init --name personal

# Register the local Catalog so it is available outside this directory
agx catalog add personal --path .

# Lock, review, approve, and deploy
agx lock
agx diff frontend-design
agx audit frontend-design --candidate
agx audit frontend-design
agx approve frontend-design
agx plan
agx apply

# Inspect and update
agx status
agx doctor
agx update --check
agx diff frontend-design
agx audit frontend-design --candidate
agx update frontend-design --accept
agx audit frontend-design
agx approve frontend-design
agx plan
agx apply

# Restore the previous generation
agx rollback
```

## Security principles

Skill instructions, plugins, MCP servers, and global instructions can all influence an agent's tool calls, filesystem access, and network behavior. AGX therefore treats third-party extensions as executable instructions requiring review, not as ordinary text.

- Remote Git sources must be locked to full commit SHAs.
- Unreviewed third-party extensions are denied installation by default.
- Instruction, script, permission, and configuration changes are shown before updates are accepted.
- Credentials remain with system Git, SSH agents, environment variables, or credential managers; they are not stored in catalogs or lockfiles.
- AGX only modifies paths or configuration fields it explicitly owns and never silently overwrites unknown user content.
- Installations are committed as generations, restored on failure, and available for rollback after success.

## Supported agent targets

- Codex: `${CODEX_HOME:-~/.codex}/skills`
- Claude Code: `${CLAUDE_CONFIG_DIR:-~/.claude}/skills`
- Pi: `${PI_CODING_AGENT_DIR:-~/.pi/agent}/skills`
- OpenCode: `${XDG_CONFIG_HOME:-~/.config}/opencode/skills`

Agent support is provided through built-in adapters. AGX will not load external binary adapters until the interface and security boundary have stabilized.

## Implementation direction

AGX is planned as a Go application distributed as a single binary for macOS, Linux, and Windows. The intended milestones are:

1. A local catalog, `local` and `git` sources, Codex, Claude, Pi, and OpenCode adapters, and safe copy-based installation.
2. Store lifecycle and garbage collection, richer overlay patching, diffs and auditing, generations, and rollback.
3. Plugins, MCP servers, instructions, profiles, multi-Catalog composition, and additional agent adapters.
4. Archive sources, signature verification, a public catalog index specification, and package-manager distribution.

## Development

The current scaffold requires Go 1.26 or later and has no third-party runtime dependencies.

```bash
make build       # bin/agx (bin/agx.exe on Windows)
make test        # regular test suite
make check       # race-enabled tests and go vet
make install     # install into the Go binary directory

# Build one release archive locally. GOOS and GOARCH default to the host.
make package VERSION=v0.1.0 GOOS=linux GOARCH=amd64
make checksums

# Create an empty local Catalog, then edit agx.yaml to declare Skills
go run ./cmd/agx init --name personal
go run ./cmd/agx catalog add personal --path .
go run ./cmd/agx catalog list

# With an agx.yaml in the current directory, or an active registered Catalog
go run ./cmd/agx list
go run ./cmd/agx list --catalogs personal,work
go run ./cmd/agx lock           # resolves Git sources when present
go run ./cmd/agx lock --frozen  # performs no Git or network resolution
go run ./cmd/agx doctor         # read-only checks for configured agent targets
go run ./cmd/agx diff review    # locked content versus the current candidate
go run ./cmd/agx audit review   # static audit of locked content
go run ./cmd/agx approve review # local digest-bound approval
go run ./cmd/agx update --check # resolve candidates without changing the lockfile
go run ./cmd/agx update review --accept # accept one candidate into the lockfile
go run ./cmd/agx plan --profile work  # read-only target diff for a Profile
go run ./cmd/agx apply --profile work # transactional copy installation
go run ./cmd/agx apply --catalogs personal,work # compose registered local Catalogs
go run ./cmd/agx status         # active generation and target health
go run ./cmd/agx rollback       # restore the previous snapshotted generation
go run ./cmd/agx repair         # recover an interrupted transaction
```

The test suite includes a binary-level end-to-end workflow covering lock, audit, approve, plan, apply, status, update, and rollback. The GitHub Actions workflow runs this suite on Linux, macOS, and Windows.

The Build workflow runs for `main`, pull requests, and manual dispatches. It cross-compiles downloadable archives for Linux, macOS, and Windows on both amd64 and arm64. Build artifacts are retained for 14 days and use the current Git description as the embedded version.

To publish a release, create and push a semantic version tag:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The Release workflow builds all six platform archives, generates `checksums.txt`, and creates a GitHub Release with generated notes. It can also be started manually with an existing tag; reruns safely replace release assets with the newly built files. The tag value is embedded in the binary and can be checked with `agx version`.

`agx diff <skill>` resolves the Catalog's current revision without changing `agx.lock`. `agx audit <skill>` scans locked content; add `--candidate` to scan the currently resolved candidate. High-risk findings return exit code `4` and block approval unless the user explicitly passes `agx approve <skill> --allow-risk`. Static audit results are risk signals, not a security guarantee.

`agx update --check` resolves candidates for all Skills, reports changed commits and content digests, and leaves the lockfile untouched. `agx update <skill> --accept` atomically updates only the selected lock entry. It never installs the candidate or carries approval forward; Git content must be audited and approved again before `plan` or `apply` succeeds.

## Project boundary

AGX is a global agent extension manager. It is not an agent runtime, a general-purpose plugin platform, a hosted marketplace, or a dotfiles manager.
