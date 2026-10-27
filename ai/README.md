# AI 交付记录

本目录保存本次评估相关的 OpenCode 原生会话导出、索引、决策日志和完整性声明。

```text
ai/
├── README.md              本文件
├── session-index.csv      每次相关会话一行（UTC 时间）
├── decision-log.md        实际重要决策、证据与关联提交
├── completeness.md        工具清单、导出范围、缺口与校验和
└── sessions/              原生 JSON 会话导出
```

## 重新导出

会话 ID 来自 `opencode api get /api/session`。导出命令：

```bash
opencode session export <session-id> > ai/sessions/<session-id>.json
```

导出为 OpenCode 原生 JSON（含 `info` 与 `messages`），未使用 `--sanitize`；导出的仓库路径和固定演示 token 均为非敏感内容。导出前已扫描常见密钥模式（`sk-`、`AKIA`、私钥头、GitHub/Slack token），未发现真实凭据。

## 说明

- 会话时间统一记录为 UTC。
- `session-index.csv` 的 `ended_at` 是导出时刻，不是人工“结束”时刻。
- 规划会话、实现会话、子代理会话和一次环境失败运行均保留；未使用其它 AI 编码工具。
- 详细缺口与不可导出项见 [completeness.md](completeness.md)。
