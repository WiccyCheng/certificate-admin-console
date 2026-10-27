# 完整性声明

## 工具与模型

| 项目 | 值 |
| --- | --- |
| 编码工具 | OpenCode 2 |
| 模型 | DeepSeek V4.1 Flash 1017；`deepseek/deepseek-flash`，variant `max` |
| 子代理 | OpenCode `explore` 子代理，同一模型 |
| 其它 AI 编码工具 | 无 |

实现、测试、文档和验证均通过 OpenCode 的 shell / 文件工具完成；没有使用独立的 IDE 自动补全、Copilot 或其它不可导出的补全工具。中文字体子集使用本地 `fonttools` 从 Noto Sans CJK 生成，属于构建步骤，不是 AI 生成内容。

## 已导出的原生会话

导出格式：OpenCode 原生 JSON（`info` + `messages`），未使用 `--sanitize`。时间均为 UTC，`ended_at` 是导出快照时刻。

| 会话 | 时间 | 用途 | 文件 |
| --- | --- | --- | --- |
| `ses_e83cd1e1cffef3FhZAslzNsT6t` | 07:11–07:53 | 数据模型与任务状态机审查（规划） | `sessions/ses_e83cd1e1cffef3FhZAslzNsT6t.json` |
| `ses_e83c45577ffe7XVpJjicbHpDmr` | 07:20–08:37 | chore worker：仓库准备与文档提交 | `sessions/ses_e83c45577ffe7XVpJjicbHpDmr.json` |
| `ses_e83b56706ffeQp37rqQjjsiYhs` | 07:37–07:52 | Codex TUI 启动失败排查（环境失败运行，未参与实现） | `sessions/ses_e83b56706ffeQp37rqQjjsiYhs.json` |
| `ses_e83a03f94ffe58RDubNXem0S7l` | 08:00–08:35 | REST API 设计与 A1–A7 对照（规划） | `sessions/ses_e83a03f94ffe58RDubNXem0S7l.json` |
| `ses_e837cb6b6ffeMafvJ2NSUF13hQ` | 08:39–09:06（快照） | 后端、前端、验证与文档实现（本会话） | `sessions/ses_e837cb6b6ffeMafvJ2NSUF13hQ.json` |
| `ses_e837cac7affeIzJADBnMykgbA7` | 08:39:03–08:39:16 | 子代理：启动连通性检查与仓库扫描 | `sessions/ses_e837cac7affeIzJADBnMykgbA7.json` |
| `ses_e834e9cb6ffeWcZKgwptGPFdh5` | 09:29–09:43（快照） | 前端旧请求、空筛选参数与租户切换修复及回归 | `sessions/ses_e834e9cb6ffeWcZKgwptGPFdh5.json` |

索引见 `session-index.csv`；校验和见 `session-checksums.sha256`。

## 导出限制与缺口

1. **快照边界**：原生导出是命令执行时的快照。导出命令自身、导出后的收尾操作以及本文件最后的编辑不会出现在对应会话 JSON 中；这是导出机制的限制，不是删减。
2. **会话仍在继续**：导出是快照；`ended_at` 不代表对话在业务上结束。当前修复会话的导出截止时间见索引。
3. **规划会话**：本次评估相关的 4 个规划/准备会话、实现会话和修复会话均已导出；OpenCode 中其它项目或其它日期的会话与本题无关，未导出。
4. **失败运行**：`Codex TUI 启动失败` 会话作为环境失败运行保留，未用于实现决策。
5. **其它工具**：未使用其它 AI 编码工具。若评审认为终端、编辑器或操作系统层面存在不可导出的自动补全，本仓库无法提供其原生记录；本实现没有依赖此类补全。
6. **本地路径**：导出文件包含 `/home/charles/code/OceanAI_CertifyCate_Homwork` 等本地路径。未发现真实密钥、密码或私有 token；固定演示 token（`demo-alice-token`、`demo-bob-token`）按设计公开。
7. **独立书面题**：本仓库不包含第 6 节独立书面题的 AI 代写内容；该部分应由候选人本人完成并单独声明。

## 完整性核对方法

```bash
cd ai
sha256sum -c session-checksums.sha256
python3 - <<'PY'
import json, glob
for p in sorted(glob.glob('sessions/*.json')):
    data = json.load(open(p))
    print(p, len(data['messages']), data['info']['model'])
PY
```

若重新导出会话，应同时更新 `session-index.csv`、本文件的快照说明和校验和。
