import type {
  BulkOperationResult,
  CertificateDetail,
  CertificateFilters,
  CertificateListItem,
  Course,
  Enrollment,
  Me,
  Page,
  RevocationResult
} from './types'

export class ApiError extends Error {
  status: number
  code: string
  requestId: string
  details: Array<{ field: string; reason: string }>

  constructor(status: number, code: string, message: string, requestId: string, details: Array<{ field: string; reason: string }>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.details = details
  }
}

interface RequestOptions {
  method?: string
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
}

async function request<T>(token: string, path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${token}`,
    ...options.headers
  }
  let body: string | undefined
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(options.body)
  }
  const response = await fetch(path, {
    method: options.method ?? 'GET',
    headers,
    body,
    signal: options.signal
  })
  if (!response.ok) {
    let code = 'INTERNAL_ERROR'
    let message = `请求失败（HTTP ${response.status}）`
    let requestId = response.headers.get('X-Request-Id') ?? ''
    let details: Array<{ field: string; reason: string }> = []
    try {
      const payload = await response.json()
      if (payload?.error) {
        code = payload.error.code ?? code
        message = payload.error.message ?? message
        requestId = payload.error.requestId ?? requestId
        details = payload.error.details ?? []
      }
    } catch {
      // Keep the HTTP fallback when the body is not JSON.
    }
    throw new ApiError(response.status, code, message, requestId, details)
  }
  return (await response.json()) as T
}

function positiveInteger(value: unknown, fallback: number, max: number): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 && value <= max ? value : fallback
}

// Build the shared list/CSV query. Optional filters are emitted only when they
// hold a real value: Element Plus sets a cleared select to '' in some versions,
// and undefined/null must never become "courseId=undefined" or "status=".
function filterQuery(filters: CertificateFilters, includePaging: boolean): string {
  const params = new URLSearchParams()
  const query = typeof filters.q === 'string' ? filters.q.trim() : ''
  if (query !== '') params.set('q', query)

  if (filters.status === 'ACTIVE' || filters.status === 'REVOKED') {
    params.set('status', filters.status)
  }

  const courseId = filters.courseId
  if (typeof courseId === 'number' && Number.isInteger(courseId) && courseId > 0) {
    params.set('courseId', String(courseId))
  }

  const sortBy =
    filters.sortBy === 'learnerName' || filters.sortBy === 'serialNumber' || filters.sortBy === 'issuedLocalDate'
      ? filters.sortBy
      : 'issuedLocalDate'
  params.set('sortBy', sortBy)
  const sortOrder =
    filters.sortOrder === 'asc' || filters.sortOrder === 'desc'
      ? filters.sortOrder
      : sortBy === 'issuedLocalDate'
        ? 'desc'
        : 'asc'
  params.set('sortOrder', sortOrder)

  if (includePaging) {
    params.set('page', String(positiveInteger(filters.page, 1, 1_000_000_000)))
    params.set('pageSize', String(positiveInteger(filters.pageSize, 20, 100)))
  }
  return params.toString()
}

export function getMe(token: string, signal?: AbortSignal): Promise<Me> {
  return request<Me>(token, '/api/v1/me', { signal })
}

export function listCourses(token: string, signal?: AbortSignal): Promise<Page<Course>> {
  return request<Page<Course>>(token, '/api/v1/courses?page=1&pageSize=100', { signal })
}

export function listCertificates(token: string, filters: CertificateFilters, signal?: AbortSignal): Promise<Page<CertificateListItem>> {
  return request<Page<CertificateListItem>>(token, `/api/v1/certificates?${filterQuery(filters, true)}`, { signal })
}

export function getCertificate(token: string, id: number, signal?: AbortSignal): Promise<CertificateDetail> {
  return request<CertificateDetail>(token, `/api/v1/certificates/${id}`, { signal })
}

export function listCourseEnrollments(token: string, courseId: number, signal?: AbortSignal): Promise<Page<Enrollment>> {
  return request<Page<Enrollment>>(token, `/api/v1/courses/${courseId}/enrollments?page=1&pageSize=100`, { signal })
}

export function bulkIssue(token: string, courseId: number, idempotencyKey: string, signal?: AbortSignal): Promise<BulkOperationResult> {
  return request<BulkOperationResult>(token, `/api/v1/courses/${courseId}/bulk-operations`, {
    method: 'POST',
    body: {},
    headers: { 'Idempotency-Key': idempotencyKey },
    signal
  })
}

export function revokeCertificate(token: string, id: number, signal?: AbortSignal): Promise<RevocationResult> {
  return request<RevocationResult>(token, `/api/v1/certificates/${id}/revocation`, {
    method: 'PUT',
    body: {},
    signal
  })
}

export async function exportCertificates(token: string, filters: CertificateFilters, signal?: AbortSignal): Promise<Blob> {
  const response = await fetch(`/api/v1/certificates.csv?${filterQuery(filters, false)}`, {
    headers: { Authorization: `Bearer ${token}` },
    signal
  })
  if (!response.ok) {
    let message = `导出失败（HTTP ${response.status}）`
    try {
      const payload = await response.json()
      message = payload?.error?.message ?? message
    } catch {
      // Keep the HTTP fallback.
    }
    throw new Error(message)
  }
  return response.blob()
}
