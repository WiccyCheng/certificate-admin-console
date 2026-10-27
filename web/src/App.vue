<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ApiError,
  bulkIssue,
  exportCertificates,
  getCertificate,
  getMe,
  listCertificates,
  listCourseEnrollments,
  listCourses,
  revokeCertificate
} from './api'
import type {
  BulkOperationResult,
  CertificateDetail,
  CertificateFilters,
  CertificateListItem,
  Course,
  Enrollment,
  Me,
  Page
} from './types'

const actors = [
  { token: 'demo-alice-token', label: 'Alice · East' },
  { token: 'demo-bob-token', label: 'Bob · West' }
]

const actorToken = ref(actors[0].token)
const me = ref<Me | null>(null)
const courses = ref<Course[]>([])
const error = ref('')
const loading = ref(false)
const exporting = ref(false)

const emptyPage = (pageSize: number): Page<CertificateListItem> => ({
  items: [],
  page: 1,
  pageSize,
  total: 0
})

const defaultFilters = (): CertificateFilters => ({
  q: '',
  status: '',
  courseId: undefined,
  sortBy: 'issuedLocalDate',
  sortOrder: 'desc',
  page: 1,
  pageSize: 20
})

const filters = reactive<CertificateFilters>(defaultFilters())
const certificates = ref<Page<CertificateListItem>>(emptyPage(filters.pageSize))

const detailVisible = ref(false)
const detail = ref<CertificateDetail | null>(null)
const detailLoading = ref(false)
const revoking = ref(false)

const issueVisible = ref(false)
const issueCourseId = ref<number | undefined>(undefined)
const enrollments = ref<Enrollment[]>([])
const enrollmentTotal = ref(0)
const enrollmentsLoading = ref(false)
const issuing = ref(false)
const bulkResult = ref<BulkOperationResult | null>(null)
const issueError = ref('')
const issueKey = ref<string | null>(null)

let epoch = 0
let listController: AbortController | null = null
let detailController: AbortController | null = null
let enrollmentController: AbortController | null = null

const currentActorLabel = computed(() => actors.find((actor) => actor.token === actorToken.value)?.label ?? '')
const issueCourseTitle = computed(() => courses.value.find((course) => course.id === issueCourseId.value)?.title ?? '')
const bulkTone = computed(() => {
  if (!bulkResult.value) return 'success'
  if (bulkResult.value.failed > 0) return 'danger'
  if (bulkResult.value.skipped > 0) return 'warning'
  return 'success'
})

function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    const requestId = err.requestId ? `（${err.requestId}）` : ''
    return `${err.message}${requestId}`
  }
  if (err instanceof Error) return err.message
  return '未知错误'
}

function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

async function loadMe(currentEpoch: number, token: string): Promise<void> {
  try {
    const result = await getMe(token)
    if (currentEpoch !== epoch) return
    me.value = result
  } catch (err) {
    if (currentEpoch !== epoch || isAbort(err)) return
    error.value = errorText(err)
  }
}

async function loadCourses(currentEpoch: number, token: string): Promise<void> {
  try {
    const result = await listCourses(token)
    if (currentEpoch !== epoch) return
    courses.value = result.items
  } catch (err) {
    if (currentEpoch !== epoch || isAbort(err)) return
    error.value = errorText(err)
  }
}

async function loadCertificates(page = filters.page): Promise<void> {
  filters.page = page
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  const currentEpoch = epoch
  const token = actorToken.value
  loading.value = true
  try {
    const result = await listCertificates(token, filters, controller.signal)
    if (currentEpoch !== epoch) return
    certificates.value = result
  } catch (err) {
    if (currentEpoch !== epoch || isAbort(err)) return
    error.value = errorText(err)
  } finally {
    if (listController === controller) loading.value = false
  }
}

async function bootstrap(): Promise<void> {
  const currentEpoch = epoch
  const token = actorToken.value
  await Promise.all([loadMe(currentEpoch, token), loadCourses(currentEpoch, token)])
  if (currentEpoch === epoch) await loadCertificates(1)
}

