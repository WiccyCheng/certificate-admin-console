# Certificate Admin Console REST API 契约

状态：v1 契约已冻结，并作为实现与自动化测试的唯一依据。已按《Ocean AI Labs Beijing Technical Interview Assessment - 20261.pdf》（20261026）的必做范围和 A1–A7 逐项核对；实现与验收结果见 [README](../README.md) 和 [verification.md](verification.md)。架构草案第 6 节只保留边界说明。

约定：统一前缀 `/api/v1`，JSON 字段使用 camelCase。题目中的“用户”在数据模型中对应学员 Learner；管理员 Admin 是操作人。课程完成状态属于 Enrollment，证书状态属于 Certificate，两者分别返回，不能用一个 `status` 代替。

## 0. 接口索引

| 方法 | 路径（省略 `/api/v1`） | 用途 |
| --- | --- | --- |
| GET | `/me` | 当前管理员、租户与时区 |
| GET | `/courses` | 本租户课程选项 |
| GET | `/courses/{courseId}/enrollments` | 某课程的学员、完成状态、证书和发证资格 |
| GET | `/learners/{learnerId}/enrollments` | 某学员各课程的完成状态、证书和发证资格 |
| GET | `/certificates` | 证书列表：搜索、筛选、排序、分页 |
| GET | `/certificates/{certificateId}` | 证书详情 |
| POST | `/courses/{courseId}/bulk-operations` | 对该课程全部报名执行批量发证，必须带幂等键 |
| PUT | `/certificates/{certificateId}/revocation` | 吊销证书；重复请求幂等 |
| GET | `/certificates.csv` | 按列表相同的搜索与排序导出全部匹配证书 |

首版不包含登录、课程/学员编辑、单张发证、恢复/重发、后台任务或公开验真。`GET /learners` 和 `GET /bulk-operations/{operationId}` 是可选扩展，不属于首版验收前置条件。

## 1. 通用约定

### 1.1 地址、编码与 JSON

- 所有接口都在 `/api/v1` 下；浏览器与 API 同源，不启用 CORS。开发时由 Vite 代理到 Go 服务。
- 请求与响应使用 UTF-8。普通 JSON 响应的 `Content-Type` 为 `application/json; charset=utf-8`。
- 带非空 body 的请求必须使用 `Content-Type: application/json`（允许带 charset 参数）；否则返回 `415`。
- JSON 对象字段顺序不影响语义；客户端不得依赖字段顺序。
- 枚举值区分大小写。实体状态使用大写：`ACTIVE`、`REVOKED`、`COMPLETED`、`INCOMPLETE`。批量发证的逐学员结果按题目要求使用小写：`issued`、`skipped`、`failed`。
- 未知 query 参数、未知 body 成员、非法枚举、重复 query 参数都返回 `400`。未知请求头忽略，但 `X-Tenant-Id` 不会被采信。
- 响应中的未知字段由客户端忽略，便于以后追加字段；已文档化的字段始终存在，缺失值用 `null`，数组用 `[]` 而不是 `null`。
- 单资源和操作结果直接返回 JSON 对象，不再套一层 `data`。

### 1.2 ID、时间与序列号

| 类型 | 格式 | 示例 |
| --- | --- | --- |
| 实体 ID | 正整数，JSON number；路径/query 只接受 `[1-9][0-9]*` | `1`、`42` |
| operationId | 小写 UUID v4 字符串 | `bd2e0cf6-37b0-48a3-bc31-b442a84557bb` |
| UTC 时间 | `YYYY-MM-DDTHH:mm:ss.SSSZ` | `2026-01-10T13:30:00.000Z` |
| 租户本地时间 | `YYYY-MM-DDTHH:mm:ss.SSS±HH:MM` | `2026-01-11T00:30:00.000+11:00` |
| 租户本地日期 | `YYYY-MM-DD` | `2026-01-11` |
| 时区 | IANA 名称 | `Australia/Sydney` |
| serialNumber | 不透明字符串；客户端不得解析其格式 | `CERT-EAST-7F3A...` |

- 所有 `*At` 时间字段都是 UTC 固定格式 `YYYY-MM-DDTHH:mm:ss.SSSZ`，唯一例外是 `issuedLocalAt`，它使用租户时区并带 UTC 偏移。
- `issuedLocalDate` 和 `issuedLocalAt` 由 `issuedAt` 按证书所属租户的 IANA 时区派生，不使用浏览器时区。
- 写接口的时间由服务端确定；客户端不能提交发证时间或吊销时间。
- 不接受 Unix 秒、Unix 毫秒或无时区时间。

### 1.3 分页

所有列表接口统一返回：

```json
{
  "items": [],
  "page": 1,
  "pageSize": 20,
  "total": 0
}
```

| 参数 | 默认 | 规则 |
| --- | --- | --- |
| `page` | `1` | 从 1 开始的正整数；非法值返回 `400` |
| `pageSize` | `20` | 1–100 的整数；非法值返回 `400`，不做静默截断 |
| `total` | — | 当前筛选条件下的总行数，不受分页影响 |
| `items` | — | 当前页数据；越界页返回 `200` 和空数组 |

- 所有排序都必须是确定性的，并追加证书/报名主键作为次排序条件。
- 一次列表响应的 `total` 与 `items` 必须在同一读事务/同一快照内计算；先 `COUNT` 再查询当前页时，不能让中间提交的写入造成 `total` 与 `items` 不一致。跨页不要求同一快照。SQLite WAL 下读事务保持固定快照，同时允许其他连接提交写入。
- CSV 导出不分页；CSV 不接受 `page`、`pageSize`。

### 1.4 认证、actor 与租户

- 所有接口（含 `/me` 和 CSV）都要求：

```http
Authorization: Bearer <demo-token>
```

- 演示环境固定两个 token，不提供登录、注册、刷新或签发接口：

| token | 管理员 | 租户 | 时区 |
| --- | --- | --- | --- |
| `demo-alice-token` | `alice@east.test` | East | `Australia/Sydney` |
| `demo-bob-token` | `bob@west.test` | West | `Australia/Perth` |

- token 区分大小写。缺失、格式错误、未知 token 或多个 `Authorization` 值统一返回 `401`，并带 `WWW-Authenticate: Bearer realm="certificate-admin"`。
- 服务端由 actor 解析可信 `tenantId`，不接受客户端通过 query、body 或 `X-Tenant-Id` 选择租户。把 `tenantId` 作为 query/body 字段提交属于未知参数，返回 `400`。
- 路径资源不属于当前租户时，与资源不存在统一返回 `404`，不透露其他租户资源是否存在。
- 列表/CSV 的 `courseId`、`learnerId` 只是过滤条件；传入其他租户的 ID 返回空结果，不返回 `404`，避免泄露存在性。
- 不提供公开的管理员列表；actor 选项由前端固定配置。

