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

The first implementation milestone is intentionally narrower: one local catalog, `local` and `git` skill sources, Codex and Claude Code adapters, copy-based installation, and the `list`, `lock`, `plan`, `apply`, `status`, and `doctor` commands. Plugins, MCP servers, instructions, profiles, and multiple catalogs remain part of the target model but are not Milestone 1 commitments.

The current prototype can strictly load a single `agx.yaml`, list declared skills, lock local Skill directories by content digest, write `agx.lock` atomically, and verify local state with `agx lock --frozen`. Git resolution and installation commands are not implemented yet.

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
- **Store** keeps immutable source objects addressed by content hash.
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
    scope: user
    sources:
      - instructions/common.md
      - instructions/coding.md
      - instructions/safety.md
    targets:
      codex: {}
      claude: {}

profiles:
  default:
    skills:
      - code-review
      - frontend-design
    plugins:
      - example-plugin
    mcpServers:
      - github
    instructions:
      - personal
```

## Intended workflow

```bash
# Initialize or register a personal catalog
agx init
agx catalog add personal git@github.com:user/my-agx.git

# Lock, review, and deploy
agx lock
agx audit
agx plan --profile default
agx apply --profile default

# Inspect and update
agx status
agx doctor
agx update --check

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

## Planned agent support

- Codex
- Claude Code
- OpenCode
- Pi

Agent support is provided through built-in adapters. AGX will not load external binary adapters until the interface and security boundary have stabilized.

## Implementation direction

AGX is planned as a Go application distributed as a single binary for macOS, Linux, and Windows. The intended milestones are:

1. A local catalog, `local` and `git` sources, Codex and Claude adapters, and safe copy-based installation.
2. A content-addressed store, diffs and auditing, overlays, generations, and rollback.
3. Plugins, MCP servers, instructions, profiles, multiple catalogs, and additional agent adapters.
4. Archive sources, signature verification, a public catalog index specification, and package-manager distribution.

## Development

The current scaffold requires Go 1.26 or later and has no third-party runtime dependencies.

```bash
go test ./...
go build ./cmd/agx

# With an agx.yaml in the current directory
go run ./cmd/agx list
go run ./cmd/agx lock
go run ./cmd/agx lock --frozen
```

## Project boundary

AGX is a global agent extension manager. It is not an agent runtime, a general-purpose plugin platform, a hosted marketplace, or a dotfiles manager.