async function switchActor(): Promise<void> {
  epoch++
  listController?.abort()
  detailController?.abort()
  enrollmentController?.abort()
  detailVisible.value = false
  issueVisible.value = false
  detail.value = null
  certificates.value = emptyPage(filters.pageSize)
  courses.value = []
  me.value = null
  bulkResult.value = null
  enrollments.value = []
  issueError.value = ''
  error.value = ''
  filters.page = 1
  await bootstrap()
}

function applyFilters(): void {
  void loadCertificates(1)
}

function resetFilters(): void {
  Object.assign(filters, defaultFilters())
  void loadCertificates(1)
}

function onPageChange(page: number): void {
  void loadCertificates(page)
}

function onPageSizeChange(size: number): void {
  filters.pageSize = size
  void loadCertificates(1)
}

async function openDetail(row: CertificateListItem): Promise<void> {
  detailVisible.value = true
  detail.value = null
  detailLoading.value = true
  detailController?.abort()
  const controller = new AbortController()
  detailController = controller
  const currentEpoch = epoch
  try {
    const result = await getCertificate(actorToken.value, row.id, controller.signal)
    if (currentEpoch !== epoch) return
    detail.value = result
  } catch (err) {
    if (currentEpoch !== epoch || isAbort(err)) return
    error.value = errorText(err)
    detailVisible.value = false
  } finally {
    if (detailController === controller) detailLoading.value = false
  }
}

async function reloadDetail(): Promise<void> {
  if (!detail.value) return
  try {
    detail.value = await getCertificate(actorToken.value, detail.value.id)
  } catch (err) {
    error.value = errorText(err)
  }
}

