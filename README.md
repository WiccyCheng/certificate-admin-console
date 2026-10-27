# Certificate Admin Console

多租户证书管理后台，按 `docs/` 中已冻结的架构、数据模型和 API 契约实现。覆盖证书列表、详情、课程批量发证、吊销、CSV 导出，以及 A1–A7 验收场景。

- 后端：Go 1.22.5 + Gin 1.10.0 + SQLite（`modernc.org/sqlite` 1.34.5，纯 Go，无 CGO）
- 前端：Vue 3 + TypeScript + Vite 8 + Element Plus
- 运行形态：单个 Go 进程同时提供 `/api/v1` 和构建后的前端静态文件
- 数据：SQLite WAL，独立写连接 + 多读连接；默认库 `data/demo.db`

## 快速启动

环境要求：Go 1.22.5、Node.js 20.19+（实测 24.3.0）、npm。

```bash
./scripts/run.sh
# 打开 http://localhost:8080
```

首次运行会安装前端依赖（若 `web/node_modules` 不存在）、构建前端、创建并初始化 `data/demo.db`。重复启动不会重置已有数据。

```bash
# 换端口或换库
ADDR=:8081 ./scripts/run.sh
DB=data/another.db ./scripts/run.sh
```

演示身份（前端右上角切换，后端只信 Bearer token）：

| actor | token | 租户 | 时区 |
| --- | --- | --- | --- |
| alice@east.test | `demo-alice-token` | East | `Australia/Sydney` |
| bob@west.test | `demo-bob-token` | West | `Australia/Perth` |

重置默认库（先停止服务）：

```bash
rm -rf data/demo.db*
```

开发模式：

```bash
go run ./cmd/server --db data/demo.db --addr :8080
# 另一个终端
cd web && npm run dev   # http://localhost:5173，/api 代理到 :8080
```

## 功能与接口

| 方法 | 路径（省略 `/api/v1`） | 说明 |
| --- | --- | --- |
| GET | `/me` | 当前管理员、租户、时区 |
| GET | `/courses` | 本租户课程选项 |
| GET | `/courses/{courseId}/enrollments` | 课程报名、完成状态、证书、发证资格 |
| GET | `/learners/{learnerId}/enrollments` | 学员各课程报名 |
| GET | `/certificates` | 搜索、筛选、排序、分页 |
| GET | `/certificates/{id}` | 详情（UTC、本地时间、本地日期、发证/吊销人） |
| POST | `/courses/{id}/bulk-operations` | 同步批量发证，必须带 `Idempotency-Key` |
| PUT | `/certificates/{id}/revocation` | 吊销；重复请求幂等 |
| GET | `/certificates.csv` | 流式 CSV，复用列表的筛选与排序 |

完整契约见 [docs/API.md](docs/API.md)，数据模型见 [docs/data-model.md](docs/data-model.md)。

## 界面

| 证书列表 | 批量发证与逐学员结果 |
| --- | --- |
| ![证书列表](docs/screenshots/list.png) | ![批量发证](docs/screenshots/bulk-issue.png) |

| 证书详情（A5 本地时间） |
| --- |
| ![证书详情](docs/screenshots/detail.png) |

### 关键语义

- **租户隔离**：tenant 只由 token 解析，客户端提交 `tenantId` 返回 `400`，`X-Tenant-Id` 被忽略。跨租户路径资源统一 `404`，列表/CSV 过滤返回空结果。
- **批量发证幂等**：作用域 `tenant + admin + key`。同键同课程返回已存结果（`200`），同键不同课程返回 `409`；首次提交 `201`。并发同键由写事务串行化，只产生一个逻辑结果。
- **吊销**：`ACTIVE → REVOKED` 首次 `201`，重复 `200` 且不新增审计；吊销是终态，本题不重发。
- **时间**：数据库存 UTC `YYYY-MM-DDTHH:mm:ss.SSSZ`；`issuedLocalDate` / `issuedLocalAt` 由租户 IANA 时区派生，不使用浏览器时区。
- **搜索**：学员姓名或序列号的字面子串，ASCII 不区分大小写，`%` / `_` 为普通字符。
- **CSV**：固定四列、CRLF、UTF-8 无 BOM、公式注入防护；流式写出，中途读取错误会中止连接而不是保存截断文件。

### 序列号规则

| 来源 | 格式 | 示例 |
| --- | --- | --- |
| 在线发证 | `CERT-{TENANT_CODE}-{ULID}`（26 位 Crockford Base32） | `CERT-EAST-01M5Y8QM77F49S7Y0A5T1MC0DX` |
| 默认种子 | `CERT-{TENANT}-{NAME}-0001` | `CERT-EAST-TAYLOR-0001` |
| 大数据生成器 | `{TENANT}-LOAD-{i:06d}` | `EAST-LOAD-000007` |

三类前缀互不冲突；在线序列号时间有序且无需回读。

## 默认演示数据

每个租户 2 门课程、4 位学员、5 条报名、2 张证书、3 条审计；两租户合计 4 门课程、8 位学员、10 条报名、4 张证书、6 条审计。

| 学员 | 课程 | 完成状态 | 初始证书 |
| --- | --- | --- | --- |
| Alex Lee | Safety 101 | COMPLETED | 无 |
| Sam Chen | Safety 101 | INCOMPLETE | 无 |
| Taylor Kim | Safety 101 | COMPLETED | ACTIVE |
| Morgan Wu | Safety 101 | COMPLETED | REVOKED |
| Alex Lee（同一人） | First Aid | COMPLETED | 无 |

