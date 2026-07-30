<div align="center">

# ✻ sm

**AI 会话管理器** — 把本机所有 **Claude Code** 与 **OpenAI Codex**
对话收进一个终端界面:按项目分组、实时预览,一次按键回到你上次停下的
地方。

[![Release](https://img.shields.io/github/v/release/dukechain2333/ai-sessions-manager?sort=semver&color=D97757)](https://github.com/dukechain2333/ai-sessions-manager/releases)
[![CI](https://github.com/dukechain2333/ai-sessions-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/dukechain2333/ai-sessions-manager/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/dukechain2333/ai-sessions-manager)](go.mod)
![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-blue)
[![License: MIT](https://img.shields.io/github/license/dukechain2333/ai-sessions-manager?color=green)](LICENSE)

[English](README.md) · **简体中文**

</div>

---

```
✻ sm · AI Sessions   52 sessions
 > filter…
╭────────────────────────────────────╮╭─────────────────────────────────────╮
│ ▾ ai-sessions-manager (1)          ││ > So currently my Claude Code       │
│ ▶ Build session history web app    ││   sessions are dispersed among      │
│   ai-sessions-manager · just now   ││   different dirs…                   │
│ ▾ HyperSAGNN_Interaction (4)       ││ ⏺ Using superpowers:brainstorming   │
│   Experiment with top 3 fit …      ││   to explore the design…            │
│   HyperSAGNN_Interaction · 4h ago  ││                                     │
│ ▸ william (12)                     ││ ⎿ Skill: superpowers:brainstorming  │
│ ▸ prs-net (2)                      ││                                     │
╰────────────────────────────────────╯╰─────────────────────────────────────╯
 ↵ resume  tab focus  n new  d delete  / filter  a agent  g group  q quit
```

编码 agent 的会话以 `.jsonl` 文件的形式散落在几十个目录里——只要你在
多个地方干过活,就再也找不全它们。`sm` 是一个单二进制 TUI,把这些会话
收进一个可浏览、可搜索的列表,并能把你带回任何一段对话——回到会话
最初所在的那个目录里继续。

> 中文文档与英文版同步维护;若有出入,以 [英文版](README.md) 为准。

## 功能

- **一个列表,所有项目** — 扫描 `~/.claude/projects`(存在时还包括
  `~/.codex/sessions`),按项目分组、分组头可折叠,按最近使用排序。
- **实时对话预览**、**模糊过滤**,以及覆盖所有会话全部消息的
  **全文搜索**。
- **在正确的地方恢复** — 在会话最初的目录里执行
  `claude --resume <id>`(或 `codex resume <id>`),即使会话后来
  `cd` 去了别处。
- **新建会话**可在任何已知项目目录发起;**安全删除**(文件移入
  `.trash/`,绝不 `rm`)。
- **两种视图** — 混合列表或按 agent 分标签页(`v` 切换),每个 agent
  有自己的主题色。
- **可选的 [tmux 集成](#tmux-集成)** — 启动跑在可分离的 tmux 会话里,
  带实时 `●` 标记和一键终止。
- **[真·系统窗口](#在新窗口中打开启动)** — 配置 `open_in: "window"`
  后,启动会打开原生 iTerm2 / Ghostty 窗口或 Warp 标签页,`sm` 原地
  不动;本地与 SSH 远程均可用。
- 单个静态二进制(macOS 与 Linux,Intel 与 Apple Silicon),零运行时
  依赖。

## 安装

`sm` 需要 [Claude Code](https://claude.com/claude-code) CLI(`claude`)
在 `PATH` 上,才能真正恢复会话。

**Homebrew(macOS 与 Linux)**

```sh
brew install dukechain2333/tap/sm
```

**APT 仓库(Debian / Ubuntu,amd64 与 arm64)** — 添加一次,之后像
普通软件包一样升级:

```sh
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://dukechain2333.github.io/ai-sessions-manager/public.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/ai-sessions-manager.gpg
echo "deb [signed-by=/etc/apt/keyrings/ai-sessions-manager.gpg] https://dukechain2333.github.io/ai-sessions-manager stable main" \
  | sudo tee /etc/apt/sources.list.d/ai-sessions-manager.list
sudo apt update && sudo apt install ai-sessions-manager
```

> 包名是 `ai-sessions-manager`(安装的命令是 `sm`),因为 Ubuntu
> 官方源里已经存在名为 `sm` 的包。

**安装脚本** — 把最新 release 二进制放进 `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/install.sh | sh
# 选项:… | sh -s -- --version v0.3.0 --bin /usr/local/bin
```

**其他方式** —
[releases 页面](https://github.com/dukechain2333/ai-sessions-manager/releases)
提供独立的 `.deb` / `.rpm` 包(`sudo apt install ./<file>.deb`、
`sudo rpm -i <file>.rpm`)以及各平台的 `tar.gz` 纯二进制。也可以从源码
构建(Go ≥ 1.24):`git clone` 本仓库后 `make install`。

<details>
<summary><b>Beta 版本</b></summary>

预发布版本(`v*-beta.*`)不会进入上面的稳定渠道。想尝鲜:

```sh
# Homebrew — beta 是独立的 cask(先卸载稳定版):
brew uninstall sm 2>/dev/null; brew install --cask dukechain2333/tap/sm-beta

# Debian / Ubuntu — 直接安装 release 页面上的 .deb:
curl -sLO https://github.com/dukechain2333/ai-sessions-manager/releases/download/v0.5.0-beta.3/ai-sessions-manager_0.5.0-beta.3_linux_amd64.deb
sudo apt install ./ai-sessions-manager_0.5.0-beta.3_linux_amd64.deb
```

包的版本号形如 `0.5.0~beta.N`,在 Debian 的排序规则里位于 `0.5.0`
*之前*——稳定版进入 APT 仓库后,一次普通的 `apt upgrade` 就会自动
替换掉 beta。Homebrew 上切回稳定版:
`brew uninstall --cask sm-beta && brew install dukechain2333/tap/sm`。

</details>

## 使用

```sh
sm                      # 浏览 ~/.claude/projects(存在时还包括 ~/.codex/sessions)
sm --projects-dir DIR   # 指定其他 Claude Code 目录
sm --codex-dir DIR      # 指定其他 Codex 会话目录
sm --config PATH        # 指定其他 config.json
sm ssh HOST             # ssh + 原生窗口桥(Ghostty/Warp),见"真·系统窗口"
sm --version
```

### 按键

| 按键 | 动作 |
|---|---|
| `↑/↓` `j/k` | 移动选中项(到边缘会响铃;顶部再按 `↑` 进入过滤栏) |
| `enter` | 恢复选中的会话;在项目分组头上则折叠/展开 |
| `space` | 折叠 / 展开当前项目分组 |
| `g` | 切换:按项目分组 ⇄ 按时间平铺 |
| `v` | 切换视图:混合列表 ⇄ 按 agent 分标签页 |
| `a` | 列表模式:项目内 agent 子分组开/关;标签页模式:切换 Claude ⇄ Codex |
| `tab` | 聚焦预览栏(滚动长对话)与返回 |
| `/` | 模糊过滤(enter 保留,esc 清除) |
| `s` | 全文搜索 |
| `n` | 在选定目录新建会话(两个 agent 都装了时会询问用哪个) |
| `d` | 删除选中会话(移入 `.trash/`) |
| `x` | 终止选中会话的 tmux;在项目分组头上则终止该项目全部(需确认) |
| `e` | 显示 / 隐藏"空"会话(只有 hook、没有真实提问的) |
| `r` | 重新扫描 |
| `,` | 设置(在应用内编辑 `config.json`;保存后重启生效) |
| `q` | 退出 |

### 鼠标

处处可点:单击选中(双击恢复),分组头可折叠,滚轮移动选中项或滚动
预览,帮助栏动作和对话框按钮都是真按钮。设置面板同样如此:点行选中,
点值修改(复选框切换、`◂` 反向循环枚举、文本项打开行内编辑器),滚轮
移动,帮助行里的 `s save` / `esc close` 也是按钮。开启鼠标上报后,用
**Shift+拖选** 选取文本(鼠标类 TUI 的通行做法)。

### 搜索

过滤栏有两层 — **Tab** 在两层间切换(`/` 直接聚焦模糊过滤层,`s`
直接聚焦全文搜索层):

- **filter…** — 对标题、项目名、首条提问做模糊匹配。
- **search…** — 对所有会话的全部内容做全文搜索。空格分隔的词必须
  全部命中(AND)。结果按命中次数排序;预览跳到第一处命中,预览聚焦
  时 `n` / `N` 在命中间跳转。

首次搜索会在用户缓存目录下建立纯文本索引(`sm-index/`);之后只对有
变化的会话增量重建。

### 恢复会话,以及找回删掉的会话

`enter` 会挂起 `sm`,在会话最初的目录里运行 agent,agent 退出后回到
列表。Claude 第一次打开某个目录时可能会问 **"Is this a project you
trust?"** —— 那是 Claude Code 自己的安全门,不是 `sm`。

删除只是移动。恢复一个会话:

```sh
mv ~/.claude/projects/.trash/<project-slug>/<id>.jsonl \
   ~/.claude/projects/<project-slug>/
```

## 配置

首次运行时 `sm` 会把下面的默认 `config.json` 写到
`$XDG_CONFIG_HOME/sm/config.json`(通常是 `~/.config/sm/config.json`);
用 `--config` 可指向别处。改不改随意,`sm` 绝不覆盖你的修改,文件格式
损坏时回落到默认值并给出提示。

也可以在 `sm` 里按 `,` 打开设置面板编辑所有配置项——保存会以规范格式
重写 `config.json`;改动在下次启动 `sm` 时生效。

```json
{
  "view": "list",
  "open_in": { "mode": "current", "iterm2": { "ssh": "" } },
  "tmux": { "enabled": false },
  "colors": {
    "claude": { "light": "#C15F3C", "dark": "#D97757" },
    "codex":  { "light": "#0A7C66", "dark": "#10A37F" }
  }
}
```

| 配置项 | 取值 | 作用 |
|---|---|---|
| `view` | `"list"`(默认)/ `"tabs"` | 启动时的视图模式;`v` 可随时切换 |
| `open_in.mode` | `"current"`(默认)/ `"window"` | `"current"` 挂起 `sm`、在当前终端运行 agent;`"window"` 让每次启动都开一个[新窗口](#在新窗口中打开启动),`sm` 留在屏幕上。简写:`"open_in": "window"` |
| `open_in.iterm2.ssh` | ssh 目标 | 仅用于 `sm` 跑在 SSH 远端时的 iTerm2 开窗——填你在 Mac 上 `ssh` 后面敲的那个目标。见下文 |
| `tmux.enabled` | `false`(默认)/ `true` | 启动跑在名为 `sm-<agent>-<id8>` 的 tmux 会话里,断开也不丢工作;带来 `●` 标记和 `x` 终止键。需要 `tmux` 在 `PATH` 上 |
| `colors.claude` / `colors.codex` | `{"light","dark"}` 十六进制 | 各 agent 的主题色 |

`open_in` 与 `tmux.enabled` 可以组合:tmux 开启时,开窗启动是受跟踪的
(`●`、`x`、回车重进);关闭时,窗口不受跟踪。

## 终端支持

TUI 本体在任何终端里都能跑,包括 tmux 里——浏览、搜索、
`open_in: "current"`(在当前终端里恢复)处处可用,不需要下面的任何
东西。

当启动需要开到**别处**(`open_in: "window"`)时,你用的终端才开始有
区别。`sm` 会自动探测自己运行所在的终端——无需任何配置——并选用它
所支持的最佳机制:

| 终端 | 平台 | 启动开成什么 | 机制 | 重复启动 |
|---|---|---|---|---|
| **iTerm2** | macOS | 原生窗口 | 私有转义序列 → AutoLaunch 桥接脚本 | 聚焦已开窗口 |
| **Ghostty** | macOS 1.3+、Linux 1.2+ | 原生窗口 | AppleScript / `ghostty +new-window` | 聚焦(macOS);新开窗口(Linux) |
| **Warp** | macOS 与 Linux,仅 Stable | 最前窗口的原生**标签页** | Tab Config 文件 + `warp://` URI | 新开标签页(Warp 不返回可聚焦的句柄) |
| 其他任意终端 | 任意 | tmux 窗口 | `sm` 把自己包进一个 tmux 会话 | 跳到已有窗口 |

值得了解:

- 普通 `ssh` 之下,只有 iTerm2 能把窗口开回你的桌面——它的转义序列
  随连接传输。Ghostty 和 Warp 请改用 **`sm ssh <host>`** 连接:同样的
  ssh 会话,外加一条开窗桥。
- 在 tmux 里,启动仍会路由到"拥有"这个 tmux 的终端:环境标记会被
  继承,所以在某个终端里启动的 tmux server,即使换了终端 attach,
  启动仍开回原来的终端。
- tmux 跟踪开启时,重复启动会收敛进同一个 tmux 会话,不管有多少窗口
  或标签页镜像着它。
- Warp Preview 不受支持(URI scheme 与配置目录都不同);请用 Warp
  Stable。

配置项与一次性设置见下一节;深入内容——各机制的原理、排障、安全
模型——见 [docs/native-windows.md](docs/native-windows.md)(英文)。

## 在新窗口中打开启动

配置 `"open_in": "window"` 后,恢复/新建会开出**真正的终端窗口**——
`sm` 原地不动。开出什么样的窗口取决于你的终端:

| 你的终端 | 本地 | SSH 远程 | 一次性设置 |
|---|---|---|---|
| **iTerm2**(macOS) | 原生窗口 | 在 Mac 上开原生窗口 | [安装 AutoLaunch 脚本](docs/native-windows.md#iterm2-macos);SSH 场景还需设置 `iterm2.ssh` |
| **Ghostty**(macOS 1.3+、Linux 1.2+) | 原生窗口 | 在桌面开原生窗口 | 本地无需任何设置;SSH 场景改用 **`sm ssh <host>`** 连接即可 |
| **Warp**(macOS 与 Linux) | 原生标签页 | 在桌面开原生标签页 | 本地无需任何设置;SSH 场景改用 **`sm ssh <host>`** 连接即可 |
| 其他任意终端 | tmux 窗口 | tmux 窗口 | `tmux` 在 `PATH` 上(`sm` 自动把自己包进名为 `sm` 的 tmux 会话) |

最常用的极简配置:

```json
{ "open_in": "window" }
```

对本地 iTerm2、本地 Ghostty、本地 Warp、`sm ssh` 下的 Ghostty 或
Warp、以及 tmux 回退,开箱即用。只有 iTerm2-over-SSH 需要填反连目标:

```json
{ "open_in": { "mode": "window", "iterm2": { "ssh": "myserver" } } }
```

所有原生模式下,窗口里跑的都是与别处相同的受跟踪 tmux 会话(`●`、
`x`、回车重进),关窗口不会杀会话,重复启动会聚焦仍开着的窗口。
**完整指南——设置步骤、各机制原理、排障与安全模型——见
[docs/native-windows.md](docs/native-windows.md)(英文)。**

## tmux 集成

- 有存活 tmux 的会话显示 `●` 标记;项目分组头在其任一会话存活时也
  显示 `●`。
- `x` 终止选中会话的 tmux;在项目分组头上则终止该项目的全部(需
  确认)。在 `sm` 之外做的终止也会被自动察觉。
- 已知边缘情况:**新建**会话的 tmux 是在下次扫描时按"该目录里最新的
  会话"来关联列表行的;如果回到列表前在同一目录连开两个新会话,两者
  的标注可能互换(都仍可从项目分组头终止)。

## 卸载

```sh
brew uninstall sm                # homebrew
sudo apt remove ai-sessions-manager  # apt / deb
sudo rpm -e ai-sessions-manager  # rpm
rm -f ~/.local/bin/sm            # 脚本 / 手动安装
```

## 开发

```sh
make test    # go test ./...
make vet     # go vet ./...
make build   # ./sm
```

架构:`internal/store` 是与 UI 无关的会话 `.jsonl` 读取层;
`internal/ui` 是基于
[Bubble Tea](https://github.com/charmbracelet/bubbletea) 的 TUI;
`internal/bridge` + `internal/ghostty` + `internal/warp` +
`scripts/iterm2/` 实现[原生开窗启动器](docs/native-windows.md)。设计
文档在 `docs/` 下。

发版全自动:推送 `v*` 标签即触发
[GoReleaser](https://goreleaser.com) —— 二进制、`.deb`/`.rpm`、GitHub
Release、Homebrew tap 与 APT 仓库全部出自这一个标签(预发布标签只进
[beta 渠道](#安装))。

## 许可证

[MIT](LICENSE)