async function revoke(): Promise<void> {
  if (!detail.value) return
  const target = detail.value
  try {
    await ElMessageBox.confirm(
      `确认吊销证书 ${target.serialNumber}？吊销是终态，不能恢复或重新发证。`,
      '吊销确认',
      { type: 'warning', confirmButtonText: '确认吊销', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  revoking.value = true
  try {
    const result = await revokeCertificate(actorToken.value, target.id)
    ElMessage.success(`已吊销：${result.revokedAt}`)
    await Promise.all([reloadDetail(), loadCertificates()])
  } catch (err) {
    error.value = errorText(err)
  } finally {
    revoking.value = false
  }
}

function resetIssueState(): void {
  issueCourseId.value = undefined
  enrollments.value = []
  enrollmentTotal.value = 0
  bulkResult.value = null
  issueError.value = ''
  issueKey.value = null
}

function openIssue(): void {
  resetIssueState()
  issueVisible.value = true
}

function closeIssue(): void {
  issueVisible.value = false
}

function onIssueClosed(): void {
  enrollmentController?.abort()
  resetIssueState()
}

function onIssueCourseChange(): void {
  bulkResult.value = null
  issueError.value = ''
  issueKey.value = null
  void loadEnrollments()
}

async function loadEnrollments(): Promise<void> {
  if (issueCourseId.value === undefined) return
  enrollmentController?.abort()
  const controller = new AbortController()
  enrollmentController = controller
  const currentEpoch = epoch
  const courseId = issueCourseId.value
  enrollmentsLoading.value = true
  try {
    const result = await listCourseEnrollments(actorToken.value, courseId, controller.signal)
    if (currentEpoch !== epoch) return
    enrollments.value = result.items
    enrollmentTotal.value = result.total
  } catch (err) {
    if (currentEpoch !== epoch || isAbort(err)) return
    issueError.value = errorText(err)
  } finally {
    if (enrollmentController === controller) enrollmentsLoading.value = false
  }
}

function makeIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `ui-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`
}

async function submitIssue(): Promise<void> {
  if (issueCourseId.value === undefined || issuing.value || bulkResult.value) return
  const courseId = issueCourseId.value
  if (issueKey.value === null) issueKey.value = makeIdempotencyKey()
  issuing.value = true
  issueError.value = ''
  try {
    const result = await bulkIssue(actorToken.value, courseId, issueKey.value)
    bulkResult.value = result
    ElMessage.success(`操作已提交：发证 ${result.issued}，跳过 ${result.skipped}，失败 ${result.failed}`)
    await Promise.all([loadCertificates(1), loadEnrollments()])
  } catch (err) {
    issueError.value = errorText(err)
  } finally {
    issuing.value = false
  }
}

function retryIssue(): void {
  void submitIssue()
}

function eligibilityText(enrollment: {
  issuanceEligibility?: { eligible: boolean; reasonCode: Enrollment['issuanceEligibility']['reasonCode'] }
}): string {
  const eligibility = enrollment.issuanceEligibility
  if (!eligibility) return '不可发证'
  if (eligibility.eligible) return '可发证'
  switch (eligibility.reasonCode) {
    case 'COURSE_INCOMPLETE':
      return '课程未完成'
    case 'ALREADY_CERTIFIED':
      return '已有有效证书'
    case 'CERTIFICATE_REVOKED':
      return '已有吊销证书'
    default:
      return '不可发证'
  }
}

function resultStatusText(status: string): string {
  if (status === 'issued') return '已发证'
  if (status === 'skipped') return '已跳过'
  return '失败'
}

function resultReasonText(reason: string | null): string {
  switch (reason) {
    case 'COURSE_INCOMPLETE':
      return '课程未完成'
    case 'ALREADY_CERTIFIED':
      return '已有有效证书'
    case 'CERTIFICATE_REVOKED':
      return '已有吊销证书'
    case 'ISSUANCE_ERROR':
      return '发证错误'
    default:
      return '—'
  }
}

async function downloadCsv(): Promise<void> {
  exporting.value = true
  try {
    const blob = await exportCertificates(actorToken.value, filters)
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'certificates.csv'
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    URL.revokeObjectURL(url)
    ElMessage.success('CSV 已开始下载')
  } catch (err) {
    error.value = errorText(err)
  } finally {
    exporting.value = false
  }
}

onMounted(() => {
  void bootstrap()
})
</script>

<template>
  <el-container class="app-shell">
    <el-header class="topbar">
      <div class="brand">
        <div class="brand-mark">C</div>
        <div>
          <div class="brand-title">证书管理后台</div>
          <div class="brand-subtitle">Certificate Admin Console</div>
        </div>
      </div>
      <div class="actor-switch">
        <span class="actor-label">当前身份</span>
        <el-select v-model="actorToken" size="default" class="actor-select" @change="switchActor">
          <el-option v-for="actor in actors" :key="actor.token" :label="actor.label" :value="actor.token" />
        </el-select>
        <el-tag v-if="me" effect="dark" type="info" class="tenant-tag">
          {{ me.tenant.name }} · {{ me.tenant.timezone }}
        </el-tag>
        <span v-else class="actor-label">{{ currentActorLabel }}</span>
      </div>
    </el-header>

    <el-main class="page">
      <el-alert
        v-if="error"
        :title="error"
        type="error"
        show-icon
        closable
        class="page-alert"
        @close="error = ''"
      />

      <el-card shadow="never" class="panel">
        <div class="toolbar">
          <el-input
            v-model="filters.q"
            placeholder="搜索学员姓名或序列号"
            clearable
            class="search-input"
            @keyup.enter="applyFilters"
            @clear="applyFilters"
          />
          <el-select v-model="filters.status" placeholder="全部状态" clearable class="filter-select" @change="applyFilters">
            <el-option label="有效 ACTIVE" value="ACTIVE" />
            <el-option label="已吊销 REVOKED" value="REVOKED" />
          </el-select>
          <el-select v-model="filters.courseId" placeholder="全部课程" clearable class="filter-select" @change="applyFilters">
            <el-option v-for="course in courses" :key="course.id" :label="course.title" :value="course.id" />
          </el-select>
          <el-select v-model="filters.sortBy" class="filter-select" @change="applyFilters">
            <el-option label="按本地发证日期" value="issuedLocalDate" />
            <el-option label="按学员姓名" value="learnerName" />
            <el-option label="按序列号" value="serialNumber" />
          </el-select>
          <el-select v-model="filters.sortOrder" class="order-select" @change="applyFilters">
            <el-option label="降序" value="desc" />
            <el-option label="升序" value="asc" />
          </el-select>
          <el-button type="primary" @click="applyFilters">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
          <div class="toolbar-spacer" />
          <el-button :loading="exporting" @click="downloadCsv">导出 CSV</el-button>
          <el-button type="success" @click="openIssue">批量发证</el-button>
        </div>

        <el-table
          v-loading="loading"
          :data="certificates.items"
          row-key="id"
          class="cert-table"
          empty-text="没有匹配的证书"
          @row-click="openDetail"
        >
          <el-table-column prop="serialNumber" label="序列号" min-width="280" show-overflow-tooltip />
          <el-table-column label="学员" min-width="130">
            <template #default="{ row }">{{ row.learner.name }}</template>
          </el-table-column>
          <el-table-column label="课程" min-width="140">
            <template #default="{ row }">{{ row.course.title }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'danger'" effect="light">
                {{ row.status === 'ACTIVE' ? '有效' : '已吊销' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="issuedAt" label="发证时间（UTC）" min-width="210" />
          <el-table-column prop="issuedLocalDate" label="本地发证日期" width="140" />
        </el-table>

        <div class="pagination-row">
          <el-pagination
            :current-page="certificates.page"
            :page-size="certificates.pageSize"
            :page-sizes="[10, 20, 50, 100]"
            :total="certificates.total"
            layout="total, sizes, prev, pager, next"
            background
            @current-change="onPageChange"
            @size-change="onPageSizeChange"
          />
        </div>
      </el-card>
    </el-main>

    <el-drawer v-model="detailVisible" title="证书详情" size="520px" @closed="detail = null">
      <div v-loading="detailLoading" class="detail-body">
        <template v-if="detail">
          <div class="detail-heading">
            <div>
              <div class="detail-serial">{{ detail.serialNumber }}</div>
              <div class="detail-subtitle">{{ detail.learner.name }} · {{ detail.course.title }}</div>
            </div>
            <el-tag :type="detail.status === 'ACTIVE' ? 'success' : 'danger'" effect="dark">
              {{ detail.status === 'ACTIVE' ? '有效' : '已吊销' }}
            </el-tag>
          </div>

          <el-descriptions :column="1" border class="detail-descriptions">
            <el-descriptions-item label="证书 ID">{{ detail.id }}</el-descriptions-item>
            <el-descriptions-item label="UTC 发证时间">{{ detail.issuedAt }}</el-descriptions-item>
            <el-descriptions-item label="租户本地时间">{{ detail.issuedLocalAt }}</el-descriptions-item>
            <el-descriptions-item label="本地发证日期">{{ detail.issuedLocalDate }}</el-descriptions-item>
            <el-descriptions-item label="租户时区">{{ detail.timezone }}</el-descriptions-item>
            <el-descriptions-item label="发证人">{{ detail.issuedBy.email }}</el-descriptions-item>
            <el-descriptions-item label="吊销时间">{{ detail.revokedAt ?? '—' }}</el-descriptions-item>
            <el-descriptions-item label="吊销人">{{ detail.revokedBy?.email ?? '—' }}</el-descriptions-item>
          </el-descriptions>

          <div v-if="detail.status === 'ACTIVE'" class="danger-zone">
            <div class="danger-title">吊销证书</div>
            <div class="danger-hint">吊销是终态，不会删除证书，也不能恢复或重新发证。</div>
            <el-button type="danger" :loading="revoking" @click="revoke">吊销证书</el-button>
          </div>
          <el-alert v-else type="warning" :closable="false" title="该证书已吊销，状态不可恢复。" show-icon />
        </template>
      </div>
    </el-drawer>

    <el-dialog v-model="issueVisible" title="批量发证" width="760px" @closed="onIssueClosed">
      <div class="issue-form">
        <span class="issue-label">目标课程</span>
        <el-select
          v-model="issueCourseId"
          placeholder="选择课程"
          class="issue-course"
          :disabled="!!bulkResult"
          @change="onIssueCourseChange"
        >
          <el-option v-for="course in courses" :key="course.id" :label="`${course.title}（#${course.id}）`" :value="course.id" />
        </el-select>
        <span v-if="issueCourseTitle" class="issue-hint">课程：{{ issueCourseTitle }}</span>
      </div>

      <el-alert
        v-if="enrollmentTotal > enrollments.length"
        type="info"
        :closable="false"
        show-icon
        :title="`报名共 ${enrollmentTotal} 条，此处仅展示前 ${enrollments.length} 条；提交时会处理课程全部报名。`"
        class="issue-alert"
      />

      <el-table
        v-if="issueCourseId !== undefined"
        v-loading="enrollmentsLoading"
        :data="enrollments"
        size="small"
        max-height="260"
        empty-text="该课程没有报名"
        class="enrollment-table"
      >
        <el-table-column label="学员" min-width="120">
          <template #default="{ row }">{{ row.learner.name }}</template>
        </el-table-column>
        <el-table-column label="完成状态" width="110">
          <template #default="{ row }">
            <el-tag :type="row.completionStatus === 'COMPLETED' ? 'success' : 'info'" size="small" effect="plain">
              {{ row.completionStatus === 'COMPLETED' ? '已完成' : '未完成' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="现有证书" min-width="220">
          <template #default="{ row }">
            <span v-if="row.certificate">{{ row.certificate.serialNumber }}</span>
            <span v-else class="muted">无</span>
          </template>
        </el-table-column>
        <el-table-column label="发证资格" width="130">
          <template #default="{ row }">
            <el-tag :type="row.issuanceEligibility.eligible ? 'success' : 'warning'" size="small">
              {{ eligibilityText(row) }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>

      <el-alert
        v-if="issueError"
        type="error"
        :closable="false"
        show-icon
        :title="issueError"
        class="issue-alert"
      />

      <div v-if="bulkResult" class="result-block">
        <div class="result-head">
          <div>
            <div class="result-title">操作结果</div>
            <div class="result-subtitle">operationId: {{ bulkResult.operationId }}</div>
          </div>
          <el-tag :type="bulkTone" effect="dark">{{ bulkResult.status }}</el-tag>
        </div>
        <div class="result-counts">
          <span>总计 <strong>{{ bulkResult.total }}</strong></span>
          <span class="issued">发证 <strong>{{ bulkResult.issued }}</strong></span>
          <span class="skipped">跳过 <strong>{{ bulkResult.skipped }}</strong></span>
          <span class="failed">失败 <strong>{{ bulkResult.failed }}</strong></span>
        </div>
        <el-table :data="bulkResult.results" size="small" max-height="240">
          <el-table-column prop="learnerName" label="学员" min-width="120" />
          <el-table-column label="结果" width="100">
            <template #default="{ row }">
              <el-tag
                size="small"
                :type="row.status === 'issued' ? 'success' : row.status === 'skipped' ? 'warning' : 'danger'"
              >
                {{ resultStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="原因" min-width="130">
            <template #default="{ row }">{{ resultReasonText(row.reasonCode) }}</template>
          </el-table-column>
          <el-table-column label="序列号" min-width="240">
            <template #default="{ row }">{{ row.serialNumber ?? '—' }}</template>
          </el-table-column>
        </el-table>
      </div>

      <template #footer>
        <el-button @click="closeIssue">关闭</el-button>
        <el-button v-if="issueError && issueKey && !bulkResult" type="warning" :loading="issuing" @click="retryIssue">
          使用同一幂等键重试
        </el-button>
        <el-button
          type="primary"
          :loading="issuing"
          :disabled="issueCourseId === undefined || !!bulkResult"
          @click="submitIssue"
        >
          发起批量发证
        </el-button>
      </template>
    </el-dialog>
  </el-container>
</template>