Taylor 的 `issued_at` 为 `2026-01-10T13:30:00.000Z`：East 展示 `2026-01-11T00:30:00.000+11:00`，West 展示 `2026-01-10T21:30:00.000+08:00`。

注意：每个租户初始只有 Taylor 一张 ACTIVE 证书。若在同一默认库先执行 A7 吊销，再执行 A2 批量发证，Alex 仍会被发证；若要先演示 A2 再演示 A7，请重置默认库或先给其他学员发证。

## 100,000 行大数据集

生成器使用独立数据库，不污染默认库；目标文件已存在时拒绝执行。

```bash
rm -f data/large-demo.db* data/large-demo.manifest.json
./scripts/seed-large.sh                    # 默认 data/large-demo.db
ADDR=:8081 DB=data/large-demo.db ./scripts/run.sh
```

等价的显式命令：

```bash
go run ./cmd/seed-large --db data/large-demo.db --tenant east --certificates 100000
```

结果：East 共 100,000 张证书（含默认 2 张），West 保持 2 张；新增课程 `Export Load`、99,998 位学员、报名、ACTIVE 证书和 ISSUED 审计。生成后执行 `foreign_key_check`、`integrity_check` 和证书/审计一致性检查，并写入 `data/large-demo.manifest.json`。

## 验证

```bash
./scripts/verify.sh     # gofmt 检查 + go vet + go test -race ./... + 前端构建
```

A1–A7 自动化覆盖：

| 场景 | 测试 | 验证内容 |
| --- | --- | --- |
| A1 租户隔离 | `TestTenantIsolationA1` | 跨租户 404、列表/CSV 空结果、`tenantId` 400、`X-Tenant-Id` 忽略、跨租户 bulk 无副作用 |
| A2 重试安全 | `TestBulkIdempotencyA2A3A4`、`TestBulkConcurrentSameKeyA2`、`TestIdempotencyKeyLifecycleAndScope` | 同键同课程 `200` 原样回放；8 并发只发一张证书；失败请求不消耗键 |
| A3 键误用 | `TestBulkIdempotencyA2A3A4` | 同键不同课程 `409`，不覆盖、不二次执行 |
| A4 学员结果 | `TestEnrollmentEligibilityA4`、`TestBulkIdempotencyA2A3A4` | `issued` / `skipped` 及三种原因、按 `learnerId ASC`、计数不变量 |
| A5 DST 与本地日期 | `TestTimeAndLocalDateA5` | East/West 本地时间、本地日期、默认排序与次排序 |
| A6 导出 | `TestCSVExportA6`、`TestCSVAbortOnMidStreamError` | 四列/CRLF/空结果/筛选/重复导出一致/中途错误中止连接 |
| A7 吊销审计 | `TestRevocationA7` | 首次 `201`、重复与并发 `200`、仅一条 REVOKED 审计、跨租户 404 |

### 实测结果（2026-10-27，WSL2 / Go 1.22.5 / Node 24.3.0）

- `go test -race ./...`：通过，约 3.1s。
- 100,000 行生成：**9.86s**，峰值内存约 117MB；`foreign_key_check`、`integrity_check`、审计一致性均通过。
- 100,000 行 CSV：**0.397s**，5,800,049 字节，100,001 行；两次导出 SHA-256 相同。
- 筛选 `Load Learner 007`：1,000 行，**0.051s**，58,054 字节。
- 导出期间列表请求：`200`，**0.034s**。
- 浏览器端到端（Playwright Chromium）：actor 切换、详情 A5、批量发证 1 issued / 3 skipped、CSV 下载均通过。

详细命令、输出与边界说明见 [docs/verification.md](docs/verification.md)。

## 项目结构

```text
cmd/server/           Go HTTP 服务入口（API + 静态前端）
cmd/seed-large/       100,000 行大数据生成器
internal/store/       连接池、迁移、默认种子
internal/service/     鉴权、查询、事务发证、吊销、CSV 流
internal/httpapi/     Gin 路由、校验、错误、CSV、集成测试
migrations/           嵌入式 SQL 迁移
web/                  Vue 3 + TypeScript + Element Plus 前端
scripts/              一键运行 / 验证 / 大数据脚本
docs/                 架构、数据模型、API、验收记录
ai/                   AI 会话导出、索引、决策日志、完整性声明
```

## 已知限制与取舍

- 批量发证是同步请求，不引入异步任务、队列或轮询；题目未要求。
- 吊销是终态，不支持恢复或吊销后重发；唯一约束按此设计。
- 演示 token 是固定值，没有登录、注册、token 签发或真实鉴权。
- 不提供可选扩展 `GET /learners` 和 `GET /bulk-operations/{operationId}`。
- `failed / ISSUANCE_ERROR` 分支已实现（savepoint 回滚单个学员），但默认数据没有自然触发场景；自动化测试覆盖了 `issued` 和两类 `skipped`。
- SQLite busy 的 `503` 映射已实现（10s busy timeout），测试未强制制造锁等待耗尽。
- CSV 不带 BOM，便于程序比对；Excel 直接打开中文 CSV 的兼容性不在本题范围。
- 前端完整引入 Element Plus，JS 约 1.0MB（gzip 321.8KB）；未做代码分割。
- 中文字体使用 Noto Sans CJK SC 子集（`web/src/assets/`，SIL OFL，许可证随附），避免目标机器缺少 CJK 字体时出现方框。
- 题目 PDF 未提交到仓库；实现依据为 `docs/` 中已冻结的契约。
- 单机 SQLite，不承诺多主机共享本地数据库。
- AI 交付记录见 [ai/completeness.md](ai/completeness.md)；实现会话之前的历史规划会话不在当前环境可导出范围内，已如实披露。
