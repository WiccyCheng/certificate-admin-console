# 决策日志

记录实际影响实现与交付的决策。每条包含背景、决定、被拒绝的方案和证据；不把草案或未发生的讨论写成已接受决策。

时间均为 UTC。相关会话见 `ai/sessions/`，验证细节见 `docs/verification.md`。

## D01 冻结 API v1，按契约实现

- 时间：2026-10-27
- 背景：实现开始时 `docs/API.md` 标注“待最终确认”，用户指示按既有约定开始，序列号等细节可自行决定。
- 决定：把 API v1 视为实现与测试的唯一依据；不增加 `GET /learners`、`GET /bulk-operations/{operationId}`、异步任务或登录。
- 拒绝：边实现边改契约；增加“刷新后恢复操作”的可选接口。理由是首版验收不需要，且会扩大范围。
- 证据：`docs/API.md` 状态行；`TestValidationRoutingAndHeaders`、`TestBulkIdempotencyA2A3A4`。
- 提交：`2df1eee`、`bbe4ba1`。

## D02 序列号格式

- 背景：数据模型只给出建议，未固定。
- 决定：
  - 在线发证：`CERT-{TENANT_CODE}-{ULID}`（26 位 Crockford Base32，时间有序）。
  - 默认种子：`CERT-{TENANT}-{NAME}-0001`。
  - 大数据：`{TENANT}-LOAD-{i:06d}`。
- 拒绝：UUID v4（不可按时间排序）；数据库自增序列回读（多一次写/读且与其他字段组合复杂）；三类数据共用一个前缀。
- 证据：`internal/service/service.go`、`internal/store/seed.go`、`cmd/seed-large/main.go`；测试断言在线序列号为 `CERT-EAST-` + 26 位。
- 提交：`2df1eee`。

## D03 SQLite 驱动、连接池与并发

- 决定：`modernc.org/sqlite`（纯 Go）；WAL；写池 `MaxOpenConns(1)` + `_txlock=immediate`；读池 8 连接且 `query_only(1)`；`busy_timeout(10000)`、`foreign_keys(1)`。
- 拒绝：CGO 驱动（部署依赖 gcc）；所有操作共用一个连接池（CSV 长读会挤占列表读）；内存数据库（重启丢失事实）。
- 证据：`internal/store/db.go`；`TestBulkConcurrentSameKeyA2`；大库导出期间列表 0.034s。
- 提交：`2df1eee`。

## D04 批量发证事务与幂等

- 决定：`BEGIN IMMEDIATE` 内先校验课程，再按 `tenant + admin + key` 查幂等记录；新操作逐学员写证书/审计，逐学员 savepoint；`bulk_operations` 最后写入，证书通过延迟外键引用；`response_json` 原样存储并原样回放。
- 拒绝：
  - 持久化 `PROCESSING/QUEUED` 状态（题目无异步要求，崩溃回滚即可）。
  - 先写占位操作行（需要额外清理逻辑；延迟外键足够）。
  - 在内存保存幂等结果（重启丢失）。
  - 重放时按当前状态重新计算（会改变历史结果）。
- 证据：`internal/service/bulk.go`；`TestBulkIdempotencyA2A3A4`、`TestBulkConcurrentSameKeyA2`、`TestIdempotencyKeyLifecycleAndScope`。
- 提交：`2df1eee`。

## D05 吊销为终态

- 决定：事务内条件更新 `ACTIVE → REVOKED`，同事务写一条 REVOKED 审计；重复/并发吊销返回第一次的 `revokedAt`/`revokedBy`，不新增审计；不提供恢复或重发。
- 拒绝：删除证书；软删除状态列；吊销原因字段；吊销后允许重新发证（会改变唯一约束、审计和列表语义）。
- 证据：`internal/service/revoke.go`；`TestRevocationA7`。
- 提交：`2df1eee`。

## D06 时间与本地日期

- 决定：所有 `*At` 存 UTC `YYYY-MM-DDTHH:mm:ss.SSSZ`；`issuedLocalDate` 和 `issuedLocalAt` 用租户 IANA 时区在服务端派生；种子、在线发证、大数据生成器共用规则。
- 拒绝：按 UTC 日期排序；浏览器本地时区换算；写死 `+11:00`/`+08:00`；存 Unix 时间戳。
- 证据：`internal/service/models.go`、`queries.go`；`TestTimeAndLocalDateA5`；A5 样例与文档一致。
- 提交：`2df1eee`。

## D07 搜索、排序与 CSV

