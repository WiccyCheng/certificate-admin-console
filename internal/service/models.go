// Package service implements authentication, certificate queries, issuance,
// revocation and CSV streaming on top of the SQLite store.
package service

import (
	"time"

	_ "time/tzdata"
)

// Actor is the authenticated administrator with the tenant resolved from the
// token. Clients can never choose tenantId themselves.
type Actor struct {
	AdminID    int64
	Email      string
	TenantID   int64
	TenantCode string
	TenantName string
	Timezone   string
	Location   *time.Location
}

type AdminRef struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type TenantRef struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

type LearnerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type CourseRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type MeResponse struct {
	Admin  AdminRef  `json:"admin"`
	Tenant TenantRef `json:"tenant"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Total    int64 `json:"total"`
}

type CertificateSummary struct {
	ID              int64  `json:"id"`
	SerialNumber    string `json:"serialNumber"`
	Status          string `json:"status"`
	IssuedAt        string `json:"issuedAt"`
	IssuedLocalDate string `json:"issuedLocalDate"`
}

type CertificateListItem struct {
	ID              int64      `json:"id"`
	SerialNumber    string     `json:"serialNumber"`
	Learner         LearnerRef `json:"learner"`
	Course          CourseRef  `json:"course"`
	Status          string     `json:"status"`
	IssuedAt        string     `json:"issuedAt"`
	IssuedLocalDate string     `json:"issuedLocalDate"`
}

type CertificateDetail struct {
	ID              int64      `json:"id"`
	SerialNumber    string     `json:"serialNumber"`
	Learner         LearnerRef `json:"learner"`
	Course          CourseRef  `json:"course"`
	Status          string     `json:"status"`
	IssuedAt        string     `json:"issuedAt"`
	IssuedLocalDate string     `json:"issuedLocalDate"`
	IssuedLocalAt   string     `json:"issuedLocalAt"`
	Timezone        string     `json:"timezone"`
	IssuedBy        AdminRef   `json:"issuedBy"`
	RevokedAt       *string    `json:"revokedAt"`
	RevokedBy       *AdminRef  `json:"revokedBy"`
}

type Eligibility struct {
	Eligible   bool    `json:"eligible"`
	ReasonCode *string `json:"reasonCode"`
}

type EnrollmentItem struct {
	ID                  int64               `json:"id"`
	Learner             LearnerRef          `json:"learner"`
	Course              CourseRef           `json:"course"`
	CompletionStatus    string              `json:"completionStatus"`
	CompletedAt         *string             `json:"completedAt"`
	Certificate         *CertificateSummary `json:"certificate"`
	IssuanceEligibility Eligibility         `json:"issuanceEligibility"`
}

type BulkLearnerResult struct {
	LearnerID     int64   `json:"learnerId"`
	LearnerName   string  `json:"learnerName"`
	Status        string  `json:"status"`
	ReasonCode    *string `json:"reasonCode"`
	CertificateID *int64  `json:"certificateId"`
	SerialNumber  *string `json:"serialNumber"`
}

type BulkOperationResult struct {
	OperationID string              `json:"operationId"`
	CourseID    int64               `json:"courseId"`
	Status      string              `json:"status"`
	Total       int                 `json:"total"`
	Issued      int                 `json:"issued"`
	Skipped     int                 `json:"skipped"`
	Failed      int                 `json:"failed"`
	Results     []BulkLearnerResult `json:"results"`
}

type RevocationResult struct {
	CertificateID int64    `json:"certificateId"`
	RevokedAt     string   `json:"revokedAt"`
	RevokedBy     AdminRef `json:"revokedBy"`
}

// CertificateFilter is shared by the list and CSV endpoints so both use the
// same tenant, search, filter and ordering semantics.
type CertificateFilter struct {
	Q         string
	Status    string
	CourseID  int64
	LearnerID int64
	SortBy    string
	SortOrder string
	Page      int
	PageSize  int
}

type CertificateCSVRow struct {
	LearnerName     string
	CourseTitle     string
	SerialNumber    string
	IssuedLocalDate string
}
