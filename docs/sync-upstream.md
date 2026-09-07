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
# 解决冲突 (DSH 触点: internal/launch/{session,commands,notify_resume,exec}.go,
# internal/review/{args,settings,execute}.go, internal/config/config.go,
# internal/menu/agents.go, internal/takeover/ops.go, i18n 三语 locale)
go build ./... && go test ./internal/i18n/
git checkout master && git merge fork-dev
git push origin master
```

## DSH 触点清单 (合并冲突时重点看)

| 文件 | 内容 |
|---|---|
| `internal/launch/dshsession.go` | 会话预建 (zstd) + kanban-cwd 反查 |
| `internal/launch/dsh_inject.go` | tmux 下 TUI 就绪后 send-keys 注入 |
| `internal/launch/session.go` | `case "dsh"`: 空会话/`--profile tui --resume` |
| `internal/launch/commands.go` | start 预建 + prompt 剥离 + 注入接线 |
| `internal/launch/notify_resume.go` | resume prompt 剥离 |
| `internal/launch/exec.go` | Windows console 批处理独立控制台 |
| `internal/review/args.go` | headless 审核参数 + codex 同路解析 |
| `internal/review/settings.go` | DSH 审核定义 |
| `internal/review/execute.go` | 输出文件重定向豁免 |
| `internal/config/config.go` | agent 列表/可执行名/模型默认值 |
| `internal/menu/agents.go` | 标签 "DSH" |
| `internal/takeover/ops.go` | 退出命令 /exit |
| `i18n locales (cn/en/ja)` | `launch.dsh_*`, `review.dsh_*` 等 |
