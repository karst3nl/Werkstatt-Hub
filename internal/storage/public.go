package storage

import (
	"context"
	"database/sql"
	"errors"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
)

// PublicEntries exposes only the shared workshop board. Public reads are
// separate from actor-checked operations so future private modules stay private.
func (s *Store) PublicEntries(ctx context.Context) ([]core.Entry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+entryColumns+" FROM entries WHERE module='basis' ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.Entry, 0)
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *Store) PublicEntry(ctx context.Context, id int64) (core.Entry, error) {
	return scanEntry(s.db.QueryRowContext(ctx, "SELECT "+entryColumns+" FROM entries WHERE id=? AND module='basis'", id))
}

func (s *Store) PublicIdeas(ctx context.Context, filter ideas.Filter) ([]ideas.Idea, error) {
	query, args, err := s.ideaQuery(filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ideas.Idea, 0)
	for rows.Next() {
		idea, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, idea)
	}
	return result, rows.Err()
}

func (s *Store) PublicIdea(ctx context.Context, id int64) (ideas.Idea, error) {
	return ideaFrom(ctx, s.db, id)
}

func (s *Store) PublicIdeaCategories(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT i.category FROM ideas i JOIN entries e ON e.id=i.entry_id WHERE e.module='idee' ORDER BY i.category COLLATE NOCASE,i.category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var category string
		if err = rows.Scan(&category); err != nil {
			return nil, err
		}
		result = append(result, category)
	}
	return result, rows.Err()
}

func (s *Store) PublicComments(ctx context.Context, entryID int64) ([]core.Comment, error) {
	result := make([]core.Comment, 0)
	err := s.write(ctx, func(tx *sql.Tx) error {
		var module string
		err := tx.QueryRowContext(ctx, "SELECT module FROM entries WHERE id=? AND module IN ('basis','idee','aufgabe','werkzeugmeldung','anschaffung','materialpruefung')", entryID).Scan(&module)
		if errors.Is(err, sql.ErrNoRows) {
			return core.ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,entry_id,author_id,body,signature_name,signature_at FROM comments WHERE entry_id=? ORDER BY id", entryID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var comment core.Comment
			var at string
			if err = rows.Scan(&comment.ID, &comment.EntryID, &comment.AuthorID, &comment.Body, &comment.Signature.Name, &at); err != nil {
				return err
			}
			if comment.Signature.At, err = parseTime(at); err != nil {
				return err
			}
			result = append(result, comment)
		}
		return rows.Err()
	})
	return result, err
}

// ActiveAuthors supplies the per-submission name list without issuing a session
// or granting that author's permissions to the browser.
func (s *Store) ActiveAuthors(ctx context.Context) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,role,active FROM users WHERE active=1 AND deleted=0 ORDER BY name COLLATE NOCASE,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]core.User, 0)
	for rows.Next() {
		var user core.User
		if err = rows.Scan(&user.ID, &user.Name, &user.Role, &user.Active); err != nil {
			return nil, err
		}
		result = append(result, user)
	}
	return result, rows.Err()
}