### 1.5 校验顺序

服务端按以下顺序处理请求，前一步失败即返回，不产生业务副作用：

1. 解析身份：无效/缺失 token → `401`。
2. 校验语法：路径 ID、query、body、Content-Type、Idempotency-Key、枚举和范围 → `400` 或 `415`。
3. 校验资源归属：当前租户内找不到路径资源 → `404`。
4. 执行业务：幂等冲突 → `409`；临时锁故障 → `503`；其他未分类故障 → `500`。

因此，跨租户的合法请求统一得到 `404`；非法参数即使指向其他租户也得到 `400`，不会泄露资源是否存在。CSV 的所有校验都必须在发送响应头之前完成。

### 1.6 请求头与响应头

| 方向 | 头 | 约定 |
| --- | --- | --- |
| 请求 | `Authorization` | 见 1.4，所有接口必填 |
| 请求 | `Content-Type` | 非空 body 必须是 `application/json` |
| 请求 | `Idempotency-Key` | 仅批量发证必填；必须且只能出现一次 |
| 响应 | `Content-Type` | JSON 或 `text/csv; charset=utf-8` |
| 响应 | `X-Request-Id` | 所有响应都带；格式 `req-` + 16 位小写十六进制 |
| 响应 | `Cache-Control` | 所有认证响应为 `no-store` |
| 响应 | `WWW-Authenticate` | 仅 `401` 带 |
| 响应 | `Retry-After` | 仅 `503` 带，正整数秒 |
| 响应 | `Allow` | 仅 `405` 带，列出该路径允许的方法 |
| 响应 | `Content-Disposition` | 仅 CSV 带，固定 `attachment; filename="certificates.csv"` |

- `X-Request-Id` 由服务端生成；客户端传入的同名头被忽略。`error.requestId` 必须与响应头一致。
- 不使用 cookie 或 session；actor 完全由 Bearer token 决定。

### 1.7 错误格式

所有错误（包括 `405`、`415` 和 CSV 响应头发出前的失败）使用同一结构：

```json
{
  "error": {
    "code": "IDEMPOTENCY_CONFLICT",
    "message": "该幂等键已用于不同的请求",
    "requestId": "req-7f3a4c2e9b1d0a56",
    "details": [
      { "field": "Idempotency-Key", "reason": "REQUEST_MISMATCH" }
    ]
  }
}
```

| HTTP | error.code | 使用场景 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数、body、枚举、分页、Idempotency-Key 格式不合法 |
| 401 | `UNAUTHENTICATED` | 缺失、无效或多个身份凭证 |
| 404 | `NOT_FOUND` | 当前租户内找不到路径资源，或未知 API 路径 |
| 405 | `METHOD_NOT_ALLOWED` | 已知路径不支持该 HTTP 方法 |
| 409 | `IDEMPOTENCY_CONFLICT` | 同一作用域的幂等键对应不同业务请求 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | 非空 body 不是 `application/json` |
| 500 | `INTERNAL_ERROR` | 未分类服务端故障 |
| 503 | `TEMPORARILY_UNAVAILABLE` | SQLite 写锁等待耗尽等临时故障 |

- `error.code` 是稳定契约；`message` 是给人看的文本，不保证跨版本不变。
- `details` 可选；出现时是 `{field, reason}` 数组，`reason` 为稳定短代码。
- 原始 SQL 错误、堆栈和数据库路径只进日志，不返回给客户端。
- 业务上的 `skipped` / `failed` 位于已提交的批量操作结果中，不放进 HTTP error 对象。
- 写请求收到 `500` 或网络中断时，客户端不能据此断言未提交；批量发证必须复用原 `Idempotency-Key` 重试，吊销直接重试 PUT。
- CSV 在响应头发出后发生流中断时，只能关闭连接；下载方把截断文件视为失败，服务端不能追加 JSON 错误或声称导出成功。

## 2. 公共响应对象

### 2.1 基础引用

| 对象 | 字段 | 说明 |
| --- | --- | --- |
| `AdminRef` | `id` integer、`email` string | 管理员；不含密码或 token |
| `TenantRef` | `id` integer、`code` string、`name` string、`timezone` string | `timezone` 为 IANA 名称 |
| `LearnerRef` | `id` integer、`name` string | 学员姓名不做唯一约束 |
| `CourseRef` | `id` integer、`title` string | 课程标题不做唯一约束 |
| `CertificateSummary` | `id` integer、`serialNumber` string、`status` `ACTIVE`/`REVOKED`、`issuedAt` UTC、`issuedLocalDate` 本地日期 | 用于报名列表中的“已有证书”摘要 |
| `IssuanceEligibility` | `eligible` boolean、`reasonCode` string/null | 仅在 `eligible=false` 时 `reasonCode` 非空 |

`IssuanceEligibility.reasonCode` 只有三种：

| reasonCode | 含义 |
| --- | --- |
| `COURSE_INCOMPLETE` | 报名未完成，优先于证书检查 |
| `ALREADY_CERTIFIED` | 已完成且已有 `ACTIVE` 证书 |
| `CERTIFICATE_REVOKED` | 已完成且已有 `REVOKED` 证书；本题吊销为终态，不重发 |

资格判断顺序固定为：先看完成状态，再看已有证书。若数据异常地同时出现“未完成 + 有证书”，返回 `COURSE_INCOMPLETE`。资格是查询时的快照，提交批量发证时必须在事务内重新检查。

### 2.2 EnrollmentItem

两个报名列表使用同一条目结构：

```json
{
  "id": 1,
  "learner": { "id": 1, "name": "Alex Lee" },
  "course": { "id": 1, "title": "Safety 101" },
  "completionStatus": "COMPLETED",
  "completedAt": "2026-01-09T00:00:00.000Z",
  "certificate": null,
  "issuanceEligibility": { "eligible": true, "reasonCode": null }
}
```

- `completionStatus` 只取 `INCOMPLETE` / `COMPLETED`。
- `INCOMPLETE` 时 `completedAt` 必须为 `null`；`COMPLETED` 时必须是 UTC 时间。
- 没有证书时 `certificate` 为 `null`，不引入持久化的 `NOT_ISSUED` 状态。
- 有证书时 `certificate` 是 `CertificateSummary`，包含发证时间与本地日期；完整信息走证书详情。
- 服务端返回全部报名，不以“是否有证书”过滤；UI 才能同时看到未完成、已完成未发证、已发证和已吊销。

### 2.3 CertificateListItem 与 CertificateDetail

`CertificateListItem`：

