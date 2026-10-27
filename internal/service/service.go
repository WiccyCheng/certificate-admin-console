package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"certadmin/internal/store"
)

const utcLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrUnauthenticated     = errors.New("unauthenticated")
	ErrNotFound            = errors.New("not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrBusy                = errors.New("temporarily unavailable")
)

// Service owns all business operations and explicit SQL.
type Service struct {
	db  *store.DB
	now func() time.Time
}

func New(db *store.DB) *Service {
	return &Service{db: db, now: time.Now}
}

func formatUTC(t time.Time) string {
	return t.UTC().Format(utcLayout)
}

func parseUTC(value string) (time.Time, error) {
	return time.Parse(utcLayout, value)
}

func localDate(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

func localDateTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02T15:04:05.000-07:00")
}

func mapDBError(err error) error {
	if err == nil {
		return nil
	}
	if store.IsBusy(err) {
		return ErrBusy
	}
	return err
}

// newUUID returns a lowercase UUID v4.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:]), nil
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newULID builds a lexicographically sortable ULID at time t.
func newULID(t time.Time) (string, error) {
	var raw [16]byte
	ms := uint64(t.UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}

	bits := make([]byte, 0, 128)
	for _, v := range raw {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (v>>uint(i))&1)
		}
	}
	out := make([]byte, 26)
	out[0] = crockford[bitsValue(bits[0:3])]
	for i := 0; i < 25; i++ {
		out[i+1] = crockford[bitsValue(bits[3+i*5:3+(i+1)*5])]
	}
	return string(out), nil
}

func bitsValue(bits []byte) int {
	v := 0
	for _, bit := range bits {
		v = v<<1 | int(bit)
	}
	return v
}

// onlineSerial is the documented online issuance format:
// CERT-{TENANT}-{ULID}. Seed and large-dataset rows use separate deterministic
// prefixes so collisions are impossible by construction.
func onlineSerial(tenantCode string, t time.Time) (string, error) {
	id, err := newULID(t)
	if err != nil {
		return "", err
	}
	return "CERT-" + strings.ToUpper(tenantCode) + "-" + id, nil
}

// Authenticate resolves the fixed demo token to an administrator and tenant.
func (s *Service) Authenticate(ctx context.Context, token string) (*Actor, error) {
	email, ok := demoTokens[token]
	if !ok {
		return nil, ErrUnauthenticated
	}
	var actor Actor
	err := s.db.Read.QueryRowContext(ctx, `
		SELECT a.id, a.email, t.id, t.code, t.name, t.timezone
		FROM admins a
		JOIN tenants t ON t.id = a.tenant_id
		WHERE a.email = ?`, email).
		Scan(&actor.AdminID, &actor.Email, &actor.TenantID, &actor.TenantCode, &actor.TenantName, &actor.Timezone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnauthenticated
		}
		return nil, mapDBError(err)
	}
	loc, err := time.LoadLocation(actor.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", actor.Timezone, err)
	}
	actor.Location = loc
	return &actor, nil
}

var demoTokens = map[string]string{
	"demo-alice-token": "alice@east.test",
	"demo-bob-token":   "bob@west.test",
}
