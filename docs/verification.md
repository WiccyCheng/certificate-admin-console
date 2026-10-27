# 验证记录

日期：2026-10-27。所有结果来自本仓库的实际运行，不是估算。

## 1. 环境

| 项目 | 值 |
| --- | --- |
| 平台 | WSL2 Linux |
| Go | 1.22.5 |
| Node / npm | 24.3.0 / 11.4.2 |
| SQLite 驱动 | `modernc.org/sqlite` 1.34.5（纯 Go） |
| 浏览器冒烟 | Playwright 1.64.1，Chromium 156 headless |
| 默认库 | `data/demo.db`（首次启动自动建库/种子） |

## 2. 自动化验证

命令：

```bash
gofmt -l cmd internal migrations
go vet ./...
GIN_MODE=release go test -race ./...
(cd web && npm run build)
```

结果：`gofmt` 无输出，`go vet` 无告警，`go test -race ./...` 通过（约 3.1s），前端 TypeScript 检查与构建通过。

```text
--- PASS: TestTenantIsolationA1 (0.22s)
--- PASS: TestBulkIdempotencyA2A3A4 (0.13s)
--- PASS: TestBulkConcurrentSameKeyA2 (0.13s)
--- PASS: TestTimeAndLocalDateA5 (0.13s)
--- PASS: TestRevocationA7 (0.23s)
--- PASS: TestValidationRoutingAndHeaders (0.14s)
--- PASS: TestCSVExportA6 (0.13s)
--- PASS: TestCSVFormulaAndQuoting (0.00s)
--- PASS: TestCSVAbortOnMidStreamError (0.71s)
--- PASS: TestEnrollmentEligibilityA4 (0.12s)
--- PASS: TestIdempotencyKeyLifecycleAndScope (0.12s)
--- PASS: TestMultipleAuthorizationHeaders (0.09s)
ok  certadmin/internal/httpapi  3.186s
```

### A1 租户隔离

`TestTenantIsolationA1` 覆盖：

- 缺失 token → `401` + `WWW-Authenticate`；
- Alice 仅能看到 East 的证书 3、4；Bob 仅能看到 West 的证书 1、2；
- 跨租户详情、课程报名、学员报名、bulk 发证 → `404`；
- 跨租户 bulk 后 `bulk_operations` 仍为 0，East 证书数不变；
- `?courseId=3` 列表 `total=0`，CSV 仅表头；
- `?tenantId=1` → `400`，`X-Tenant-Id: 2` 不改变租户范围。

### A2 重试安全

`TestBulkIdempotencyA2A3A4`、`TestBulkConcurrentSameKeyA2`、`TestIdempotencyKeyLifecycleAndScope` 覆盖：

- 首次 `201`；同键同课程重放 `200`，响应字节完全一致；
- 重放不因后续吊销改变历史结果；
- 8 个并发同键请求：1 个 `201`、7 个 `200`，响应一致，最终只新增 1 张证书、1 条 ISSUED 审计、1 条 bulk 记录；
- 失败的请求（非法 body→`400`）不消耗幂等键，随后同键合法请求仍为 `201`；
- 幂等键作用域为 tenant + admin：Bob 可独立复用同一字符串键。

### A3 键误用

同键不同课程 → `409 IDEMPOTENCY_CONFLICT`，不创建第二张证书，不覆盖原结果；非法 body 即使键已存在仍先返回 `400`。

### A4 学员结果

课程 1（Safety 101）批量结果按 `learnerId ASC`：

| learner | status | reasonCode |
| --- | --- | --- |
| Alex Lee | issued | null |
| Sam Chen | skipped | COURSE_INCOMPLETE |
| Taylor Kim | skipped | ALREADY_CERTIFIED |
| Morgan Wu | skipped | CERTIFICATE_REVOKED |

计数满足 `total = results.length = issued + skipped + failed`。报名列表的 `issuanceEligibility` 同样覆盖四种状态。

### A5 DST 与本地日期

- East / `2026-01-10T13:30:00.000Z` → `issuedLocalAt=2026-01-11T00:30:00.000+11:00`，本地日期 `2026-01-11`；
- West / 同一 UTC 时刻 → `2026-01-10T21:30:00.000+08:00`，本地日期 `2026-01-10`；
- 列表按本地日期升降序、按姓名/序列号排序、仅传 `sortOrder` 时的默认字段均符合契约；
- 始终追加 `certificate.id ASC` 次排序。

### A6 导出增长

`TestCSVExportA6` 覆盖固定表头、CRLF、UTF-8 无 BOM、空结果仅表头、字面 `%`/`_`、ASCII 大小写、状态/课程筛选、重复导出字节一致。

`TestCSVAbortOnMidStreamError` 注入第 250 行读取错误：响应头已发出，连接被中止，客户端读取返回错误，不会得到“成功”的截断文件。

100,000 行实测（命令与输出）：

