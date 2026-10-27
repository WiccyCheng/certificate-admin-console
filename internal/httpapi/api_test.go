package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"certadmin/internal/service"
	"certadmin/internal/store"

	"github.com/gin-gonic/gin"
)

type testEnv struct {
	t      *testing.T
	db     *store.DB
	srv    *Server
	server *httptest.Server
	client *http.Client
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.SeedIfEmpty(); err != nil {
		db.Close()
		t.Fatalf("seed: %v", err)
	}
	svc := service.New(db)
	srv := New(svc)
	server := httptest.NewServer(srv.Handler())
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	env := &testEnv{t: t, db: db, srv: srv, server: server, client: client}
	t.Cleanup(func() {
		server.Close()
		db.Close()
	})
	return env
}

func (e *testEnv) do(method, path, token, body string, headers map[string]string) (*http.Response, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("read %s %s: %v", method, path, err)
	}
	return resp, data
}

func (e *testEnv) json(method, path, token, body string, headers map[string]string, wantStatus int) map[string]any {
	e.t.Helper()
	resp, data := e.do(method, path, token, body, headers)
	if resp.StatusCode != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d, body=%s", method, path, resp.StatusCode, wantStatus, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		e.t.Fatalf("%s %s: invalid JSON %q: %v", method, path, data, err)
	}
	return out
}

func (e *testEnv) count(query string, args ...any) int64 {
	e.t.Helper()
	var value int64
	if err := e.db.Read.QueryRow(query, args...).Scan(&value); err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return value
}

func items(t *testing.T, page map[string]any) []any {
	t.Helper()
	value, ok := page["items"].([]any)
	if !ok {
		t.Fatalf("items is not an array: %#v", page["items"])
	}
	return value
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %#v", value)
	}
	return out
}

