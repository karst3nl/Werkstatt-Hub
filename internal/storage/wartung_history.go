package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/wartung"
)

const wartungRecordColumns = `r.id,r.object_id,r.author_id,r.performed_date,r.note,r.finding,r.signature_name,r.signature_at,
 a.user_id,a.signature_name,a.signature_at`
const wartungRecordJoin = " FROM wartung_records r LEFT JOIN wartung_approvals a ON a.record_id=r.id"

func scanWartungRecord(row scanner) (wartung.Record, error) {
	var record wartung.Record
	var signed string
	var approvedBy sql.NullInt64
	var approvedName, approvedAt sql.NullString
	err := row.Scan(&record.ID, &record.ObjectID, &record.AuthorID, &record.PerformedDate, &record.Note, &record.Finding,
		&record.Signature.Name, &signed, &approvedBy, &approvedName, &approvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return record, core.ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if record.Signature.At, err = parseTime(signed); err != nil {
		return record, err
	}
	if approvedBy.Valid {
		record.Approval = &wartung.Approval{UserID: approvedBy.Int64, Signature: core.Signature{Name: approvedName.String}}
		record.Approval.Signature.At, err = parseTime(approvedAt.String)
	}
	return record, err
}

func wartungRecordFrom(ctx context.Context, q queryer, objectID, recordID int64) (wartung.Record, error) {
	return scanWartungRecord(q.QueryRowContext(ctx, "SELECT "+wartungRecordColumns+wartungRecordJoin+" WHERE r.object_id=? AND r.id=?", objectID, recordID))
}

func (s *Store) WartungHistory(ctx context.Context, objectID int64) ([]wartung.Record, error) {
	if _, err := s.WartungObject(ctx, objectID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+wartungRecordColumns+wartungRecordJoin+" WHERE r.object_id=? ORDER BY r.performed_date DESC,r.id DESC", objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]wartung.Record, 0)
	for rows.Next() {
		record, err := scanWartungRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// RecordWartung accepts the explicitly selected active author. It creates no
// session; admin and approval rights continue to require their own actor checks.
func (s *Store) RecordWartung(ctx context.Context, authorID, objectID int64, in wartung.RecordInput) (wartung.Record, error) {
	return s.recordWartungAt(ctx, authorID, objectID, in, time.Now().UTC())
}

func (s *Store) recordWartungAt(ctx context.Context, authorID, objectID int64, in wartung.RecordInput, now time.Time) (wartung.Record, error) {
	var result wartung.Record
	err := s.write(ctx, func(tx *sql.Tx) error {
		author, err := actorFrom(ctx, tx, authorID)
		if err != nil {
			return err
		}
		if !wartung.CanRecord(author) {
			return core.ErrForbidden
		}
		if !wartung.ValidPerformedDate(in.PerformedDate, now, s.taskSettings.Location) || !wartung.ValidFinding(in.Finding) {
			return core.ErrValidation
		}
		if in.Note, err = optionalToolText(in.Note, 20000); err != nil {
			return err
		}
		before, err := wartungObjectFrom(ctx, tx, objectID)
		if err != nil {
			return err
		}
		if !before.Active {
			return core.ErrForbidden
		}
		last := max(before.LastMaintenanceDate, in.PerformedDate)
		if _, err = wartung.NextDue(last, before.IntervalValue, before.IntervalUnit); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO wartung_records(object_id,author_id,performed_date,note,finding,signature_name,signature_at) VALUES(?,?,?,?,?,?,?)`, objectID, author.ID, in.PerformedDate, in.Note, in.Finding, author.Name, timestamp(now))
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE wartung_objects SET updated_at=?,version=version+1 WHERE id=?", timestamp(now), objectID); err != nil {
			return err
		}
		result, err = wartungRecordFrom(ctx, tx, objectID, id)
		if err != nil {
			return err
		}
		after, err := wartungObjectFrom(ctx, tx, objectID)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, author, "wartung.record_create", "wartung_record", id, before,
			struct {
				Record wartung.Record `json:"record"`
				Object wartung.Object `json:"object"`
			}{result, after}, now)
	})
	if err != nil {
		return wartung.Record{}, err
	}
	return result, nil
}

func (s *Store) ApproveWartung(ctx context.Context, actorID, objectID, recordID int64) (wartung.Record, error) {
	var result wartung.Record
	err := s.write(ctx, func(tx *sql.Tx) error {
		actor, err := actorFrom(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !wartung.CanApprove(actor) {
			return core.ErrForbidden
		}
		before, err := wartungRecordFrom(ctx, tx, objectID, recordID)
		if err != nil {
			return err
		}
		if before.Approval != nil {
			return core.ErrConflict
		}
		now := time.Now().UTC()
		if _, err = tx.ExecContext(ctx, "INSERT INTO wartung_approvals(record_id,user_id,signature_name,signature_at) VALUES(?,?,?,?)", recordID, actor.ID, actor.Name, timestamp(now)); err != nil {
			return err
		}
		result, err = wartungRecordFrom(ctx, tx, objectID, recordID)
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor, "wartung.record_approve", "wartung_record", recordID, before, result, now)
	})
	if err != nil {
		return wartung.Record{}, err
	}
	return result, nil
}
