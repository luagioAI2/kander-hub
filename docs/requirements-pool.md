# 需求池（kander req）

需求池是看板之外的独立轻量存储，位于 `kanban/requirements/`。需求卡不进入
`backlog→todo→working→review→done` 生命周期，不会被 `kander start` 启动，
也与上游任务卡的事务/审核体系完全隔离——同步上游时该目录与代码触点互不冲突。

## 需求卡

`requirements/<YYYYMMDD>-<slug>-req.md`，极简结构：

```markdown
# <标题>

- SOURCE: <来源标识，如 pool://login-doc-42 或文档路径>
- STATUS: draft
- CREATED_AT: 2026-09-08 03:40
- DOCS: <关联文档标识，逗号分隔，可选>
- TASKS: <关联任务卡 ID，逗号分隔>
- TASK_GROUPS: <关联任务组 ID，逗号分隔>

## SUMMARY

<一句话需求摘要>

## NOTES

N/A
```

状态机：

| 状态 | 含义 |
| --- | --- |
| `draft` | 未拆解（初始状态） |
| `decomposed` | 已拆解，TASKS/TASK_GROUPS 已记录关联任务 |
| `completed` | 关联任务全部 done 后标记（手工确认或 `--all-done` 自动校验） |
| `archived` | 终态，不再参与进度 |

进度按关联任务实时计算：`done 数 / 关联总数`（任务组按现有成员展开）。

## 命令

```sh
kander req new --source <来源或路径> [--summary-file <文件>] <slug> <标题...>
kander req list [--status <状态>] [--json]
kander req show [--json] <需求ID>
kander req convert <需求ID> [任务ID...] [--groups <组,组...>]   # 记录关联并转 decomposed
kander req link <需求ID> [任务ID...] [--groups <组,组...>]      # 仅追加关联
kander req unlink <需求ID>                                       # 清空关联
kander req complete <需求ID> [--all-done]                        # 标记完成
kander req remove <需求ID>                                       # 删除（completed 卡受保护）
```

典型流程：

1. 从外部公共需求池拿到需求文档 → `kander req new --source pool://login-doc-42 login-fix "修复登录 XX bug"`。
2. 手工拆解出任务卡（`kander new`，可配合 `--large`/任务组）→ `kander req convert <需求ID> <任务ID...>`。
3. 任务推进；`kander req list` 随时看进度（如 `2/5`）。
4. 关联任务全部 done → `kander req complete <需求ID> --all-done`（校验失败会提示；去掉 `--all-done` 可强制）。
5. 按需回写外部公共需求池（当前为手动/脚本行为，kander 不直接写外部池）。

## 实现边界

- 存储仅依赖 `internal/fs`（防 reparse point、原子写、独占锁 `.kander/locks/requirements.lock`）。
- `link/convert` 校验任务 ID 存在；`unlink` 清空 TASKS/TASK_GROUPS。
- i18n 三语目录各新增 35 个 `board.req*` / `board.messages.req*` key，数量保持一致。