```json
{
  "id": 3,
  "serialNumber": "CERT-EAST-TAYLOR-0001",
  "learner": { "id": 3, "name": "Taylor Kim" },
  "course": { "id": 1, "title": "Safety 101" },
  "status": "ACTIVE",
  "issuedAt": "2026-01-10T13:30:00.000Z",
  "issuedLocalDate": "2026-01-11"
}
```

`CertificateDetail`：

```json
{
  "id": 3,
  "serialNumber": "CERT-EAST-TAYLOR-0001",
  "learner": { "id": 3, "name": "Taylor Kim" },
  "course": { "id": 1, "title": "Safety 101" },
  "status": "ACTIVE",
  "issuedAt": "2026-01-10T13:30:00.000Z",
  "issuedLocalDate": "2026-01-11",
  "issuedLocalAt": "2026-01-11T00:30:00.000+11:00",
  "timezone": "Australia/Sydney",
  "issuedBy": { "id": 1, "email": "alice@east.test" },
  "revokedAt": null,
  "revokedBy": null
}
```

- `status=ACTIVE` 时 `revokedAt`、`revokedBy` 必须为 `null`；`status=REVOKED` 时两者必须非空。
- `issuedLocalAt` 是 A5 的展示来源：East 为 `2026-01-11T00:30:00.000+11:00`，West 同一 UTC 时刻为 `2026-01-10T21:30:00.000+08:00`。
- `issuedBy` / `revokedBy` 是操作管理员引用；不返回密码、token 或审计明细。
- 证书详情不返回 `tenantId`；租户由 token 隐含。

### 2.4 BulkOperationResult

```json
{
  "operationId": "bd2e0cf6-37b0-48a3-bc31-b442a84557bb",
  "courseId": 1,
  "status": "COMPLETED",
  "total": 1,
  "issued": 1,
  "skipped": 0,
  "failed": 0,
  "results": [
    {
      "learnerId": 1,
      "learnerName": "Alex Lee",
      "status": "issued",
      "reasonCode": null,
      "certificateId": 5,
      "serialNumber": "CERT-EAST-ALEX-0001"
    }
  ]
}
```

| 字段 | 类型 | 约定 |
| --- | --- | --- |
| `operationId` | string | 服务端生成的 UUID v4 |
| `courseId` | integer | 本次操作的目标课程 |
| `status` | string | 首版恒为 `COMPLETED`，由“操作已提交”派生，不是可变状态列 |
| `total` | integer | 操作创建时的报名总数；重放不随之后新增报名变化 |
| `issued` | integer | 实际发证成功数 |
| `skipped` | integer | 未尝试发证数 |
| `failed` | integer | 可恢复的逐学员失败数 |
| `results` | array | 按 `learnerId ASC` 排列的逐学员结果 |

计数不变量：`total == results.length == issued + skipped + failed`。

`BulkLearnerResult`：

```json
{
  "learnerId": 1,
  "learnerName": "Alex Lee",
  "status": "issued",
  "reasonCode": null,
  "certificateId": 5,
  "serialNumber": "CERT-EAST-ALEX-0001"
}
```

| 逐学员 status | reasonCode | certificateId / serialNumber |
| --- | --- | --- |
| `issued` | `null` | 非空，指向本次新证书 |
| `skipped` | `COURSE_INCOMPLETE` | 两者均为 `null` |
| `skipped` | `ALREADY_CERTIFIED` | 非空，指向已有 `ACTIVE` 证书 |
| `skipped` | `CERTIFICATE_REVOKED` | 非空，指向已有 `REVOKED` 证书 |
| `failed` | `ISSUANCE_ERROR` | 两者均为 `null`；错误详情只进日志 |

- `failed` 只表示单个学员的可恢复失败，其证书和审计一起回滚；数据库/事务级故障不会伪装成 `failed`，而是整个请求失败。
- `status=COMPLETED` 不表示全部发证成功；UI 必须结合三个计数展示“全部成功 / 含跳过 / 存在失败”。

### 2.5 RevocationResult

```json
{
  "certificateId": 3,
  "revokedAt": "2026-10-27T05:00:00.000Z",
  "revokedBy": { "id": 1, "email": "alice@east.test" }
}
```

### 2.6 Me

```json
{
  "admin": { "id": 1, "email": "alice@east.test" },
  "tenant": {
    "id": 1,
    "code": "EAST",
    "name": "East",
    "timezone": "Australia/Sydney"
  }
}
```

### 2.7 分页与错误

分页对象和错误对象见 1.3、1.7。`items` 和 `results` 始终是数组；`certificate`、`completedAt`、`revokedAt`、`revokedBy`、`reasonCode` 在无值时为 `null`。

## 3. 接口契约

### 3.1 GET /me

返回当前 actor 对应的管理员和租户。用于顶部显示 actor、租户和时区。

**请求**

```http
GET /api/v1/me
Authorization: Bearer demo-alice-token
```

- 无路径、query、body 参数。
- 未知 query 参数返回 `400`。

**响应 `200`**

```json
{
  "admin": { "id": 1, "email": "alice@east.test" },
  "tenant": {
    "id": 1,
    "code": "EAST",
    "name": "East",
    "timezone": "Australia/Sydney"
  }
}
```

**错误**：`401`、`400`、`500`、`503`。

### 3.2 GET /courses

返回当前租户的课程选项，供批量发证选择课程。

**请求**

| 参数 | 位置 | 规则 |
| --- | --- | --- |
| `page` | query | 见 1.3 |
| `pageSize` | query | 见 1.3 |

```http
GET /api/v1/courses?page=1&pageSize=20
Authorization: Bearer demo-alice-token
```

- 不支持搜索；课程数量小，前端按需继续加载后续页。
- 未知 query 参数或重复参数返回 `400`。

**响应 `200`**

```json
{
  "items": [
    { "id": 1, "title": "Safety 101" },
    { "id": 2, "title": "First Aid" }
  ],
  "page": 1,
  "pageSize": 20,
  "total": 2
}
```

- 排序固定为 `course.id ASC`。
- 租户没有课程时返回 `200` 和空 `items`。

**错误**：`400`、`401`、`500`、`503`。

### 3.3 GET /courses/{courseId}/enrollments

返回某课程在当前租户内的全部报名，供发证前查看完成状态、已有证书和资格。

**请求**

| 参数 | 位置 | 规则 |
| --- | --- | --- |
| `courseId` | path | 正整数；必须属于当前租户 |
| `completionStatus` | query | 可选，`INCOMPLETE` / `COMPLETED` |
| `page` | query | 见 1.3 |
| `pageSize` | query | 见 1.3 |

```http
GET /api/v1/courses/1/enrollments?page=1&pageSize=20
Authorization: Bearer demo-alice-token
```

**响应 `200`**

