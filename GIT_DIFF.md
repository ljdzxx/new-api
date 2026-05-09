# Git Diff Summary

> Generated: 2026-05-07

## Branch & Remote Info

| Item | Detail |
|------|--------|
| Current branch | `main` |
| Local HEAD | `1ad983d2` — feat: support claude-opus-4-7 (#4293) |
| origin/main | `ljdzxx/new-api` |
| upstream/main | `QuantumNous/new-api` |

## Commit Divergence

| Relation | Count |
|----------|-------|
| upstream/main 领先本地 main | **308 commits** |
| 本地 main 领先 upstream/main | **57 commits** |
| 本地 main 领先 origin/main | **23 commits**（未 push） |

## Uncommitted Local Changes

### Modified Files (11)

- `controller/channel.go`
- `controller/relay.go`
- `docs/review-notes.md`
- `dto/channel_settings.go`
- `middleware/auth.go`
- `model/channel.go`
- `relay/channel/xiaomi/adaptor.go`
- `relay/claude_handler.go`
- `service/openai_chat_responses_compat.go`
- `web/src/components/table/channels/modals/EditChannelModal.jsx`
- `web/src/components/table/usage-logs/UsageLogsColumnDefs.jsx`

### Untracked Files (8)

- `API.md`
- `CHANNEL_SCHEME.md`
- `CODEX-INNER-TOOLS-CN.md`
- `CODEX-INNER-TOOLS.md`
- `relay/channel/xiaomi/responses_compat.go`
- `service/channel_forward.go`
- `service/channel_forward_test.go`
- `service/openaicompat/responses_chat_compat.go`
- `service/openaicompat/responses_chat_compat_test.go`

## Conclusion

upstream 已大幅更新 308 个 commit，本地 main 严重落后于上游。建议尽快合并上游代码（rebase 或 merge），避免后续冲突扩大。
