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

当前已经实现的 Skill 范围包括：本地 Catalog 注册表和单个 active Catalog、Profiles、内容寻址 Store、`local`/`git` 来源、Codex、Claude Code、Pi、OpenCode Adapter、复制安装、Overlay，以及 `init`、`catalog`、`store`、`list`、`lock`、`plan`、`apply`、`status`、`rollback`、`repair`、`diff`、`audit`、`approve`、`update`、`doctor` 命令。Plugins、MCP Servers、Instructions、远程 Catalog 和多 Catalog 合并仍属于目标模型，尚未实现。

当前原型已经可以初始化、注册、选择、加载并锁定本地 Catalog、检查并选择性接受来源更新、应用确定性的 Overlay、比较锁定内容和候选 Skill、执行静态风险审计、保存与摘要绑定的本机审批，并通过 copy 模式的 `agx apply` 安装本地或已审批的 Git 来源 Skills。当前 Overlay 支持对 `SKILL.md` 追加前置/后置内容，以及禁用指定脚本；尚未支持的 rename 和目标私有 metadata 会明确拒绝。Git Skill 默认处于未审查状态：只有 `agx approve` 为精确 commit、内容摘要、Overlay 摘要、Adapter 安全版本和策略摘要记录审批后，`plan` 和 `apply` 才会继续；任一绑定值变化（包括接受更新）都会使审批失效。Status 和 doctor 可以识别未完成的事务；当 journal 记录的内容摘要仍然匹配时，`agx repair` 会安全完成补偿回滚。未知现有目标仍默认视为冲突，只有内容完全一致时才能使用 `--adopt`；被外部修改的受管理目标不会被静默覆盖。引入回滚快照之前创建的 generation 无法恢复。GitHub Actions 会在 Linux、macOS 和 Windows 上执行原生测试与构建，并在 Linux 上运行 race 检查和 vet。

本地 Skill 和 overlay 路径可以使用相对于 Catalog 的路径、绝对路径，或者以 `~/` 表示当前用户家目录。例如，在类 Unix 系统中可以使用 `skills/review`、`~/agent-skills/review` 和 `/opt/agent-skills/review`；Windows 也支持 `C:\agent-skills\review` 这样的原生绝对路径。AGX 不展开环境变量，也不支持 `~alice` 这样的其他用户家目录。Git 来源的 `path` 仍必须相对于仓库根目录。由于原始路径表达式会保存在 `agx.lock` 中，多台机器共用同一 Catalog 时，建议优先使用 `~/`，避免使用绑定单台机器的绝对路径。

`agx init --name personal` 会创建一个非破坏性的空 `agx.yaml`，以及 `skills/`、`overlays/` 目录；如果 Catalog 已存在则拒绝覆盖。空 Catalog 可以正常执行 list 和 lock；当空 Catalog 会移除已管理的 Skills 时，`plan` 和 `apply` 必须显式传入 `--allow-empty`。

本地 Catalog 可以注册后在任意目录选择使用：

```bash
agx catalog add personal --path ~/my-agx
agx catalog add work --path ~/work/agx.yaml
agx catalog list
agx catalog use work
agx catalog remove personal
```

第一条注册记录会自动成为 active Catalog。`remove` 只删除注册信息，不会删除 Catalog 文件。支持 `--catalog` 的命令按以下顺序解析 Catalog：显式 `--catalog`、当前目录的 `agx.yaml`、注册表中的 active Catalog。`agx init` 有意保持不同语义：未传 `--catalog` 时始终创建 `./agx.yaml`。注册表保存在操作系统用户配置目录下的 `agx/catalogs.yaml`；可以用绝对路径环境变量 `AGX_CONFIG_HOME` 覆盖该目录。注册表目前只接受本地文件或目录，只选择一个 active Catalog，不会获取远程 Catalog 仓库，也不会合并多个 Catalog。