```json
{
  "items": [
    {
      "id": 1,
      "learner": { "id": 1, "name": "Alex Lee" },
      "course": { "id": 1, "title": "Safety 101" },
      "completionStatus": "COMPLETED",
      "completedAt": "2026-01-09T00:00:00.000Z",
      "certificate": null,
      "issuanceEligibility": { "eligible": true, "reasonCode": null }
    },
    {
      "id": 2,
      "learner": { "id": 2, "name": "Sam Chen" },
      "course": { "id": 1, "title": "Safety 101" },
      "completionStatus": "INCOMPLETE",
      "completedAt": null,
      "certificate": null,
      "issuanceEligibility": { "eligible": false, "reasonCode": "COURSE_INCOMPLETE" }
    },
    {
      "id": 3,
      "learner": { "id": 3, "name": "Taylor Kim" },
      "course": { "id": 1, "title": "Safety 101" },
      "completionStatus": "COMPLETED",
      "completedAt": "2026-01-08T00:00:00.000Z",
      "certificate": {
        "id": 3,
        "serialNumber": "CERT-EAST-TAYLOR-0001",
        "status": "ACTIVE",
        "issuedAt": "2026-01-10T13:30:00.000Z",
        "issuedLocalDate": "2026-01-11"
      },
      "issuanceEligibility": { "eligible": false, "reasonCode": "ALREADY_CERTIFIED" }
    },
    {
      "id": 4,
      "learner": { "id": 4, "name": "Morgan Wu" },
      "course": { "id": 1, "title": "Safety 101" },
      "completionStatus": "COMPLETED",
      "completedAt": "2026-01-07T00:00:00.000Z",
      "certificate": {
        "id": 4,
        "serialNumber": "CERT-EAST-MORGAN-0001",
        "status": "REVOKED",
        "issuedAt": "2026-01-09T13:30:00.000Z",
        "issuedLocalDate": "2026-01-10"
      },
      "issuanceEligibility": { "eligible": false, "reasonCode": "CERTIFICATE_REVOKED" }
    }
  ],
  "page": 1,
  "pageSize": 20,
  "total": 4
}
```

- 排序固定为 `enrollment.id ASC`。
- 课程存在但没有报名时返回 `200` 和空 `items`。
- 课程不存在或属于其他租户时返回 `404`。
- `issuanceEligibility` 只说明查询时资格，批量提交时重新检查。

**错误**：`400`、`401`、`404`、`500`、`503`。

### 3.4 GET /learners/{learnerId}/enrollments

返回某学员在当前租户内各课程的完成状态、证书和资格。

**请求**

| 参数 | 位置 | 规则 |
| --- | --- | --- |
| `learnerId` | path | 正整数；必须属于当前租户 |
| `courseId` | query | 可选；正整数，仅作为本租户内的过滤条件 |
| `completionStatus` | query | 可选，`INCOMPLETE` / `COMPLETED` |
| `page` | query | 见 1.3 |
| `pageSize` | query | 见 1.3 |

```http
GET /api/v1/learners/1/enrollments?page=1&pageSize=20
Authorization: Bearer demo-alice-token
```

**响应 `200`**：结构与 3.3 相同，`items` 是 `EnrollmentItem`，排序固定为 `enrollment.id ASC`。

```json
{
  "items": [
    {
      "id": 1,
      "learner": { "id": 1, "name": "Alex Lee" },
      "course": { "id": 1, "title": "Safety 101" },
      "completionStatus": "COMPLETED",
      "completedAt": "2026-01-09T00:00:00.000Z",
      "certificate": null,
      "issuanceEligibility": { "eligible": true, "reasonCode": null }
    },
    {
      "id": 5,
      "learner": { "id": 1, "name": "Alex Lee" },
      "course": { "id": 2, "title": "First Aid" },
      "completionStatus": "COMPLETED",
      "completedAt": "2026-01-09T00:00:00.000Z",
      "certificate": null,
      "issuanceEligibility": { "eligible": true, "reasonCode": null }
    }
  ],
  "page": 1,
  "pageSize": 20,
  "total": 2
}
```

- 学员不存在或属于其他租户时返回 `404`。
- `courseId` 不存在、属于其他租户或该学员未报名时，返回 `200` 和空 `items`，不额外暴露课程是否存在。
- `courseId` 语法非法时返回 `400`。

**错误**：`400`、`401`、`404`、`500`、`503`。

### 3.5 GET /certificates

证书列表。搜索覆盖学员姓名或序列号；支持状态/课程/学员筛选、排序和分页。

**请求**

| 参数 | 位置 | 默认 | 规则 |
| --- | --- | --- | --- |
| `q` | query | 无 | 学员姓名或序列号的字面子串；去除首尾空白；空串等同不筛选 |
| `status` | query | 无 | `ACTIVE` / `REVOKED`；区分大小写 |
| `courseId` | query | 无 | 正整数；本租户内过滤，不校验存在性 |
| `learnerId` | query | 无 | 正整数；本租户内过滤，不校验存在性 |
| `sortBy` | query | `issuedLocalDate` | `learnerName` / `serialNumber` / `issuedLocalDate` |
| `sortOrder` | query | 见下表 | `asc` / `desc` |
| `page` | query | `1` | 见 1.3 |
| `pageSize` | query | `20` | 见 1.3 |

`q` 的规则：

- 只匹配 `learner.name` 或 `certificate.serial_number`，不匹配课程标题。
- 字面子串匹配；`%` 和 `_` 是普通字符，不是 SQL 通配符。
- ASCII 字母不区分大小写；非 ASCII 字符按原样比较。
- 首尾空白被去除；中间空白保留；去除后为空则不筛选。
- `q` 内部两个字段按 OR 组合；与其他筛选按 AND 组合。

排序规则：

| `sortBy` | 默认 `sortOrder` | 排序键 |
| --- | --- | --- |
| `learnerName` | `asc` | 学员姓名，SQLite BINARY 比较 |
| `serialNumber` | `asc` | 序列号，BINARY 比较 |
| `issuedLocalDate` | `desc` | 租户本地日期，字典序 |

- 如果显式提供 `sortOrder`，覆盖该字段的默认方向；例如 `sortBy=issuedLocalDate&sortOrder=asc` 表示最旧优先。
- 如果只提供 `sortOrder`，它作用于默认的 `sortBy=issuedLocalDate`。
- 所有排序最后追加 `certificate.id ASC`；即使主排序是 `desc`，次排序仍为 `id ASC`。这是 A6 重复导出一致性的保证。
- 不支持按 `issuedAt`、`status`、课程标题排序；传入未知 `sortBy` 返回 `400`。

```http
GET /api/v1/certificates?courseId=1&sortBy=issuedLocalDate&sortOrder=desc&page=1&pageSize=20
Authorization: Bearer demo-alice-token
```

