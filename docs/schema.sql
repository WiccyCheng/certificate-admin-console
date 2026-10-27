-- Confirmed reference schema for docs/data-model.md; not yet an application migration.
-- Configure WAL/busy_timeout at connection initialization, outside transactions.
PRAGMA foreign_keys = ON;

CREATE TABLE tenants (
    id INTEGER PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    timezone TEXT NOT NULL
) STRICT;

CREATE TABLE admins (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    email TEXT NOT NULL UNIQUE,
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE learners (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE courses (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    title TEXT NOT NULL,
    UNIQUE (tenant_id, id)
) STRICT;

CREATE TABLE enrollments (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    course_id INTEGER NOT NULL,
    learner_id INTEGER NOT NULL,
    completion_status TEXT NOT NULL
        CHECK (completion_status IN ('INCOMPLETE', 'COMPLETED')),
    completed_at TEXT,
    UNIQUE (tenant_id, course_id, learner_id),
    UNIQUE (tenant_id, id, course_id, learner_id),
    FOREIGN KEY (tenant_id, course_id) REFERENCES courses(tenant_id, id),
    FOREIGN KEY (tenant_id, learner_id) REFERENCES learners(tenant_id, id),
    CHECK (
        (completion_status = 'INCOMPLETE' AND completed_at IS NULL)
        OR (completion_status = 'COMPLETED' AND completed_at IS NOT NULL)
    )
) STRICT;

-- A row exists only after the operation commits; there is no PROCESSING state.
-- Certificates may reference it via the deferred FK even though it is written last.
CREATE TABLE bulk_operations (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    actor_admin_id INTEGER NOT NULL,
    course_id INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
    request_json TEXT NOT NULL CHECK (json_valid(request_json)),
    response_json TEXT NOT NULL CHECK (json_valid(response_json)),
    started_at TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    UNIQUE (tenant_id, id, course_id),
    UNIQUE (tenant_id, actor_admin_id, idempotency_key),
    FOREIGN KEY (tenant_id, actor_admin_id) REFERENCES admins(tenant_id, id),
    FOREIGN KEY (tenant_id, course_id) REFERENCES courses(tenant_id, id),
    CHECK (completed_at >= started_at)
) STRICT;

CREATE TABLE certificates (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    enrollment_id INTEGER NOT NULL,
    course_id INTEGER NOT NULL,
    learner_id INTEGER NOT NULL,
    serial_number TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'REVOKED')),
    issued_at TEXT NOT NULL,
    issued_local_date TEXT NOT NULL CHECK (
        length(issued_local_date) = 10
        AND issued_local_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    ),
    issued_by_admin_id INTEGER NOT NULL,
    revoked_at TEXT,
    revoked_by_admin_id INTEGER,
    bulk_operation_id TEXT,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, course_id, learner_id),
    UNIQUE (tenant_id, enrollment_id),
    FOREIGN KEY (tenant_id, enrollment_id, course_id, learner_id)
        REFERENCES enrollments(tenant_id, id, course_id, learner_id),
    FOREIGN KEY (tenant_id, issued_by_admin_id) REFERENCES admins(tenant_id, id),
    FOREIGN KEY (tenant_id, revoked_by_admin_id) REFERENCES admins(tenant_id, id),
    FOREIGN KEY (tenant_id, bulk_operation_id, course_id)
        REFERENCES bulk_operations(tenant_id, id, course_id)
        DEFERRABLE INITIALLY DEFERRED,
    CHECK (
        (status = 'ACTIVE' AND revoked_at IS NULL AND revoked_by_admin_id IS NULL)
        OR (status = 'REVOKED' AND revoked_at IS NOT NULL
            AND revoked_by_admin_id IS NOT NULL AND revoked_at >= issued_at)
    )
) STRICT;

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id),
    certificate_id INTEGER NOT NULL,
    actor_admin_id INTEGER NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('ISSUED', 'REVOKED')),
    occurred_at TEXT NOT NULL,
    UNIQUE (tenant_id, certificate_id, event_type),
    FOREIGN KEY (tenant_id, certificate_id) REFERENCES certificates(tenant_id, id),
    FOREIGN KEY (tenant_id, actor_admin_id) REFERENCES admins(tenant_id, id)
) STRICT;

CREATE INDEX idx_learners_tenant_name ON learners (tenant_id, name, id);
CREATE INDEX idx_courses_tenant_title ON courses (tenant_id, title, id);
CREATE INDEX idx_enrollments_tenant_learner ON enrollments (tenant_id, learner_id, id);
CREATE INDEX idx_certificates_tenant_date ON certificates (tenant_id, issued_local_date, id);
CREATE INDEX idx_certificates_tenant_serial ON certificates (tenant_id, serial_number, id);
CREATE INDEX idx_certificates_tenant_learner ON certificates (tenant_id, learner_id, id);
CREATE INDEX idx_audit_events_tenant_actor ON audit_events (tenant_id, actor_admin_id, id);