Profile 用于选择部署子集，不改变 lockfile 锁定的范围。`skills.include` 非空时从列出的 Skills 开始；未声明 `include` 时默认选择 Catalog 中的全部 Skills；随后移除 `skills.exclude`，最后将 `targets` 与每个 Skill 自身启用的 targets 取交集。Catalog 加载时会校验 Profile 名称和所有引用。

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

可以使用 `agx list --profile work`、`agx plan --profile work` 和 `agx apply --profile work`。`agx lock` 仍锁定完整 Catalog，因此切换 Profile 不会丢失来源解析结果。应用 Profile 时，其选中集合会成为完整期望状态，之前由 AGX 管理但不再入选的目标会被移除。如果 Profile 未选中任何内容且当前存在已管理 Skill，`plan` 和 `apply` 仍要求显式传入 `--allow-empty`。新 generation 和 `agx status` 会记录所用 Profile。

`agx lock` 会按 SHA-256 摘要保存精确的原始 Skill 和 Overlay 目录。锁定内容的审查、规划、审批和安装会校验并物化这些不可变对象，不再重复拉取 Git 仓库，也不依赖之后可能消失的本地来源目录。候选内容审查和更新检查仍解析实时来源。实时本地来源或 Overlay 一旦发生变化，仍视为 lock drift，需要重新执行 `agx lock`；来源仅仅缺失时，可以使用已验证的 Store 对象。损坏对象会被明确拒绝，不会静默修复。Store 默认位于 AGX 状态目录下的 `store/`，也可以通过绝对路径环境变量 `AGX_STORE_HOME` 调整位置。自动垃圾回收尚未实现。

Store 维护命令默认先报告，再执行变更：

```bash
agx store status
agx store verify
agx store gc --dry-run
agx store gc
agx store gc --prune-stale --force
```

每次成功写入 lockfile 或接受更新，都会记录 lockfile 引用的内容摘要。GC 只删除未被这些引用记录使用的对象。lockfile 暂时不可用时，其引用默认保留；只有显式传入 `--prune-stale` 才会移除。没有任何 active lock 引用时，删除对象还必须显式传入 `--force`。尚未实现后台自动垃圾回收。

当前 Overlay 通过在 Skill 中增加 `overlay: overlays/review` 声明，并在 `overlays/review/overlay.yaml` 中使用以下格式：

```yaml
apiVersion: agx.dev/v1alpha1
kind: Overlay

content:
  prepend: prepend.md
  append: append.md

disableScripts:
  - scripts/upload-result.sh
```

引用的 Markdown 会应用到 `SKILL.md`，被禁用的脚本不会进入最终安装产物。写入 `agx.lock` 前，AGX 会验证 Overlay manifest、其引用文件，以及 Overlay 是否能应用到已解析的 Skill。`agx.lock` 分别记录原始来源摘要和 Overlay 摘要，审查与部署则针对应用 Overlay 后的最终结果。

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
- **Store** 以内容哈希保存经过校验的不可变 Skill 和 Overlay 目录。
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
# 初始化个人本地 Catalog
agx init --name personal

# 注册本地 Catalog，使其在当前目录之外也可用
agx catalog add personal --path .

# 锁定、审查、审批并部署
agx lock
agx diff frontend-design
agx audit frontend-design --candidate
agx audit frontend-design
agx approve frontend-design
agx plan
agx apply

# 检查和更新
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

## 已支持的 Agent Target

- Codex：`${CODEX_HOME:-~/.codex}/skills`
- Claude Code：`${CLAUDE_CONFIG_DIR:-~/.claude}/skills`
- Pi：`${PI_CODING_AGENT_DIR:-~/.pi/agent}/skills`
- OpenCode：`${XDG_CONFIG_HOME:-~/.config}/opencode/skills`

Agent 的支持由内置 Adapter 提供。在接口和安全边界稳定前，AGX 不加载外部二进制 Adapter。

## 实现方向

AGX 计划使用 Go 实现，以单一二进制提供 macOS、Linux 和 Windows 支持。预期的主要阶段包括：

