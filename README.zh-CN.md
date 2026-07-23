# AGX

[English](README.md) | **简体中文**

> Agent Extensions — 面向 AI 编程 Agent 的跨平台全局扩展管理器。

AGX 通过统一的 Catalog、锁文件、内容存储、Agent Adapter 和事务式安装流程，管理并向多个 AI 编程 Agent 分发用户级全局扩展。

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

## 项目状态

AGX 目前处于早期设计和实现阶段，CLI、Catalog Schema 和安装流程尚未对外稳定。本 README 描述的是目标架构，不代表所有功能已经可用。

首个实现里程碑会刻意缩小范围：单个本地 Catalog、`local`/`git` Skill 来源、Codex/Claude Code Adapter、复制安装，以及 `list`、`lock`、`plan`、`apply`、`status`、`doctor` 命令。Plugins、MCP Servers、Instructions、Profiles 和多 Catalog 仍属于目标模型，但不是 Milestone 1 的交付承诺。

当前原型已经可以加载并锁定单个 Catalog、诊断 Codex/Claude Code target、生成只读计划、通过 copy 模式的 `agx apply` 安装本地或 Git 来源的 Skills，并使用 `agx status` 检查当前 generation。Apply 会先完成全部 staging，再切换目标；受管理目录会先备份，失败时回滚，并在平台状态目录中记录 generation。Status 可以识别缺失或被外部修改的受管理目标，以及未完成的事务。未知现有目标仍默认视为冲突，只有内容完全一致时才能使用 `--adopt`；被外部修改的受管理目标不会被静默覆盖。用户主动回滚命令尚未实现。

## 为什么需要 AGX

不同 Agent 使用不同的全局目录、配置格式和扩展机制。直接在每个 Agent 中分别维护会导致：

- Skills 和 Instructions 逐渐漂移。
- 第三方扩展的来源和版本难以追踪。
- Plugin 或 MCP 更新时缺少差异展示与审查流程。
- 更换机器或操作系统后无法准确复现环境。
- Agent 原生配置、个人内容和第三方依赖相互混杂。

AGX 将这些全局扩展声明为一份可锁定、可审查、可回滚的期望状态，再由 Adapter 投影到各个 Agent。

## 管理范围

AGX 计划管理：

- **Skills**：本地、公开 Git 或私有 Git 来源的工作流单元。
- **Plugins**：由特定 Agent Adapter 安装和验证的原生扩展。
- **MCP Servers**：统一声明，并按 Agent 的全局配置格式进行语义级合并。
- **Instructions**：生成个人级 `AGENTS.md`、`CLAUDE.md` 等全局指令入口。
- **Profiles**：为不同场景选择需要部署的扩展集合。

AGX 不计划接管：

- Agent 账户、API Token、SSH 私钥或其他凭据内容。
- 默认模型、主题、UI 偏好和对话历史。
- 通用 dotfiles、Shell、Git 或编辑器配置。
- 项目级业务依赖和通用开发环境包。

## 核心模型

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

- **Catalog** 描述用户期望的全局扩展状态。
- **Lockfile** 将 Git branch 或 tag 解析为不可变 commit，并记录内容摘要。
- **Store** 以内容哈希保存不可变源对象。
- **Adapter** 处理不同 Agent 的路径、能力、渲染与配置合并差异。
- **Generation** 记录一次完整部署，用于状态检查和回滚。

## 目标 Catalog 示例

> 以下内容展示目标模型，其中包含当前 Milestone 1 解析器尚不接受的字段。当前可执行的子集以 `schemas/catalog.schema.json` 为准。

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

## 预期工作流

```bash
# 初始化或注册个人 Catalog
agx init
agx catalog add personal git@github.com:user/my-agx.git

# 锁定、审查并部署
agx lock
agx audit
agx plan --profile default
agx apply --profile default

# 检查和更新
agx status
agx doctor
agx update --check

# 恢复上一个 generation
agx rollback
```

## 安全原则

Skill 指令、Plugin、MCP 和全局 Instructions 都可能影响 Agent 的工具调用、文件访问和网络行为。AGX 因此将第三方扩展视为需要审查的可执行指令，而不是普通文本。

- 远程 Git 来源必须锁定到完整 commit SHA。
- 未审查的第三方扩展默认不允许安装。
- 更新前展示指令、脚本、权限和配置差异。
- 凭据交给系统 Git、SSH Agent、环境变量或凭据管理器，不写入 Catalog 和 Lockfile。
- 只修改 AGX 明确拥有的路径或配置字段，不静默覆盖未知用户内容。
- 安装以 generation 为单位进行，失败时恢复，成功后可回滚。

## 计划支持的 Agent

- Codex
- Claude Code
- OpenCode
- Pi

Agent 的支持由内置 Adapter 提供。在接口和安全边界稳定前，AGX 不加载外部二进制 Adapter。

## 实现方向

AGX 计划使用 Go 实现，以单一二进制提供 macOS、Linux 和 Windows 支持。预期的主要阶段包括：

1. 本地 Catalog、`local`/`git` 来源、Codex/Claude Adapter 和安全复制安装。
2. 内容寻址 Store、差异与审计、Overlay、Generation 和回滚。
3. Plugins、MCP、Instructions、Profiles、多 Catalog 及更多 Agent Adapter。
4. Archive 来源、签名验证、公共 Catalog 索引和包管理器分发。

## 开发

当前代码骨架要求 Go 1.26 或更高版本，并且没有第三方运行时依赖。

```bash
go test ./...
go build ./cmd/agx

# 当前目录中已有 agx.yaml 时
go run ./cmd/agx list
go run ./cmd/agx lock           # 存在 Git 来源时进行解析
go run ./cmd/agx lock --frozen  # 不执行 Git 或网络解析
go run ./cmd/agx doctor         # 只读检查 Codex/Claude target
go run ./cmd/agx plan           # 只读目标差异预览
go run ./cmd/agx apply          # 事务式 copy 安装
go run ./cmd/agx status         # 当前 generation 和目标健康状态
```

## 项目边界

AGX 是全局 Agent 扩展管理器，不是 Agent 运行时、通用插件平台、Marketplace 服务或 dotfiles 管理器。