**响应 `200`**

```json
{
  "items": [
    {
      "id": 3,
      "serialNumber": "CERT-EAST-TAYLOR-0001",
      "learner": { "id": 3, "name": "Taylor Kim" },
      "course": { "id": 1, "title": "Safety 101" },
      "status": "ACTIVE",
      "issuedAt": "2026-01-10T13:30:00.000Z",
      "issuedLocalDate": "2026-01-11"
    },
    {
      "id": 4,
      "serialNumber": "CERT-EAST-MORGAN-0001",
      "learner": { "id": 4, "name": "Morgan Wu" },
      "course": { "id": 1, "title": "Safety 101" },
      "status": "REVOKED",
      "issuedAt": "2026-01-09T13:30:00.000Z",
      "issuedLocalDate": "2026-01-10"
    }
  ],
  "page": 1,
  "pageSize": 20,
  "total": 2
}
```

- 不传 `status` 时同时返回 `ACTIVE` 和 `REVOKED`。
- `courseId` / `learnerId` 匹配不到本租户数据时返回 `200` 和空 `items`，不返回 `404`。
- 越界页返回 `200` 和空 `items`，`total` 仍是匹配总数。
- 列表条目不包含 `revokedAt`、`revokedBy`；这些只在详情返回。

**错误**：`400`、`401`、`500`、`503`。

### 3.6 GET /certificates/{certificateId}

证书详情。

**请求**

- `certificateId`：path，正整数。
- 不支持 query 参数；未知 query 返回 `400`。

```http
GET /api/v1/certificates/3
Authorization: Bearer demo-alice-token
```

**响应 `200`**

```json
{
  "id": 3,
  "serialNumber": "CERT-EAST-TAYLOR-0001",
  "learner": { "id": 3, "name": "Taylor Kim" },
  "course": { "id": 1, "title": "Safety 101" },
  "status": "ACTIVE",
  "issuedAt": "2026-01-10T13:30:00.000Z",
  "issuedLocalDate": "2026-01-11",
  "issuedLocalAt": "2026-01-11T00:30:00.000+11:00",
  "timezone": "Australia/Sydney",
  "issuedBy": { "id": 1, "email": "alice@east.test" },
  "revokedAt": null,
  "revokedBy": null
}
```

吊销后同一资源返回（`id=3` 的发证信息保持不变，只追加吊销信息）：

```json
{
  "id": 3,
  "serialNumber": "CERT-EAST-TAYLOR-0001",
  "learner": { "id": 3, "name": "Taylor Kim" },
  "course": { "id": 1, "title": "Safety 101" },
  "status": "REVOKED",
  "issuedAt": "2026-01-10T13:30:00.000Z",
  "issuedLocalDate": "2026-01-11",
  "issuedLocalAt": "2026-01-11T00:30:00.000+11:00",
  "timezone": "Australia/Sydney",
  "issuedBy": { "id": 1, "email": "alice@east.test" },
  "revokedAt": "2026-10-27T05:00:00.000Z",
  "revokedBy": { "id": 1, "email": "alice@east.test" }
}
```

- 证书不存在或属于其他租户时返回 `404`。
- 详情同时返回 UTC 时间、本地日期、本地时间和 IANA 时区；UI 用 `issuedLocalAt` 展示 A5 的 `2026-01-11 00:30` / `2026-01-10 21:30`。
- 审计事实保存在数据库，不在详情中返回审计列表；`issuedBy` / `revokedBy` 是操作者引用。

**错误**：`400`、`401`、`404`、`500`、`503`。

### 3.7 POST /courses/{courseId}/bulk-operations

对课程全部报名执行一次同步批量发证。每次请求是一个逻辑操作，使用 `Idempotency-Key` 区分重试和新操作。

**请求**

| 项目 | 规则 |
| --- | --- |
| `courseId` | path，正整数；必须属于当前租户 |
| `Idempotency-Key` | header，必填；必须且只能出现一次；1–128 位 `[A-Za-z0-9_-]`；区分大小写 |
| body | 可省略、空 body 或 `{}`；三者等价；不接受任何业务字段；未知成员、`null`、数组、损坏 JSON 一律 `400` |
| query | 不允许；出现任何 query 参数返回 `400` |
| Content-Type | 有非空 body 时必须为 `application/json` |

请求示例：

```http
POST /api/v1/courses/1/bulk-operations
Authorization: Bearer demo-alice-token
Idempotency-Key: 6f1c2b7e-8a4d-4c91-b2e3-1f0a9d7c5e42
Content-Type: application/json

{}
```

- 唯一业务参数是路径中的 `courseId`。body 不承载业务参数；本 API 不接收 `learnerIds` 子集、自定义发证时刻或其他字段。省略 body、空 body、`{}` 解析后等价为空对象，均合法。
- 服务端把请求规范化为 `{"courseId": <path id>}`，计算 SHA-256 指纹，并按 `tenantId + adminId + Idempotency-Key` 保存已提交结果。body 不参与业务指纹。
- 同一键用于不同 `courseId` 属于不同请求，返回 `409`，不执行第二次操作、不覆盖原结果。
- body 的语法校验先于幂等查找：`{"tenantId":1}`、其他未知成员、`null`、数组、损坏 JSON 都返回 `400`，无论键是否存在。这样不会出现“已有键时未知字段变 409”的歧义。
- 因为 body 没有业务字段，A3 的“同键不同请求”以不同 `courseId` 验证；不存在“同键不同有效 payload”的合法请求。

**body 形态与结果**（先校验 body 语法；body 合法后，课程必须属于当前租户，跨租户/不存在返回 `404`，不查键）：

| body | 新键 | 已有键 + 同课程 | 已有键 + 不同课程 |
| --- | --- | --- | --- |
| 省略 / 空 body / `{}` | `201` 新操作 | `200` 原样回放 | `409` |
| 未知成员（含 `tenantId`） | `400` | `400` | `400` |
| `null` / 数组 / 损坏 JSON | `400` | `400` | `400` |

**响应 `201 Created`（首次提交）**

**响应 `200 OK`（同键同请求重放）**

两种状态返回同一份已存储的 JSON：