1. 本地 Catalog、`local`/`git` 来源、Codex、Claude、Pi、OpenCode Adapter 和安全复制安装。
2. Store 生命周期和垃圾回收、更丰富的 Overlay patch、差异与审计、Generation 和回滚。
3. Plugins、MCP、Instructions、Profiles、多 Catalog 组合及更多 Agent Adapter。
4. Archive 来源、签名验证、公共 Catalog 索引和包管理器分发。

## 开发

当前代码骨架要求 Go 1.26 或更高版本，并且没有第三方运行时依赖。

```bash
make build       # 生成 bin/agx（Windows 为 bin/agx.exe）
make test        # 运行常规测试
make check       # 运行 race 测试和 go vet
make install     # 安装到 Go 二进制目录

# 在本地生成单个平台发布包；GOOS 和 GOARCH 默认使用当前主机值
make package VERSION=v0.1.0 GOOS=linux GOARCH=amd64
make checksums

# 创建空的本地 Catalog，然后编辑 agx.yaml 声明 Skills
go run ./cmd/agx init --name personal
go run ./cmd/agx catalog add personal --path .
go run ./cmd/agx catalog list

# 当前目录中已有 agx.yaml，或者已经选择 active Catalog 时
go run ./cmd/agx list
go run ./cmd/agx lock           # 存在 Git 来源时进行解析
go run ./cmd/agx lock --frozen  # 不执行 Git 或网络解析
go run ./cmd/agx doctor         # 只读检查 Catalog 中配置的 Agent target
go run ./cmd/agx diff review    # 比较锁定内容和当前候选内容
go run ./cmd/agx audit review   # 静态审计锁定内容
go run ./cmd/agx approve review # 与摘要绑定的本机审批
go run ./cmd/agx update --check # 解析候选内容但不修改 lockfile
go run ./cmd/agx update review --accept # 将一个候选版本写入 lockfile
go run ./cmd/agx plan --profile work  # 只读展示 Profile 的目标差异
go run ./cmd/agx apply --profile work # 事务式 copy 安装
go run ./cmd/agx status         # 当前 generation 和目标健康状态
go run ./cmd/agx rollback       # 恢复上一个已保存快照的 generation
go run ./cmd/agx repair         # 恢复被中断的事务
```

测试套件包含二进制级端到端流程，覆盖 lock、audit、approve、plan、apply、status、更新和 rollback。GitHub Actions 会在 Linux、macOS 和 Windows 上运行这套测试。

Build workflow 会在 `main`、Pull Request 和手动触发时运行，为 Linux、macOS、Windows 的 amd64 与 arm64 构建可下载压缩包。构建产物保留 14 天，二进制版本使用当前 Git 描述信息。

发布版本时，创建并推送语义化版本标签：

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

Release workflow 会构建六个平台压缩包、生成 `checksums.txt`，并创建带自动生成说明的 GitHub Release。也可以通过已有标签手动触发；重新运行时会安全替换该 Release 的附件。标签值会注入二进制，可通过 `agx version` 检查。

`agx diff <skill>` 会解析 Catalog 当前 revision，但不会修改 `agx.lock`。`agx audit <skill>` 默认扫描锁定内容；增加 `--candidate` 可扫描当前解析出的候选内容。高风险发现会返回退出码 `4`，并阻止审批，除非用户明确传入 `agx approve <skill> --allow-risk`。静态审计只能提供风险信号，不能保证内容安全。

`agx update --check` 会解析所有 Skill 的候选版本，报告 commit 和内容摘要变化，但不会修改 lockfile。`agx update <skill> --accept` 只会原子更新选中 Skill 的 lock 条目，不会自动安装，也不会继承旧审批；Git 内容必须重新 audit 和 approve，之后 `plan` 和 `apply` 才会继续。

## 项目边界

AGX 是全局 Agent 扩展管理器，不是 Agent 运行时、通用插件平台、Marketplace 服务或 dotfiles 管理器。
