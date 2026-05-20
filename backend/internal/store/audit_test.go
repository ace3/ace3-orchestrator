package store

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestRecordAuditEventRedactsActor(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO audit_events (actor, action, target, request_id, ip, metadata)
		VALUES ($1,$2,$3,$4,$5,$6)`)).
		WithArgs("Bearer [REDACTED]", "POST /api/tasks/{id}/run", "/api/tasks/task-1/run", "req-1", "127.0.0.1", []byte(`{}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	st := New(sqlx.NewDb(raw, "sqlmock"), nil)
	st.RecordAuditEvent(context.Background(), AuditEventInput{
		Actor:     "Bearer secret-token",
		Action:    "POST /api/tasks/{id}/run",
		Target:    "/api/tasks/task-1/run",
		RequestID: "req-1",
		IP:        "127.0.0.1",
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
