# AI 交付记录

本目录保存本次评估相关的 **OpenCode 与 Codex** 原生日志、索引、决策日志、指令留档及完整性声明。

- `sessions/ses_*.json`：7 份 OpenCode 原生导出。
- `sessions/codex-*.jsonl`：6 份 Codex 原生 rollout 日志，包括规划、环境检查和 review。
- `session-index.csv`：每个会话一行，包含工具、模型、UTC 时间、用途及相关提交。
- `session-checksums.sha256`：所有会话文件的校验和。
- `decision-log.md`：实际决策、接受/纠正与取舍、验证证据及提交。
- `instructions.md`：既有 AI 指令留档；完整指令上下文亦保留在原生日志中。
- `completeness.md`：工具范围、快照边界、导出限制和独立书面题声明。

## 导出来源

OpenCode：`opencode session export <session-id>`。Codex：本机 `~/.codex/sessions/2026/10/27/rollout-*.jsonl` 中经核对属于本项目的会话，按原始字节复制并用 session ID 命名。不要复制整个 Codex 用户目录，其中含凭据和其它项目数据。

当前会话的归档是快照，截止时刻见索引；本次复制动作之后的最终回复不在快照内。更新导出时应同步维护索引和校验和。

```bash
cd ai
sha256sum -c session-checksums.sha256
```