```json
{
  "operationId": "bd2e0cf6-37b0-48a3-bc31-b442a84557bb",
  "courseId": 1,
  "status": "COMPLETED",
  "total": 4,
  "issued": 1,
  "skipped": 3,
  "failed": 0,
  "results": [
    {
      "learnerId": 1,
      "learnerName": "Alex Lee",
      "status": "issued",
      "reasonCode": null,
      "certificateId": 5,
      "serialNumber": "CERT-EAST-ALEX-0001"
    },
    {
      "learnerId": 2,
      "learnerName": "Sam Chen",
      "status": "skipped",
      "reasonCode": "COURSE_INCOMPLETE",
      "certificateId": null,
      "serialNumber": null
    },
    {
      "learnerId": 3,
      "learnerName": "Taylor Kim",
      "status": "skipped",
      "reasonCode": "ALREADY_CERTIFIED",
      "certificateId": 3,
      "serialNumber": "CERT-EAST-TAYLOR-0001"
    },
    {
      "learnerId": 4,
      "learnerName": "Morgan Wu",
      "status": "skipped",
      "reasonCode": "CERTIFICATE_REVOKED",
      "certificateId": 4,
      "serialNumber": "CERT-EAST-MORGAN-0001"
    }
  ]
}
```

**行为约定**

1. 先校验身份、body 语法和课程归属。跨租户课程返回 `404`，在创建操作、证书和审计之前拒绝。
2. 开启 `BEGIN IMMEDIATE` 写事务；按 `tenantId + adminId + key` 查找幂等记录。
3. 已有同指纹结果：原样返回已存 JSON，HTTP `200`；不重新计算、不因后续吊销或新增报名改变历史结果。
4. 已有不同指纹（不同 `courseId`）：返回 `409`，不执行第二次操作，不覆盖原结果。
5. 新请求：生成 `operationId`，按 `learner_id ASC` 遍历该课程全部报名，逐学员判断并写入证书、审计和最终操作结果；全部变更与 `bulk_operations` 在同一事务提交。
6. 逐学员判断顺序固定：未完成 → `skipped / COURSE_INCOMPLETE`；已有 `ACTIVE` 证书 → `skipped / ALREADY_CERTIFIED`；已有 `REVOKED` 证书 → `skipped / CERTIFICATE_REVOKED`；否则发证 → `issued`。单个学员的可恢复错误回滚该学员的证书和审计，记为 `failed / ISSUANCE_ERROR`。
7. 全部报名都 `skipped`、全部 `failed` 或课程没有报名时，只要操作提交成功，仍返回 `201`（重放 `200`），body 是上述结果；空课程为 `total=0`、三个计数为 0、`results=[]`。`201` 表示“操作已提交”，不表示“每个学员都发证成功”。
8. 事务级故障整体回滚，不返回成功对象；`500` / `503` 的具体语义见 1.7。
9. 同一键且同一课程的并发请求由数据库事务串行化：首个提交者返回 `201`，其余读取已提交结果返回 `200`；所有调用者得到同一份逻辑结果，不产生第二张证书或第二条发证审计。服务端应为同键并发保留足够的锁等待；`503` 只用于等待超限等临时故障，客户端必须用原键重试。
10. 同一管理员对同一课程发起新的业务操作时，必须使用新的 `Idempotency-Key`；复用旧键只会得到旧结果。不同管理员即使同租户也各自独立作用域。

**错误**：`400`（参数/键/body 非法）、`401`、`404`（课程不在当前租户）、`409`（同键不同课程）、`415`、`500`、`503`。

首版不返回 `202`，不引入后台队列或轮询。也不返回 `Location`；如以后启用可选的 `GET /bulk-operations/{operationId}`，再增加该头。

### 3.8 PUT /certificates/{certificateId}/revocation

创建证书的单例吊销记录。吊销不删除证书；列表和详情继续返回 `REVOKED` 状态。

**请求**

| 项目 | 规则 |
| --- | --- |
| `certificateId` | path，正整数；必须属于当前租户 |
| body | 可省略、空 body 或 `{}`；不接受任何字段 |
| query | 不允许；出现任何 query 参数返回 `400` |
| Content-Type | 有非空 body 时必须为 `application/json` |

请求示例：

```http
PUT /api/v1/certificates/3/revocation
Authorization: Bearer demo-alice-token
Content-Type: application/json

{}
```

**响应 `201 Created`（首次 ACTIVE → REVOKED）**

**响应 `200 OK`（已经 REVOKED，重复请求）**

两种状态返回同一结构：

```json
{
  "certificateId": 3,
  "revokedAt": "2026-10-27T05:00:00.000Z",
  "revokedBy": { "id": 1, "email": "alice@east.test" }
}
```

**行为约定**

1. 证书不存在或属于其他租户时返回 `404`，不写审计。
2. `ACTIVE`：在事务内做条件状态更新，写一条 `REVOKED` 审计，提交后返回 `201`。
3. 已 `REVOKED`：返回第一次吊销的 `revokedAt` / `revokedBy`，HTTP `200`；不新增审计，不改变第一次的时间和操作者。
4. 并发重复吊销同样收敛为一次状态变更和一条审计；一个调用返回 `201`，其余返回 `200`，`revokedBy` 是实际第一次执行吊销的管理员。
5. 吊销不修改 `issuedAt`、`issuedLocalDate`、`issuedLocalAt`、`serialNumber` 或发证审计。
6. 本题不接收吊销原因，不提供恢复或重新发证。已吊销证书再次参与批量发证时仍为 `skipped / CERTIFICATE_REVOKED`。
7. 吊销没有单独的幂等键；PUT 的资源语义本身就是幂等重试机制。首次响应丢失时直接重试同一 PUT。

**错误**：`400`、`401`、`404`、`415`、`500`、`503`。

### 3.9 GET /certificates.csv

按证书列表相同的租户、搜索、筛选和排序规则，流式导出全部匹配行。导出不分页。

**请求**

| 参数 | 规则 |
| --- | --- |
| `q` | 同 3.5 |
| `status` | 同 3.5 |
| `courseId` | 同 3.5 |
| `learnerId` | 同 3.5 |
| `sortBy` | 同 3.5 |
| `sortOrder` | 同 3.5 |
| `page` / `pageSize` | 不允许；出现即 `400`，避免误以为只导出当前页 |

- 未知 query、重复 query、非法枚举返回 `400`。
- `/certificates.csv` 是一个独立的静态路径，不是 `/certificates/{certificateId}` 的子路径；实现时不要注册成 `/certificates/csv`，否则会与详情路由的 ID 解析混淆。
- 导出使用与列表相同的查询构造、租户条件和 `certificate.id ASC` 次排序。
- 认证同样使用 Bearer token；浏览器端应使用 `fetch` + `Blob` 下载，不能依赖裸 `<a>` 链接自动携带 Authorization 头。

```http
GET /api/v1/certificates.csv?q=Load%20Learner%20007&sortBy=serialNumber&sortOrder=asc
Authorization: Bearer demo-alice-token
```

**响应 `200`**

```http
Content-Type: text/csv; charset=utf-8
Content-Disposition: attachment; filename="certificates.csv"
Cache-Control: no-store
X-Request-Id: req-7f3a4c2e9b1d0a56
Transfer-Encoding: chunked
```

