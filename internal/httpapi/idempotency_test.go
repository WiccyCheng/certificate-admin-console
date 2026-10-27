package httpapi

import (
	"bytes"
	"net/http"
	"testing"
)

// A failed request must not consume the idempotency key, and the key scope is
// tenant + admin, so Bob may reuse the same literal key independently.
func TestIdempotencyKeyLifecycleAndScope(t *testing.T) {
	e := newTestEnv(t)
	jsonHeaders := map[string]string{
		"Idempotency-Key": "lifecycle-key",
		"Content-Type":    "application/json",
	}

	e.json("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", `{"unknown":1}`, jsonHeaders, 400)

	resp, firstBody := e.do("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "", map[string]string{
		"Idempotency-Key": "lifecycle-key",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("valid retry after 400 status = %d, body=%s", resp.StatusCode, firstBody)
	}

	resp, bobBody := e.do("POST", "/api/v1/courses/3/bulk-operations", "demo-bob-token", "{}", jsonHeaders)
	if resp.StatusCode != 201 {
		t.Fatalf("same key for another admin/tenant status = %d, body=%s", resp.StatusCode, bobBody)
	}
	if bytes.Equal(firstBody, bobBody) {
		t.Fatalf("different tenants returned the same operation result")
	}
	if got := e.count(`SELECT COUNT(*) FROM bulk_operations`); got != 2 {
		t.Fatalf("bulk operations = %d, want 2", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM certificates WHERE tenant_id = 2`); got != 3 {
		t.Fatalf("West certificates = %d, want 3", got)
	}

	resp, replay := e.do("POST", "/api/v1/courses/1/bulk-operations", "demo-alice-token", "", map[string]string{
		"Idempotency-Key": "lifecycle-key",
	})
	if resp.StatusCode != 200 || !bytes.Equal(firstBody, replay) {
		t.Fatalf("Alice replay status=%d body=%s", resp.StatusCode, replay)
	}
}

func TestMultipleAuthorizationHeaders(t *testing.T) {
	e := newTestEnv(t)
	req, err := http.NewRequest("GET", e.server.URL+"/api/v1/me", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Add("Authorization", "Bearer demo-alice-token")
	req.Header.Add("Authorization", "Bearer demo-bob-token")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("multiple Authorization status = %d", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("missing WWW-Authenticate")
	}
}
