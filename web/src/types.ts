export interface AdminRef {
  id: number
  email: string
}

export interface TenantRef {
  id: number
  code: string
  name: string
  timezone: string
}

export interface Me {
  admin: AdminRef
  tenant: TenantRef
}

export interface Course {
  id: number
  title: string
}

export interface Learner {
  id: number
  name: string
}

export interface CertificateSummary {
  id: number
  serialNumber: string
  status: 'ACTIVE' | 'REVOKED'
  issuedAt: string
  issuedLocalDate: string
}

export interface CertificateListItem {
  id: number
  serialNumber: string
  learner: Learner
  course: Course
  status: 'ACTIVE' | 'REVOKED'
  issuedAt: string
  issuedLocalDate: string
}

export interface CertificateDetail extends CertificateListItem {
  issuedLocalAt: string
  timezone: string
  issuedBy: AdminRef
  revokedAt: string | null
  revokedBy: AdminRef | null
}

export type EligibilityReason =
  | 'COURSE_INCOMPLETE'
  | 'ALREADY_CERTIFIED'
  | 'CERTIFICATE_REVOKED'

export interface Enrollment {
  id: number
  learner: Learner
  course: Course
  completionStatus: 'INCOMPLETE' | 'COMPLETED'
  completedAt: string | null
  certificate: CertificateSummary | null
  issuanceEligibility: {
    eligible: boolean
    reasonCode: EligibilityReason | null
  }
}

export interface BulkLearnerResult {
  learnerId: number
  learnerName: string
  status: 'issued' | 'skipped' | 'failed'
  reasonCode: string | null
  certificateId: number | null
  serialNumber: string | null
}

export interface BulkOperationResult {
  operationId: string
  courseId: number
  status: 'COMPLETED'
  total: number
  issued: number
  skipped: number
  failed: number
  results: BulkLearnerResult[]
}

export interface RevocationResult {
  certificateId: number
  revokedAt: string
  revokedBy: AdminRef
}

export interface Page<T> {
  items: T[]
  page: number
  pageSize: number
  total: number
}

export interface CertificateFilters {
  q: string
  status: '' | 'ACTIVE' | 'REVOKED'
  courseId: number | undefined
  sortBy: 'learnerName' | 'serialNumber' | 'issuedLocalDate'
  sortOrder: 'asc' | 'desc'
  page: number
  pageSize: number
}