CSV 固定四列，顺序与题目一致：

```csv
learnerName,courseTitle,serialNumber,issuedLocalDate
Load Learner 007,Export Load,EAST-LOAD-000007,2026-01-09
```

**CSV 格式约定**

- UTF-8，不带 BOM；行分隔符为 CRLF；每条记录（含最后一条）以 CRLF 结束。
- 第一行固定为表头，列名和顺序固定为 `learnerName,courseTitle,serialNumber,issuedLocalDate`。
- `issuedLocalDate` 是租户本地日历日期，不是 UTC 日期。
- 字段包含逗号、双引号、CR 或 LF 时，用双引号包裹；内部双引号写两次。字段首尾有空白时也加引号以保留空白。
- 防电子表格公式执行：`learnerName` 和 `courseTitle` 若首个非空白字符是 `=`、`+`、`-`、`@`、制表符、回车或换行，则在原值前加一个半角单引号 `'`。`serialNumber` 由系统生成，不按用户文本处理。
- 没有匹配行时返回 `200` 和只有表头的一行，不返回空文件、`204` 或 `404`。
- 导出在一个一致性读取快照中完成；不得先分页再拼接。
- 流式写出，不预先把全部行聚合到内存；不要求 `Content-Length`。60 秒是 A6 的验收门槛，不是未经实测的性能承诺。
- 导出使用独立读连接/只读事务，不阻塞列表读取；A6 要求导出期间证书列表仍能成功响应。
- 响应头发出前的失败用 JSON 错误；响应头发出后的读取/写入错误必须中止连接，使客户端得到网络错误或明确的截断失败。不能写完部分 CSV 后正常 `return`，否则浏览器可能保存一份不完整文件。Go 实现可使用 `http.ErrAbortHandler`；需确认 Recovery 中间件不会把它当作普通 panic 吞掉。验收应注入一次导出中途读取错误，确认浏览器下载失败。

**错误**：`400`、`401`、`500`、`503`。

## 4. 跨接口行为

### 4.1 证书筛选与排序汇总

| 行为 | 列表 | CSV |
| --- | --- | --- |
| 租户范围 | token 决定 | token 决定 |
| `q` 搜索学员姓名/序列号 | 支持 | 支持 |
| `status` | 支持 | 支持 |
| `courseId` / `learnerId` | 支持 | 支持 |
| `sortBy` / `sortOrder` | 支持 | 支持 |
| 分页 | 支持 | 不支持，传 `page`/`pageSize` 返回 `400` |
| 次排序 | `certificate.id ASC` | 同左 |
| 空结果 | `200` 空 `items` | `200` 仅表头 |

- 列表和 CSV 必须复用同一套过滤、排序和本地日期语义；不能一个用 UTC 日期、另一个用本地日期。
- 列表的 `total`/`items` 与 CSV 都在各自请求的同一读事务内计算；跨请求、跨页不要求同一快照。
- 搜索的 `%` / `_` 在两处都按字面处理。

### 4.2 批量发证幂等状态机

| 情况 | HTTP | 是否创建/修改数据 |
| --- | --- | --- |
| 新键 + 合法课程 + 合法 body | `201` | 创建操作、证书、发证审计 |
| 同键 + 同课程 + 合法 body | `200` | 不创建、不修改；原样回放 |
| 同键 + 不同课程 + 合法 body | `409` | 不执行第二次操作，不覆盖原结果 |
| 新键/已有键 + 非法 body/参数/键 | `400` / `415` | 无副作用；语法先于幂等 |
| 新键/已有键 + 跨租户/不存在课程 + 合法 body | `404` | 无副作用；课程归属先于幂等 |
| 事务级故障 | `500` / `503` | 整体回滚；若已提交，重试同键得 `200` |

- 幂等键只在操作成功提交后持久化；失败请求不消耗键。
- body 只允许省略/空/`{}`；其语法错误先于幂等查找返回 `400`，不参与 `409`。
- 同一键的并发请求串行化；所有成功调用者得到同一份结果。
- 重放不按当前报名或证书状态重新计算。操作后新报名、证书被吊销、课程标题变化都不改变历史结果。
- 客户端为一次用户操作生成一个键；网络重试、超时、`500`/`503` 复用原键；用户发起新操作才生成新键。

### 4.3 吊销状态机

| 当前状态 | HTTP | 结果 |
| --- | --- | --- |
| `ACTIVE` | `201` | 更新为 `REVOKED`，写一条吊销审计 |
| `REVOKED` | `200` | 返回第一次吊销信息，不新增审计 |
| 不存在/跨租户 | `404` | 无副作用 |

- 吊销是终态。本题不重发、不恢复；同一课程/学员唯一证书约束不变。
- 重复吊销不改变 `revokedAt` / `revokedBy`；审计唯一约束保证最多一条 `REVOKED` 事件。
- 发证和吊销审计都必须与对应状态变更在同一事务提交。

### 4.4 租户隔离矩阵

| 请求 | 跨租户结果 |
| --- | --- |
| `GET /courses/{id}/enrollments` | `404` |
| `GET /learners/{id}/enrollments` | `404` |
| `GET /certificates/{id}` | `404` |
| `POST /courses/{id}/bulk-operations` | `404`，无证书/审计 |
| `PUT /certificates/{id}/revocation` | `404`，无审计 |
| `GET /certificates?courseId=` | `200` 空列表，不泄露存在性 |
| `GET /certificates?learnerId=` | `200` 空列表，不泄露存在性 |
| `GET /certificates.csv?courseId=` | `200` 仅表头 |
| 任何请求携带 `tenantId` query/body | `400`，不使用客户端租户 |
| 任何请求携带 `X-Tenant-Id` | 忽略，仍按 token 租户 |

### 4.5 时间与本地日期

- `issuedAt` 是时间事实来源，UTC 固定格式；`issuedLocalDate` 和 `issuedLocalAt` 由租户 IANA 时区派生。
- East `Australia/Sydney` 在 A5 时刻 `2026-01-10T13:30:00.000Z` 对应 `2026-01-11T00:30:00.000+11:00`。
- West `Australia/Perth` 在同一 UTC 时刻对应 `2026-01-10T21:30:00.000+08:00`。
- 列表和 CSV 使用本地日历日期排序；详情展示本地日期和时间。不能写死 UTC 偏移，也不能使用浏览器时区。
- 普通发证、种子数据和 100,000 行生成器必须复用同一套时区转换规则。

### 4.6 派生字段与不变量