```text
$ curl .../api/v1/certificates.csv?sortBy=issuedLocalDate&sortOrder=desc
status=200 time=0.397014s size=5800049 bytes
lines=100001
sha256 f219235182732069cf210967843772290b1ceaaaa09e51e1b1d815c46409f299

$ curl .../api/v1/certificates.csv?q=Load%20Learner%20007&sortBy=serialNumber&sortOrder=asc
status=200 time=0.050650s size=58054 bytes
lines=1001

# 导出期间请求列表
list_status=200 list_time=0.033825s
```

两次完整导出 SHA-256 相同；首行为表头，末尾无多余空行；100,001 行 = 表头 + 100,000 张 East 证书。

### A7 吊销审计

`TestRevocationA7` 覆盖：

- 首次 ACTIVE → REVOKED：`201`，写一条 REVOKED 审计；
- 重复吊销：`200`，`revokedAt` / `revokedBy` 保持第一次值，不新增审计；
- 并发 6 次吊销：1 个 `201`、5 个 `200`，仅一条 REVOKED 审计；
- 跨租户吊销 → `404`，无副作用；
- 详情中的发证字段、本地时间不变。

## 3. 100,000 行数据生成

```bash
go run ./cmd/seed-large --db data/large-demo.db --tenant east --certificates 100000
```

实测 **9.83s**，峰值内存约 114MB。manifest：

```json
{
  "tableCounts": {
    "tenants": 2,
    "admins": 2,
    "learners": 100006,
    "courses": 5,
    "enrollments": 100008,
    "certificates": 100002,
    "bulk_operations": 0,
    "audit_events": 100004
  },
  "tenantCertificates": { "EAST": 100000, "WEST": 2 },
  "searchExpectations": {
    "learnerName=Load Learner 007": 1000,
    "serialNumber=EAST-LOAD-000007": 1
  },
  "verified": {
    "exportLoadConsistency": "certs=99998 enrollments=99998 audits=99998 active=99998",
    "foreignKeyCheck": "ok",
    "integrityCheck": "ok",
    "issuedAuditConsistency": "ok",
    "targetCertificateCount": "ok"
  }
}
```

`dateDistribution` 含 30 个本地日期，合计 100,000。

## 4. 浏览器端到端

Playwright 1.64.1（Chromium 156 headless）对 `http://127.0.0.1:18081` 执行：

1. 加载列表，确认 East 种子证书；
2. 切换到 Bob，确认只显示 West 证书；切回 Alice，确认旧数据被清除；
3. 打开 Taylor 详情，确认 `2026-01-11T00:30:00.000+11:00`；
4. 打开批量发证，选择 Safety 101（#1），提交后显示 `发证 1 / 跳过 3 / 失败 0` 与逐学员原因；
5. 点击导出 CSV，下载文件名为 `certificates.csv`，4 行（表头 + 3 张证书），全部为 East；
6. 收集浏览器 console/page error，结果为空。

界面截图见 `docs/screenshots/`。运行环境没有系统中文字体，因此前端打包了 Noto Sans CJK SC 子集（SIL OFL，许可证随附）；这保证无 CJK 字体的机器也能正确显示。

### 4.1 前端状态回归（2026-10-27 修复后）

针对 review 指出的三个问题补充了 Playwright 回归：

1. **旧请求串租户**：拦截 Alice 的列表响应并延迟 1.2s，在响应到达前切换到 Bob。结果 Bob 列表只含 West 数据，没有 East 行；详情请求延迟后关闭抽屉再切换租户，抽屉保持关闭且不写回旧详情。
2. **清空筛选发送空参数**：选择课程/状态后再清空，检查最近一次列表请求的 URL：`courseId` 和 `status` 均不再出现，列表恢复完整数据。
3. **切换租户保留旧课程 ID**：在 Alice 选择 `Safety 101`（courseId=1）后切到 Bob，列表请求不再携带 `courseId`，页面显示 West 的 2 张证书；批量发证弹窗中的课程选择也被清空。

修复方式：所有异步加载统一捕获 `{epoch, token}`，响应落地前同时校验两者；切换租户会中止 me/courses/list/detail/enrollment/submit/revoke/export 的 AbortController，并重置全部筛选条件。列表与 CSV 共用同一个查询构造函数，只发送有效值。浏览器回归与主路径冒烟均通过。

## 5. 未强制覆盖的边界

- `failed / ISSUANCE_ERROR`：代码使用 savepoint 回滚单学员后继续，但默认数据和常规流程没有可恢复的数据库错误，未构造触发器强制该分支。
- `503 TEMPORARILY_UNAVAILABLE`：已实现 SQLite busy 检测与 `Retry-After`，测试未把 10s busy timeout 耗尽。
- CSV 中途错误使用注入读取错误验证，不是物理磁盘故障。
- 浏览器冒烟覆盖主路径，不包含所有分页/空态/网络中断组合。
- AI 记录：相关规划、环境失败运行、实现和子代理会话均已导出；快照边界详见 `ai/completeness.md`。
