package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"reminder/backend/internal/model"
)

type AppointmentRepository struct {
	db *sql.DB
}

func NewAppointmentRepository(db *sql.DB) *AppointmentRepository {
	return &AppointmentRepository{db: db}
}

func (r *AppointmentRepository) List(ctx context.Context, from, to time.Time) ([]model.Event, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, title, start_at, end_at, external_source, external_id
FROM appointments
WHERE start_at < ? AND end_at > ?
ORDER BY start_at ASC, end_at ASC, id ASC
`, to.UTC().Format(time.RFC3339), from.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("list appointments: %w", err)
	}
	defer rows.Close()

	events := make([]model.Event, 0)
	for rows.Next() {
		event, err := scanAppointment(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate appointments: %w", err)
	}

	return events, nil
}

func (r *AppointmentRepository) UpsertImported(ctx context.Context, source string, input model.Event) (model.Event, bool, error) {
	if input.ExternalID == "" {
		input.ExternalID = input.ID
	}
	if input.ExternalID == "" {
		return model.Event{}, false, fmt.Errorf("external event id is required")
	}

	row := r.db.QueryRowContext(ctx, `
SELECT id, title, start_at, end_at, external_source, external_id
FROM appointments
WHERE external_source = ? AND external_id = ?
`, source, input.ExternalID)

	existing, found, err := scanOptionalAppointment(row)
	if err != nil {
		return model.Event{}, false, err
	}

	if found {
		if _, err := r.db.ExecContext(ctx, `
UPDATE appointments
SET title = ?, start_at = ?, end_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, input.Title, input.StartAt.UTC().Format(time.RFC3339), input.EndAt.UTC().Format(time.RFC3339), existing.ID); err != nil {
			return model.Event{}, false, fmt.Errorf("update imported appointment %s: %w", existing.ID, err)
		}

		existing.Title = input.Title
		existing.StartAt = input.StartAt
		existing.EndAt = input.EndAt
		return existing, false, nil
	}

	event := model.Event{
		ID:             newAppointmentID(),
		Source:         model.ProviderSystem,
		SourceLabel:    "System",
		SourceDetail:   sourceDetailForSource(source),
		ExternalSource: source,
		ExternalID:     input.ExternalID,
		Title:          input.Title,
		StartAt:        input.StartAt,
		EndAt:          input.EndAt,
	}

	if _, err := r.db.ExecContext(ctx, `
INSERT INTO appointments (id, title, start_at, end_at, external_source, external_id)
VALUES (?, ?, ?, ?, ?, ?)
`, event.ID, event.Title, event.StartAt.UTC().Format(time.RFC3339), event.EndAt.UTC().Format(time.RFC3339), event.ExternalSource, event.ExternalID); err != nil {
		return model.Event{}, false, fmt.Errorf("insert imported appointment %s: %w", event.ID, err)
	}

	return event, true, nil
}

func (r *AppointmentRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM appointments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete appointment %s: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for delete appointment %s: %w", id, err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type appointmentScanner interface {
	Scan(dest ...any) error
}

func scanOptionalAppointment(row appointmentScanner) (model.Event, bool, error) {
	event, err := scanAppointment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Event{}, false, nil
		}
		return model.Event{}, false, err
	}
	return event, true, nil
}

func scanAppointment(row appointmentScanner) (model.Event, error) {
	var (
		event          model.Event
		startAtRaw     string
		endAtRaw       string
		externalSource sql.NullString
		externalID     sql.NullString
	)

	if err := row.Scan(&event.ID, &event.Title, &startAtRaw, &endAtRaw, &externalSource, &externalID); err != nil {
		return model.Event{}, err
	}

	startAt, err := time.Parse(time.RFC3339, startAtRaw)
	if err != nil {
		return model.Event{}, fmt.Errorf("parse appointment startAt %q: %w", startAtRaw, err)
	}
	endAt, err := time.Parse(time.RFC3339, endAtRaw)
	if err != nil {
		return model.Event{}, fmt.Errorf("parse appointment endAt %q: %w", endAtRaw, err)
	}

	event.Source = model.ProviderSystem
	event.SourceLabel = "System"
	event.SourceDetail = sourceDetailForSource(externalSource.String)
	event.ExternalSource = externalSource.String
	event.ExternalID = externalID.String
	event.StartAt = startAt
	event.EndAt = endAt

	return event, nil
}

func sourceDetailForSource(source string) string {
	switch source {
	case model.ProviderGoogle:
		return "Imported from Google Calendar"
	case "":
		return "Stored in local system"
	default:
		return fmt.Sprintf("Imported from %s", source)
	}
}

func newAppointmentID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("system-%d", time.Now().UnixNano())
	}
	return "system-" + hex.EncodeToString(buffer)
}
