# 同步上游 (dualface/kander)

本仓库是 [dualface/kander](https://github.com/dualface/kander) 的 fork, 在其上叠加 DSH agent 支持.

## 分支结构

- `master` — 本仓库主分支: 上游 `develop` + DSH 支持提交
- `fork-dev` — 本地开发分支, 跟踪 `upstream/develop`
- `upstream/develop` — 上游默认分支 (上游 main 分支落后, 以 develop 为准)

## 日常同步

```sh
git fetch upstream
git checkout fork-dev
git merge upstream/develop
# 解决冲突 (DSH 触点: internal/config/agents/dsh.json,
# internal/launch/{session,commands,notify_resume,exec,decompose}.go,
# internal/review/git.go, internal/menu/agents.go, internal/takeover/ops.go,
# i18n 三语 locale)
go build ./... && go test ./internal/i18n/
git checkout master && git merge fork-dev
git push origin master
```

## DSH 触点清单 (合并冲突时重点看)

| 文件 | 内容 |
|---|---|
| `internal/config/agents/dsh.json` | DSH 定义: `args.start/resume/review` + 审核只读 overlay |
| `internal/launch/dshsession.go` | 会话预建 (zstd) + kanban-cwd 反查 |
| `internal/launch/dsh_inject.go` | tmux 下 TUI 就绪后 send-keys 注入 |
| `internal/launch/session.go` | `case "dsh"`: 空会话/`--profile tui --resume` + `DSH_PERMISSION_MODE` |
| `internal/launch/commands.go` | start 预建 + prompt 剥离 + 注入接线 |
| `internal/launch/notify_resume.go` | resume prompt 剥离 |
| `internal/launch/exec.go` | Windows console 批处理独立控制台 |
| `internal/review/git.go` | `usesLastMessageReport` 含 `dsh` (末条消息即报告) |
| `internal/config/config.go` | agent 列表/可执行名/模型默认值 |
| `internal/menu/agents.go` | 标签 "DSH" |
| `internal/takeover/ops.go` | 退出命令 /exit |
| `i18n locales (cn/en/ja)` | `launch.dsh_*` 等 |

## DSH 审核调用契约 (改 `dsh.json` 前先读)

`dsh --profile headless` 只接受**位置参数任务**, 没有 `--model` / `--effort`, 最终
assistant 消息写到 stdout 后退出. 因此:

- `args.review` 只能是 `["--profile","headless","--patch","{prompt_file:overlay}","{instruction}"]`;
  传 `--model` / `--effort` 会被 headless 直接拒绝 (`unknown option`).
- `review.stdin` 必须是 `none` (任务只能走位置参数), `review.output.source` 必须是
  `stdout` (没有 `--output-last-message` 之类的落盘旗标).
- `internal/review/git.go` 的 `usesLastMessageReport` 必须含 `dsh`, 否则审核 prompt 不会
  要求"末条消息即完整报告", 报告解析必然拿不到 `kander-findings` 围栏.
- headless 配置里没有沙箱旗标, 隔离只能靠 `--patch` overlay. 该 overlay 把
  `sandbox-policy.mode` / `permission.defaultPreset` 钉回 `read-only`, 并把
  `approval.policy` 设为 `never` (非 danger 模式默认 `ask`, 一次性无 TTY 进程会挂死).
  注意 `--patch` 是**整节点替换**, 所以 `permission.presets` 必须原样重复三个预设.
- `models.review.dsh` / `models.kanban.dsh` 到不了 DSH 进程: DSH 的模型来自
  `$DSH_HOME/settings.yaml` 的 `agent-default-model` 段 (设置层是活真源, 会覆盖组合配置).
  想按角色选模型, 目前只能改 `~/.dsh/settings.yaml`.