- `certificate.status` 是持久化状态；没有 `PENDING` / `NOT_ISSUED`。
- `enrollment.completionStatus` 与 `completedAt` 必须一致。
- `issuanceEligibility` 是查询时派生值，不落盘；提交批量发证时重新检查。
- 批量操作的 `status=COMPLETED` 是“已提交”事实的派生展示，不是可变列。
- `total = issued + skipped + failed = results.length`；`results` 按 `learnerId ASC`。
- 证书 `ACTIVE` ↔ 吊销字段为空；`REVOKED` ↔ 吊销字段非空。
- 每张证书恰好一条 `ISSUED` 审计、最多一条 `REVOKED` 审计；每次实际状态变更与审计同事务。

## 5. A1–A7 对照

| 场景 | 本 API 的保证 |
| --- | --- |
| A1 租户隔离 | 所有接口由 token 解析租户；路径资源跨租户统一 `404`；列表/CSV 的租户条件不可绕过；跨租户 bulk 在写操作前返回 `404`，不创建证书/审计；不接受客户端 `tenantId` |
| A2 重试安全 | `POST /courses/{id}/bulk-operations` 同键同课程返回同一份已存结果；顺序和并发调用都只产生一个逻辑操作；证书唯一约束和审计唯一约束防止重复；所有成功调用者得到同一结果 |
| A3 键误用 | 同键不同 `courseId` 返回 `409`；不执行第二次操作、不覆盖原结果。body 不承载业务参数，未知 body 一律 `400`；A3 的复用测试以不同课程验证 |
| A4 学员结果 | 批量响应按 `learnerId ASC` 返回全部报名；`issued` / `skipped` / `failed` 及 `reasonCode` 覆盖已完成、未完成、已有 ACTIVE、已有 REVOKED；只有符合资格者发证 |
| A5 DST 与本地日期 | 详情 `issuedLocalAt` 和列表/CSV `issuedLocalDate` 使用租户 IANA 时区；East/West 的 A5 时刻分别为 `2026-01-11 00:30` / `2026-01-10 21:30`；列表排序和 CSV 使用本地日历日期 |
| A6 导出增长 | `GET /certificates.csv` 复用列表的租户、搜索、筛选和排序，不分页；固定四列；`certificate.id ASC` 次排序保证相同排序键的稳定顺序；流式写出，导出期间列表仍可读；60 秒为实测门槛；中途读取错误必须中止连接，并用“浏览器下载失败”验证 |
| A7 吊销审计 | `PUT /certificates/{id}/revocation` 首次 `ACTIVE → REVOKED` 返回 `201` 并写一条吊销审计；重复返回 `200` 和第一次信息，不新增审计；并发重复同样收敛为一条审计 |

## 6. 评审结论与本版取舍

本版在初稿基础上补齐了输入输出，并按题目做了以下修正：

1. **CSV 列收窄为题目要求的四列**：`learnerName,courseTitle,serialNumber,issuedLocalDate`。初稿额外包含 `certificateId`、`status`、`issuedAt`；虽然信息更多，但“导出 learner name, course title, serial number, and tenant-local issue date”按字面更稳妥地按四列实现。列表接口仍保留 `id`、`status`、`issuedAt` 供 UI 使用。
2. **补齐所有接口的精确请求/响应**：包括 `/me`、课程、两个报名列表、详情、批量、吊销和 CSV；初稿只给了部分示例。
3. **固定时间格式**：所有 `*At` 使用 `YYYY-MM-DDTHH:mm:ss.SSSZ`，`issuedLocalAt` 使用带偏移的本地时间；避免“RFC 3339”在毫秒和偏移上的歧义。
4. **固定搜索语义**：`q` 是学员姓名/序列号的字面子串，ASCII 不区分大小写，`%`/`_` 不是通配符，首尾空白去除。
5. **固定排序默认值**：`learnerName` / `serialNumber` 默认 `asc`，`issuedLocalDate` 默认 `desc`；始终追加 `certificate.id ASC`，保证 A6 重复导出一致。
6. **固定校验顺序和错误码**：认证 → 语法 → 资源归属 → 业务；补充 `405`、`415`、`503`、`X-Request-Id`、`Retry-After`、`Allow` 和统一错误结构。
7. **固定幂等生命周期**：键只在成功提交后持久化；同键同课程返回 `200` 回放；同键不同课程返回 `409`；并发同键串行化；重放不因后续吊销或新增报名改变。body 不承载业务参数，非法 body 一律 `400`。
8. **固定吊销语义**：首次 `201`、重复 `200`；保留第一次 `revokedAt` / `revokedBy`；不新增审计；吊销为终态。
9. **固定 CSV 行为**：不传分页、固定四列、CRLF、UTF-8 无 BOM、RFC 4180 转义、公式注入防护、空结果只有表头、响应头前错误用 JSON、响应头后错误中止连接并用下载失败验证。
10. **补充逐学员 `learnerName`**：初稿只有 `learnerId`，UI 无法独立展示结果；现在结果自包含。报名列表的证书摘要也补充 `issuedAt` / `issuedLocalDate`。

实现采用的取舍（已确认）：

- **可选接口**：`GET /learners`（按姓名找 learnerId）和 `GET /bulk-operations/{operationId}`（刷新后读取历史操作）不在首版。如果 UI 需要独立的学员搜索页或刷新后恢复操作详情，应在实现前加入；当前流程可从课程报名列表取得 learnerId，并用原幂等键重试恢复结果。
- **A3 的“request payload”**：本接口的唯一业务参数是路径 `courseId`，body 固定为空/`{}`，不参与业务指纹。因此 A3 的同键复用测试以不同 `courseId` 验证；任何 body 成员（包括 `tenantId`）都是非法请求，统一 `400`，避免与“未知字段 400”的规则冲突。若未来增加可变业务字段，必须把它们纳入规范化和指纹，并定义其 `409` 语义。
- **吊销不可重发**：题目没有明确重发规则，本契约保持吊销终态。如果要求吊销后可重新发证，需要调整唯一约束、审计和 `issuanceEligibility`。
- **CSV 无 BOM**：便于程序比对和 A6 的逐行检查；如果要求 Excel 直接打开中文 CSV，需要另行决定是否加 BOM，并同步调整验收。
- **同步批量**：题目未要求后台任务；首版同步。若单个课程报名量极大导致请求超时，需要另行设计异步任务，不属于本契约。

## 7. 不在本契约

- 登录、注册、token 签发/刷新、密码、角色管理。
- 课程/学员的创建、编辑、删除，手动改完成状态。
- 单张证书发证、吊销原因、恢复证书、吊销后重发。
- 公开证书验真页面或 API。
- 异步批量任务、队列、进度轮询、任务取消。
- 审计事件查询 API（审计仍由服务端持久化）。
- CSV 分页、导出任务、导出历史。
- 客户端选择租户、跨租户管理员列表。
- CORS、cookie、session；前端与 API 同源，开发时用 Vite 代理。
