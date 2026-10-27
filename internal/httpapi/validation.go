package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

var (
	pathIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	idemPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

func parseQuery(c *gin.Context, allowed ...string) (url.Values, *apiError) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, badRequest("查询参数格式错误", "", "")
	}
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	for key, list := range values {
		if _, ok := set[key]; !ok {
			return nil, badRequest("未知查询参数", key, "UNKNOWN_PARAMETER")
		}
		if len(list) > 1 {
			return nil, badRequest("查询参数不能重复", key, "DUPLICATE_PARAMETER")
		}
	}
	return values, nil
}

func parsePathID(c *gin.Context, name string) (int64, *apiError) {
	raw := c.Param(name)
	if !pathIDPattern.MatchString(raw) {
		return 0, badRequest("路径 ID 必须是正整数", name, "INVALID_ID")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, badRequest("路径 ID 超出范围", name, "INVALID_ID")
	}
	return value, nil
}

func stringParam(values url.Values, name string) (string, bool) {
	list, ok := values[name]
	if !ok {
		return "", false
	}
	return list[0], true
}

func positiveIntParam(values url.Values, name string) (int64, bool, *apiError) {
	raw, ok := stringParam(values, name)
	if !ok {
		return 0, false, nil
	}
	if !pathIDPattern.MatchString(raw) {
		return 0, true, badRequest("必须是正整数", name, "INVALID_INTEGER")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, true, badRequest("整数超出范围", name, "INVALID_INTEGER")
	}
	return value, true, nil
}

func pageParams(values url.Values) (int, int, *apiError) {
	page, pageSize := 1, 20
	if raw, ok := stringParam(values, "page"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1_000_000_000 {
			return 0, 0, badRequest("page 必须是正整数", "page", "INVALID_PAGE")
		}
		page = value
	}
	if raw, ok := stringParam(values, "pageSize"); ok {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, badRequest("pageSize 必须在 1–100 之间", "pageSize", "INVALID_PAGE_SIZE")
		}
		pageSize = value
	}
	return page, pageSize, nil
}

func parseCompletion(values url.Values) (string, *apiError) {
	value, ok := stringParam(values, "completionStatus")
	if !ok {
		return "", nil
	}
	if value != "INCOMPLETE" && value != "COMPLETED" {
		return "", badRequest("completionStatus 只能是 INCOMPLETE 或 COMPLETED", "completionStatus", "INVALID_ENUM")
	}
	return value, nil
}

func parseCertificateFilter(c *gin.Context, allowPaging bool) (service.CertificateFilter, *apiError) {
	var f service.CertificateFilter
	allowed := []string{"q", "status", "courseId", "learnerId", "sortBy", "sortOrder"}
	if allowPaging {
		allowed = append(allowed, "page", "pageSize")
	}
	values, aerr := parseQuery(c, allowed...)
	if aerr != nil {
		return f, aerr
	}

	f.Q = strings.TrimSpace(c.Query("q"))
	if status, ok := stringParam(values, "status"); ok {
		if status != "ACTIVE" && status != "REVOKED" {
			return f, badRequest("status 只能是 ACTIVE 或 REVOKED", "status", "INVALID_ENUM")
		}
		f.Status = status
	}
	courseID, _, aerr := positiveIntParam(values, "courseId")
	if aerr != nil {
		return f, aerr
	}
	f.CourseID = courseID
	learnerID, _, aerr := positiveIntParam(values, "learnerId")
	if aerr != nil {
		return f, aerr
	}
	f.LearnerID = learnerID

	f.SortBy = "issuedLocalDate"
	if sortBy, ok := stringParam(values, "sortBy"); ok {
		switch sortBy {
		case "learnerName", "serialNumber", "issuedLocalDate":
			f.SortBy = sortBy
		default:
			return f, badRequest("不支持的排序字段", "sortBy", "INVALID_SORT")
		}
	}
	defaultOrder := "desc"
	if f.SortBy != "issuedLocalDate" {
		defaultOrder = "asc"
	}
	f.SortOrder = defaultOrder
	if sortOrder, ok := stringParam(values, "sortOrder"); ok {
		if sortOrder != "asc" && sortOrder != "desc" {
			return f, badRequest("sortOrder 只能是 asc 或 desc", "sortOrder", "INVALID_SORT")
		}
		f.SortOrder = sortOrder
	}

	f.Page, f.PageSize, aerr = pageParams(values)
	if aerr != nil {
		return f, aerr
	}
	return f, nil
}

func idempotencyKey(c *gin.Context) (string, *apiError) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 || !idemPattern.MatchString(values[0]) {
		return "", badRequest("Idempotency-Key 必须是 1–128 位 [A-Za-z0-9_-] 且只能出现一次", "Idempotency-Key", "INVALID_IDEMPOTENCY_KEY")
	}
	return values[0], nil
}

// validateEmptyJSONBody enforces the contract's optional, member-free body.
// Empty bodies are allowed; non-empty bodies must be an application/json object
// with no members at all.
func validateEmptyJSONBody(c *gin.Context) *apiError {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		return badRequest("读取请求体失败", "", "")
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}

	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		return unsupportedMedia()
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return unsupportedMedia()
	}
	if trimmed[0] != '{' {
		return badRequest("请求体必须是 JSON 对象", "body", "INVALID_BODY")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return badRequest("请求体不是合法 JSON", "body", "INVALID_JSON")
	}
	if len(object) > 0 {
		field := ""
		for key := range object {
			field = key
			break
		}
		return badRequest("请求体不接受任何字段", field, "UNKNOWN_FIELD")
	}
	return nil
}