func TestTenantIsolationA1(t *testing.T) {
	e := newTestEnv(t)

	resp, body := e.do("GET", "/api/v1/certificates", "", "", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("missing token status = %d, body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("WWW-Authenticate") != `Bearer realm="certificate-admin"` {
		t.Fatalf("WWW-Authenticate = %q", resp.Header.Get("WWW-Authenticate"))
	}

	me := e.json("GET", "/api/v1/me", "demo-alice-token", "", nil, 200)
	tenant := asMap(t, me["tenant"])
	if tenant["code"] != "EAST" || tenant["timezone"] != "Australia/Sydney" {
		t.Fatalf("unexpected actor tenant: %#v", tenant)
	}

	alice := e.json("GET", "/api/v1/certificates", "demo-alice-token", "", nil, 200)
	if alice["total"].(float64) != 2 {
		t.Fatalf("Alice total = %v", alice["total"])
	}
	for _, raw := range items(t, alice) {
		id := asMap(t, raw)["id"].(float64)
		if id != 3 && id != 4 {
			t.Fatalf("Alice saw non-East certificate %v", id)
		}
	}
	bob := e.json("GET", "/api/v1/certificates", "demo-bob-token", "", nil, 200)
	if bob["total"].(float64) != 2 {
		t.Fatalf("Bob total = %v", bob["total"])
	}
	for _, raw := range items(t, bob) {
		id := asMap(t, raw)["id"].(float64)
		if id != 1 && id != 2 {
			t.Fatalf("Bob saw non-West certificate %v", id)
		}
	}

	e.json("GET", "/api/v1/certificates/1", "demo-alice-token", "", nil, 404)
	e.json("GET", "/api/v1/certificates/3", "demo-bob-token", "", nil, 404)
	e.json("GET", "/api/v1/courses/3/enrollments", "demo-alice-token", "", nil, 404)
	e.json("GET", "/api/v1/learners/5/enrollments", "demo-alice-token", "", nil, 404)
	e.json("POST", "/api/v1/courses/3/bulk-operations", "demo-alice-token", "{}", map[string]string{
		"Idempotency-Key": "cross-tenant",
		"Content-Type":    "application/json",
	}, 404)
	if got := e.count(`SELECT COUNT(*) FROM bulk_operations`); got != 0 {
		t.Fatalf("cross-tenant bulk created %d operations", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM certificates WHERE tenant_id = 1`); got != 2 {
		t.Fatalf("cross-tenant bulk changed East certificates: %d", got)
	}

	filtered := e.json("GET", "/api/v1/certificates?courseId=3", "demo-alice-token", "", nil, 200)
	if filtered["total"].(float64) != 0 || len(items(t, filtered)) != 0 {
		t.Fatalf("cross-tenant filter leaked data: %#v", filtered)
	}
	e.json("GET", "/api/v1/certificates?tenantId=1", "demo-alice-token", "", nil, 400)

	withHeader := e.json("GET", "/api/v1/certificates", "demo-alice-token", "", map[string]string{"X-Tenant-Id": "2"}, 200)
	if withHeader["total"].(float64) != 2 {
		t.Fatalf("X-Tenant-Id changed tenant scope: %#v", withHeader)
	}

	csvResp, csvBody := e.do("GET", "/api/v1/certificates.csv?courseId=3", "demo-alice-token", "", nil)
	if csvResp.StatusCode != 200 {
		t.Fatalf("cross-tenant CSV status = %d", csvResp.StatusCode)
	}
	if string(csvBody) != "learnerName,courseTitle,serialNumber,issuedLocalDate\r\n" {
		t.Fatalf("cross-tenant CSV leaked rows: %q", csvBody)
	}
}

func TestBulkIdempotencyA2A3A4(t *testing.T) {
	e := newTestEnv(t)
	headers := map[string]string{"Idempotency-Key": "bulk-key-1", "Content-Type": "application/json"}

	resp, firstBody := e.do("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "{}", headers)
	if resp.StatusCode != 201 {
		t.Fatalf("first bulk status = %d, body=%s", resp.StatusCode, firstBody)
	}
	var first service.BulkOperationResult
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode first result: %v", err)
	}
	if first.Total != 4 || first.Issued != 1 || first.Skipped != 3 || first.Failed != 0 {
		t.Fatalf("unexpected counts: %+v", first)
	}
	if first.Status != "COMPLETED" || first.CourseID != 1 {
		t.Fatalf("unexpected operation: %+v", first)
	}
	if len(first.Results) != 4 {
		t.Fatalf("results length = %d", len(first.Results))
	}
	wantReasons := []string{"", "COURSE_INCOMPLETE", "ALREADY_CERTIFIED", "CERTIFICATE_REVOKED"}
	wantStatuses := []string{"issued", "skipped", "skipped", "skipped"}
	for i, result := range first.Results {
		if result.LearnerID != int64(i+1) {
			t.Fatalf("result[%d] learner = %d", i, result.LearnerID)
		}
		if result.Status != wantStatuses[i] {
			t.Fatalf("result[%d] status = %s", i, result.Status)
		}
		gotReason := ""
		if result.ReasonCode != nil {
			gotReason = *result.ReasonCode
		}
		if gotReason != wantReasons[i] {
			t.Fatalf("result[%d] reason = %q, want %q", i, gotReason, wantReasons[i])
		}
	}
	if first.Results[0].CertificateID == nil || *first.Results[0].CertificateID != 5 {
		t.Fatalf("new certificate id = %v, want 5", first.Results[0].CertificateID)
	}
	if first.Results[0].SerialNumber == nil || !strings.HasPrefix(*first.Results[0].SerialNumber, "CERT-EAST-") {
		t.Fatalf("new serial = %v", first.Results[0].SerialNumber)
	}
	if len(*first.Results[0].SerialNumber) != len("CERT-EAST-")+26 {
		t.Fatalf("serial is not CERT-{TENANT}-{ULID}: %q", *first.Results[0].SerialNumber)
	}

	resp, replayBody := e.do("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "{}", headers)
	if resp.StatusCode != 200 {
		t.Fatalf("replay status = %d", resp.StatusCode)
	}
	if !bytes.Equal(firstBody, replayBody) {
		t.Fatalf("replay bytes differ:\nfirst=%s\nreplay=%s", firstBody, replayBody)
	}
	if got := e.count(`SELECT COUNT(*) FROM bulk_operations WHERE tenant_id = 1`); got != 1 {
		t.Fatalf("bulk operations = %d", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM certificates WHERE tenant_id = 1`); got != 3 {
		t.Fatalf("East certificates = %d, want 3", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM audit_events WHERE tenant_id = 1`); got != 4 {
		t.Fatalf("East audits = %d, want 4", got)
	}

	conflict := e.json("POST", "/api/v1/courses/2/bulk-operations", "demo-alice-token", "{}", headers, 409)
	errorObject := asMap(t, conflict["error"])
	if errorObject["code"] != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict code = %v", errorObject["code"])
	}
	if got := e.count(`SELECT COUNT(*) FROM certificates WHERE tenant_id = 1`); got != 3 {
		t.Fatalf("conflict changed certificates: %d", got)
	}

	invalidHeaders := map[string]string{"Idempotency-Key": "bulk-key-1", "Content-Type": "application/json"}
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `{"tenantId":1}`, invalidHeaders, 400)
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `null`, invalidHeaders, 400)
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `[]`, invalidHeaders, 400)
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `{}`,
		map[string]string{"Content-Type": "text/plain"}, 415)
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `{}`,
		map[string]string{"Idempotency-Key": "bad key", "Content-Type": "application/json"}, 400)
	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "", nil, 400)
	e.json("POST", "/api/v1/courses/1/bulk-operations?x=1", "demo-alice-token", `{}`, headers, 400)

	// A replay after later state changes must still return the historical result.
	e.json("PUT", "/api/v1/certificates/5/revocation", "demo-alice-token", "{}",
		map[string]string{"Content-Type": "application/json"}, 201)
	resp, afterRevoke := e.do("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "{}", headers)
	if resp.StatusCode != 200 || !bytes.Equal(firstBody, afterRevoke) {
		t.Fatalf("replay after revocation changed: status=%d body=%s", resp.StatusCode, afterRevoke)
	}
}

func TestBulkConcurrentSameKeyA2(t *testing.T) {
	e := newTestEnv(t)
	const workers = 8
	type result struct {
		status int
		body   []byte
		err    error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("POST", e.server.URL+"/api/v1/courses/1/bulk-operations", strings.NewReader("{}"))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Authorization", "Bearer demo-alice-token")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "concurrent-key")
			resp, err := e.client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			results <- result{status: resp.StatusCode, body: body, err: err}
		}()
	}
	wg.Wait()
	close(results)

	statuses := map[int]int{}
	var reference []byte
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent request: %v", result.err)
		}
		statuses[result.status]++
		if reference == nil {
			reference = result.body
		} else if !bytes.Equal(reference, result.body) {
			t.Fatalf("concurrent responses differ:\n%s\n%s", reference, result.body)
		}
	}
	if statuses[201] != 1 || statuses[200] != workers-1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	if got := e.count(`SELECT COUNT(*) FROM certificates WHERE tenant_id = 1`); got != 3 {
		t.Fatalf("concurrent issuance created %d East certificates", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM audit_events WHERE tenant_id = 1 AND event_type = 'ISSUED'`); got != 3 {
		t.Fatalf("ISSUED audits = %d", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM bulk_operations`); got != 1 {
		t.Fatalf("bulk operations = %d", got)
	}
}

func TestTimeAndLocalDateA5(t *testing.T) {
	e := newTestEnv(t)

	east := e.json("GET", "/api/v1/certificates/3", "demo-alice-token", "", nil, 200)
	if east["issuedLocalAt"] != "2026-01-11T00:30:00.000+11:00" {
		t.Fatalf("East issuedLocalAt = %v", east["issuedLocalAt"])
	}
	if east["issuedLocalDate"] != "2026-01-11" || east["timezone"] != "Australia/Sydney" {
		t.Fatalf("East local date/timezone = %v/%v", east["issuedLocalDate"], east["timezone"])
	}
	west := e.json("GET", "/api/v1/certificates/1", "demo-bob-token", "", nil, 200)
	if west["issuedLocalAt"] != "2026-01-10T21:30:00.000+08:00" {
		t.Fatalf("West issuedLocalAt = %v", west["issuedLocalAt"])
	}
	if west["issuedLocalDate"] != "2026-01-10" || west["timezone"] != "Australia/Perth" {
		t.Fatalf("West local date/timezone = %v/%v", west["issuedLocalDate"], west["timezone"])
	}

	asc := e.json("GET", "/api/v1/certificates?sortBy=issuedLocalDate&sortOrder=asc", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, asc); fmt.Sprint(ids) != "[4 3]" {
		t.Fatalf("local date asc ids = %v", ids)
	}
	desc := e.json("GET", "/api/v1/certificates?sortBy=issuedLocalDate&sortOrder=desc", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, desc); fmt.Sprint(ids) != "[3 4]" {
		t.Fatalf("local date desc ids = %v", ids)
	}
	byName := e.json("GET", "/api/v1/certificates?sortBy=learnerName&sortOrder=asc", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, byName); fmt.Sprint(ids) != "[4 3]" {
		t.Fatalf("name asc ids = %v", ids)
	}
	bySerial := e.json("GET", "/api/v1/certificates?sortBy=serialNumber&sortOrder=desc", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, bySerial); fmt.Sprint(ids) != "[3 4]" {
		t.Fatalf("serial desc ids = %v", ids)
	}
	defaultOrder := e.json("GET", "/api/v1/certificates?sortOrder=asc", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, defaultOrder); fmt.Sprint(ids) != "[4 3]" {
		t.Fatalf("sortOrder-only ids = %v", ids)
	}
}

func itemIDs(t *testing.T, page map[string]any) []int {
	t.Helper()
	out := make([]int, 0)
	for _, raw := range items(t, page) {
		out = append(out, int(asMap(t, raw)["id"].(float64)))
	}
	return out
}

func TestRevocationA7(t *testing.T) {
	e := newTestEnv(t)
	contentType := map[string]string{"Content-Type": "application/json"}

	resp, firstBody := e.do("PUT", "/api/v1/certificates/3/revocation", "demo-alice-token", "{}", contentType)
	if resp.StatusCode != 201 {
		t.Fatalf("first revocation status = %d body=%s", resp.StatusCode, firstBody)
	}
	var first service.RevocationResult
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode revocation: %v", err)
	}
	if first.CertificateID != 3 || first.RevokedBy.Email != "alice@east.test" {
		t.Fatalf("unexpected revocation: %+v", first)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", first.RevokedAt); err != nil {
		t.Fatalf("revokedAt format: %v", err)
	}

	resp, secondBody := e.do("PUT", "/api/v1/certificates/3/revocation", "demo-alice-token", "", nil)
	if resp.StatusCode != 200 || !bytes.Equal(firstBody, secondBody) {
		t.Fatalf("repeat revocation status=%d body=%s want=%s", resp.StatusCode, secondBody, firstBody)
	}
	if got := e.count(`SELECT COUNT(*) FROM audit_events WHERE certificate_id = 3 AND event_type = 'REVOKED'`); got != 1 {
		t.Fatalf("REVOKED audits = %d", got)
	}

	detail := e.json("GET", "/api/v1/certificates/3", "demo-alice-token", "", nil, 200)
	if detail["status"] != "REVOKED" || detail["issuedLocalAt"] != "2026-01-11T00:30:00.000+11:00" {
		t.Fatalf("detail after revocation = %#v", detail)
	}
	revokedBy := asMap(t, detail["revokedBy"])
	if revokedBy["email"] != "alice@east.test" {
		t.Fatalf("revokedBy = %#v", revokedBy)
	}

	e.json("PUT", "/api/v1/certificates/3/revocation", "demo-bob-token", "{}", contentType, 404)
	e.json("PUT", "/api/v1/certificates/4/revocation", "demo-alice-token", "{}", contentType, 200)
	if got := e.count(`SELECT COUNT(*) FROM audit_events WHERE certificate_id = 4 AND event_type = 'REVOKED'`); got != 1 {
		t.Fatalf("already-revoked certificate gained an audit: %d", got)
	}

	// Concurrent repeated revocations converge to one state change and one audit.
	fresh := newTestEnv(t)
	const workers = 6
	type outcome struct {
		status int
		body   []byte
		err    error
	}
	ch := make(chan outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("PUT", fresh.server.URL+"/api/v1/certificates/3/revocation", nil)
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			req.Header.Set("Authorization", "Bearer demo-alice-token")
			resp, err := fresh.client.Do(req)
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			ch <- outcome{status: resp.StatusCode, body: body, err: err}
		}()
	}
	wg.Wait()
	close(ch)
	statuses := map[int]int{}
	var reference []byte
	for out := range ch {
		if out.err != nil {
			t.Fatalf("concurrent revocation: %v", out.err)
		}
		statuses[out.status]++
		if reference == nil {
			reference = out.body
		} else if !bytes.Equal(reference, out.body) {
			t.Fatalf("concurrent revocation bodies differ")
		}
	}
	if statuses[201] != 1 || statuses[200] != workers-1 {
		t.Fatalf("revocation statuses = %#v", statuses)
	}
	if got := fresh.count(`SELECT COUNT(*) FROM audit_events WHERE certificate_id = 3 AND event_type = 'REVOKED'`); got != 1 {
		t.Fatalf("concurrent REVOKED audits = %d", got)
	}
}

func TestValidationRoutingAndHeaders(t *testing.T) {
	e := newTestEnv(t)

	resp, _ := e.do("GET", "/api/v1/not-a-route", "demo-alice-token", "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown route status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	requestID := resp.Header.Get("X-Request-Id")
	if !regexp.MustCompile(`^req-[0-9a-f]{16}$`).MatchString(requestID) {
		t.Fatalf("X-Request-Id = %q", requestID)
	}

	resp, body := e.do("GET", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "", nil)
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
		t.Fatalf("405 check: status=%d allow=%q body=%s", resp.StatusCode, resp.Header.Get("Allow"), body)
	}
	resp, _ = e.do("POST", "/api/v1/certificates", "demo-alice-token", "", nil)
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET" {
		t.Fatalf("405 certificates: status=%d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}

	badQueries := []string{
		"/api/v1/certificates?page=0",
		"/api/v1/certificates?pageSize=101",
		"/api/v1/certificates?page=1&page=2",
		"/api/v1/certificates?unknown=1",
		"/api/v1/certificates?sortBy=status",
		"/api/v1/certificates?sortOrder=up",
		"/api/v1/certificates?status=active",
		"/api/v1/certificates?courseId=0",
		"/api/v1/certificates/0",
		"/api/v1/certificates/abc",
		"/api/v1/certificates.csv?page=1",
		"/api/v1/certificates.csv?pageSize=20",
		"/api/v1/courses?page=-1",
		"/api/v1/courses/1/enrollments?completionStatus=done",
	}
	for _, path := range badQueries {
		t.Run(path, func(t *testing.T) {
			e.json("GET", path, "demo-alice-token", "", nil, 400)
		})
	}

	page := e.json("GET", "/api/v1/certificates?page=9&pageSize=20", "demo-alice-token", "", nil, 200)
	if page["total"].(float64) != 2 || len(items(t, page)) != 0 {
		t.Fatalf("out-of-range page = %#v", page)
	}
	second := e.json("GET", "/api/v1/certificates?pageSize=1&page=2", "demo-alice-token", "", nil, 200)
	if ids := itemIDs(t, second); fmt.Sprint(ids) != "[4]" {
		t.Fatalf("second page ids = %v", ids)
	}

	courses := e.json("GET", "/api/v1/courses?page=1&pageSize=1", "demo-alice-token", "", nil, 200)
	if courses["total"].(float64) != 2 || len(items(t, courses)) != 1 {
		t.Fatalf("courses page = %#v", courses)
	}

	// Error requestId must match the response header.
	resp, body = e.do("GET", "/api/v1/certificates?tenantId=1", "demo-alice-token", "", nil)
	var errorBody map[string]any
	if err := json.Unmarshal(body, &errorBody); err != nil {
		t.Fatalf("error body: %v", err)
	}
	errorObject := asMap(t, errorBody["error"])
	if errorObject["requestId"] != resp.Header.Get("X-Request-Id") {
		t.Fatalf("error requestId %v != header %q", errorObject["requestId"], resp.Header.Get("X-Request-Id"))
	}
	if errorObject["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("error code = %v", errorObject["code"])
	}

	// Authorization is required before resource checks.
	e.json("GET", "/api/v1/certificates/1", "", "", nil, 401)
	e.json("GET", "/api/v1/certificates/1", "unknown-token", "", nil, 401)
}

func TestCSVExportA6(t *testing.T) {
	e := newTestEnv(t)

	resp, body := e.do("GET", "/api/v1/certificates.csv", "demo-alice-token", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("CSV status = %d body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("CSV content type = %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Content-Disposition") != `attachment; filename="certificates.csv"` {
		t.Fatalf("CSV disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	want := "learnerName,courseTitle,serialNumber,issuedLocalDate\r\n" +
		"Taylor Kim,Safety 101,CERT-EAST-TAYLOR-0001,2026-01-11\r\n" +
		"Morgan Wu,Safety 101,CERT-EAST-MORGAN-0001,2026-01-10\r\n"
	if string(body) != want {
		t.Fatalf("CSV body:\n%q\nwant:\n%q", body, want)
	}

	_, again := e.do("GET", "/api/v1/certificates.csv", "demo-alice-token", "", nil)
	if !bytes.Equal(body, again) {
		t.Fatalf("two exports differ")
	}

	_, taylor := e.do("GET", "/api/v1/certificates.csv?q=Taylor", "demo-alice-token", "", nil)
	if !strings.Contains(string(taylor), "Taylor Kim") || strings.Contains(string(taylor), "Morgan Wu") {
		t.Fatalf("q=Taylor body: %q", taylor)
	}
	_, lower := e.do("GET", "/api/v1/certificates.csv?q=taylor", "demo-alice-token", "", nil)
	if !strings.Contains(string(lower), "Taylor Kim") {
		t.Fatalf("ASCII case-insensitive search failed: %q", lower)
	}
	_, percent := e.do("GET", "/api/v1/certificates.csv?q=%25", "demo-alice-token", "", nil)
	if string(percent) != "learnerName,courseTitle,serialNumber,issuedLocalDate\r\n" {
		t.Fatalf("%% was treated as a wildcard: %q", percent)
	}
	_, underscore := e.do("GET", "/api/v1/certificates.csv?q=_", "demo-alice-token", "", nil)
	if string(underscore) != "learnerName,courseTitle,serialNumber,issuedLocalDate\r\n" {
		t.Fatalf("_ was treated as a wildcard: %q", underscore)
	}

	_, reversed := e.do("GET", "/api/v1/certificates.csv?sortBy=learnerName&sortOrder=desc", "demo-alice-token", "", nil)
	if strings.Index(string(reversed), "Taylor Kim") < 0 || strings.Index(string(reversed), "Morgan Wu") < 0 ||
		strings.Index(string(reversed), "Taylor Kim") > strings.Index(string(reversed), "Morgan Wu") {
		t.Fatalf("CSV name desc order wrong: %q", reversed)
	}

	// Filtering and list availability after export.
	_, revoked := e.do("GET", "/api/v1/certificates.csv?status=REVOKED", "demo-alice-token", "", nil)
	if !strings.Contains(string(revoked), "Morgan Wu") || strings.Contains(string(revoked), "Taylor Kim") {
		t.Fatalf("status filter CSV: %q", revoked)
	}
	page := e.json("GET", "/api/v1/certificates", "demo-alice-token", "", nil, 200)
	if page["total"].(float64) != 2 {
		t.Fatalf("list failed after CSV: %#v", page)
	}
}

func TestCSVFormulaAndQuoting(t *testing.T) {
	if got := protectFormula("=SUM(A1)"); got != "'=SUM(A1)" {
		t.Fatalf("protectFormula(=) = %q", got)
	}
	if got := protectFormula("  +cmd"); got != "'  +cmd" {
		t.Fatalf("protectFormula(space+) = %q", got)
	}
	if got := protectFormula("-1"); got != "'-1" {
		t.Fatalf("protectFormula(-) = %q", got)
	}
	if got := protectFormula("@x"); got != "'@x" {
		t.Fatalf("protectFormula(@) = %q", got)
	}
	if got := protectFormula("Alex Lee"); got != "Alex Lee" {
		t.Fatalf("protectFormula(normal) = %q", got)
	}
	cases := map[string]string{
		"plain":       "plain",
		"a,b":         `"a,b"`,
		`a"b`:         `"a""b"`,
		"line\r\nend": "\"line\r\nend\"",
		" padded ":    `" padded "`,
	}
	for input, want := range cases {
		if got := csvField(input); got != want {
			t.Fatalf("csvField(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCSVAbortOnMidStreamError(t *testing.T) {
	e := newTestEnv(t)

	tx, err := e.db.Write.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO courses (id, tenant_id, title) VALUES (900, 1, 'Bulk Load')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert course: %v", err)
	}
	base := time.Date(2026, 2, 1, 13, 30, 0, 0, time.UTC)
	for i := 1; i <= 300; i++ {
		learnerID := int64(900 + i)
		if _, err := tx.Exec(`INSERT INTO learners (id, tenant_id, name) VALUES (?, 1, ?)`, learnerID, fmt.Sprintf("Bulk Learner %03d", i)); err != nil {
			tx.Rollback()
			t.Fatalf("insert learner: %v", err)
		}
		if _, err := tx.Exec(`INSERT INTO enrollments (id, tenant_id, course_id, learner_id, completion_status, completed_at)
			VALUES (?, 1, 900, ?, 'COMPLETED', '2026-01-01T00:00:00.000Z')`, learnerID, learnerID); err != nil {
			tx.Rollback()
			t.Fatalf("insert enrollment: %v", err)
		}
		issued := base.AddDate(0, 0, i%20)
		if _, err := tx.Exec(`INSERT INTO certificates
			(id, tenant_id, enrollment_id, course_id, learner_id, serial_number, status, issued_at, issued_local_date, issued_by_admin_id)
			VALUES (?, 1, ?, 900, ?, ?, 'ACTIVE', ?, ?, 1)`,
			learnerID, learnerID, learnerID, fmt.Sprintf("TEST-CSV-%04d", i),
			issued.Format("2006-01-02T15:04:05.000Z"), issued.Format("2006-01-02")); err != nil {
			tx.Rollback()
			t.Fatalf("insert certificate: %v", err)
		}
		if _, err := tx.Exec(`INSERT INTO audit_events (id, tenant_id, certificate_id, actor_admin_id, event_type, occurred_at)
			VALUES (?, 1, ?, 1, 'ISSUED', '2026-02-01T13:30:00.000Z')`, learnerID, learnerID); err != nil {
			tx.Rollback()
			t.Fatalf("insert audit: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit fixtures: %v", err)
	}

	e.srv.exportRowHook = func(row service.CertificateCSVRow, n int) error {
		if n == 250 {
			return fmt.Errorf("injected read failure")
		}
		return nil
	}

	req, err := http.NewRequest("GET", e.server.URL+"/api/v1/certificates.csv?sortBy=serialNumber", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer demo-alice-token")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("CSV request failed before headers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("aborted CSV status = %d", resp.StatusCode)
	}
	partial, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatalf("expected truncated download error, got %d bytes", len(partial))
	}
	if !strings.Contains(string(partial), "serialNumber") {
		t.Fatalf("expected a partial CSV with header, got %q", partial)
	}
}

func TestEnrollmentEligibilityA4(t *testing.T) {
	e := newTestEnv(t)
	course := e.json("GET", "/api/v1/courses/1/enrollments", "demo-alice-token", "", nil, 200)
	if course["total"].(float64) != 4 {
		t.Fatalf("course enrollments total = %v", course["total"])
	}
	wantReasons := []any{"COURSE_INCOMPLETE", "ALREADY_CERTIFIED", "CERTIFICATE_REVOKED"}
	for i, raw := range items(t, course) {
		item := asMap(t, raw)
		eligibility := asMap(t, item["issuanceEligibility"])
		if i == 0 {
			if eligibility["eligible"] != true || eligibility["reasonCode"] != nil {
				t.Fatalf("eligible item = %#v", eligibility)
			}
			if item["completedAt"] == nil || item["certificate"] != nil {
				t.Fatalf("Alex enrollment = %#v", item)
			}
			continue
		}
		if eligibility["eligible"] != false || eligibility["reasonCode"] != wantReasons[i-1] {
			t.Fatalf("item %d eligibility = %#v", i, eligibility)
		}
		if i == 1 && item["completedAt"] != nil {
			t.Fatalf("INCOMPLETE completedAt = %#v", item["completedAt"])
		}
		if i > 1 && item["certificate"] == nil {
			t.Fatalf("item %d missing certificate", i)
		}
	}

	learner := e.json("GET", "/api/v1/learners/1/enrollments", "demo-alice-token", "", nil, 200)
	if learner["total"].(float64) != 2 {
		t.Fatalf("learner enrollments = %v", learner["total"])
	}
	filtered := e.json("GET", "/api/v1/learners/1/enrollments?courseId=2", "demo-alice-token", "", nil, 200)
	if filtered["total"].(float64) != 1 {
		t.Fatalf("learner course filter = %v", filtered["total"])
	}
	none := e.json("GET", "/api/v1/learners/1/enrollments?courseId=3", "demo-alice-token", "", nil, 200)
	if none["total"].(float64) != 0 {
		t.Fatalf("cross-tenant learner course filter = %v", none["total"])
	}
	completed := e.json("GET", "/api/v1/courses/1/enrollments?completionStatus=COMPLETED", "demo-alice-token", "", nil, 200)
	if completed["total"].(float64) != 3 {
		t.Fatalf("completion filter total = %v", completed["total"])
	}
}