- 决定：
  - `q` 用 `LIKE ... ESCAPE '\'`，转义 `%`/`_`/`\`，ASCII 大小写不敏感；
  - 排序白名单 + 始终追加 `certificate.id ASC`；
  - CSV 固定四列、CRLF、UTF-8 无 BOM、公式注入前缀、流式写出；
  - 响应头后错误 `panic(http.ErrAbortHandler)`，自定义 Recovery 放行该值；
  - 导出并发上限 2，超限 `503`。
- 拒绝：额外导出 `certificateId/status/issuedAt`（API 评审已收窄为四列）；前端排序后再分页；先把全部行聚合到内存；响应头后追加 JSON 错误；使用 `encoding/csv` 默认 LF 和仅按逗号/引号加引号（不满足 CRLF 与首尾空白规则）。
- 证据：`internal/httpapi/csv.go`；`TestCSVExportA6`、`TestCSVFormulaAndQuoting`、`TestCSVAbortOnMidStreamError`；100,000 行 0.397s、重复导出 SHA-256 相同。
- 提交：`2df1eee`。

## D08 前端交互与幂等键生命周期

- 决定：单页 + Element Plus；actor 切换时中止请求并用 epoch 丢弃迟到响应；列表搜索/筛选/排序全部走服务端；批量发证每次用户提交生成一个 `crypto.randomUUID()` 键，失败后用同一个键重试；CSV 用 `fetch` + `Blob` 下载以携带 Bearer token。
- 拒绝：多页面路由；前端本地排序/分页；裸 `<a href>` 下载（不会带 Authorization）；失败后自动换新键（可能重复逻辑操作）。
- 已知取舍：关闭发证弹窗会清空内存中的键；若此时原请求实际已提交，重新打开会生成新键。服务端唯一约束仍防止重复证书，但会多一条 bulk 记录。
- 证据：`web/src/App.vue`、`api.ts`；Playwright 冒烟通过。
- 提交：`b92344a`。

## D09 中文界面与字体子集

- 背景：Chromium 冒烟截图显示系统没有任何 CJK 字体，所有中文渲染为方框；Element Plus 的 input/button 还不继承页面字体。
- 决定：打包 Noto Sans CJK SC 的 830 字符子集（约 176KB，SIL OFL，许可证随附）；设置 `--el-font-family` 并对表单控件使用 `font-family: inherit`。
- 拒绝：改成全英文界面（与已有中文文档和交互不一致）；运行时加载 Google Fonts CDN（离线一键启动不可依赖）；依赖目标机器自带中文字体（本环境已证明不可靠）。
- 证据：`web/src/assets/`、`web/src/styles.css`；`docs/screenshots/`；无字体时与修复后的截图对比。
- 提交：`b92344a`。

## D10 大数据集独立生成

- 决定：`cmd/seed-large` 只写显式指定的新文件，拒绝与默认库相同或已存在；先建 schema/默认种子，再按 2,000 行批次补齐 East 至目标总数；确定性 ID/姓名/序列号/时间；输出 manifest 并执行 `foreign_key_check`、`integrity_check`、审计一致性检查。
- 拒绝：把 100,000 行写入 `data/demo.db`；随机数据；单事务插入全部行；覆盖已有文件；关闭外键或约束换速度。
- 证据：`cmd/seed-large/main.go`；`data/large-demo.manifest.json`（本地生成，不入库）；生成 9.83s，检查全部 ok。
- 提交：`2df1eee`。

## D11 验证策略

- 决定：HTTP 级集成测试跑真实 SQLite（临时库、每连接外键/WAL），并开启 `-race`；A1–A7 逐项命名测试；另做 Playwright 主路径冒烟和 100,000 行实测。
- 拒绝：只用 mock/stub 验证事务与租户隔离；只做单元测试不启动 HTTP；用默认库跑测试（会污染演示数据）。
- 证据：`internal/httpapi/*_test.go`；`scripts/verify.sh`；`docs/verification.md`。
- 提交：`2df1eee`、`bbe4ba1`。

## D12 校验顺序修正（测试驱动的纠正）

- 背景：初始实现先校验 `Idempotency-Key` 再校验 body；测试中“非空 body + `text/plain` + 缺 key”得到 `400`，而契约 1.5 要求 body/Content-Type 先于 key。
- 决定：调整为 body/Content-Type → Idempotency-Key；补充非法 body 不消耗键的测试。
- 拒绝：为了让测试通过而放宽契约；把 `415` 改成 `400`。
- 证据：`internal/httpapi/handlers.go`、`validation.go`；`TestBulkIdempotencyA2A3A4`、`TestIdempotencyKeyLifecycleAndScope`。
- 提交：`2df1eee`（修正已在同一实现提交内）。
