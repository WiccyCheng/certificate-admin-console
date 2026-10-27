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

function certificateQuery(filters: CertificateFilters): string {
  const params = new URLSearchParams()
  if (filters.q.trim() !== '') params.set('q', filters.q.trim())
  if (filters.status !== '') params.set('status', filters.status)
  if (filters.courseId !== undefined) params.set('courseId', String(filters.courseId))
  params.set('sortBy', filters.sortBy)
  params.set('sortOrder', filters.sortOrder)
  params.set('page', String(filters.page))
  params.set('pageSize', String(filters.pageSize))
  return params.toString()
}

export function getMe(token: string, signal?: AbortSignal): Promise<Me> {
  return request<Me>(token, '/api/v1/me', { signal })
}

export function listCourses(token: string, signal?: AbortSignal): Promise<Page<Course>> {
  return request<Page<Course>>(token, '/api/v1/courses?page=1&pageSize=100', { signal })
}

export function listCertificates(token: string, filters: CertificateFilters, signal?: AbortSignal): Promise<Page<CertificateListItem>> {
  return request<Page<CertificateListItem>>(token, `/api/v1/certificates?${certificateQuery(filters)}`, { signal })
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
  const params = new URLSearchParams()
  if (filters.q.trim() !== '') params.set('q', filters.q.trim())
  if (filters.status !== '') params.set('status', filters.status)
  if (filters.courseId !== undefined) params.set('courseId', String(filters.courseId))
  params.set('sortBy', filters.sortBy)
  params.set('sortOrder', filters.sortOrder)
  const response = await fetch(`/api/v1/certificates.csv?${params.toString()}`, {
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
