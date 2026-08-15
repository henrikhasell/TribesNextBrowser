package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// News, forums, the MOTD and the web links list.
//
// None of these can be rendered by a retail install. NewsGui, ForumsGui and
// weblinksmenu are driven by webnews.cs, webforums.cs and weblinks.cs but
// defined in no .vl2 and no loose file, so the script layer for three of the
// five community panes shipped in 2002 with its controls removed. Their
// ordinals are still reachable from the console, and answering them properly
// costs little; the alternative is a refusal that reads like a broken server.

//-----------------------------------------------------------------------------
// MOTD and news
//-----------------------------------------------------------------------------

func (s *Store) MOTD(ctx context.Context) (string, error) {
	var text string
	err := s.pool.QueryRow(ctx, `SELECT text FROM motd WHERE only_row`).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return text, err
}

func (s *Store) SetMOTD(ctx context.Context, text string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO motd (only_row, text, updated) VALUES (TRUE, $1, $2)
		ON CONFLICT (only_row) DO UPDATE SET text = EXCLUDED.text,
		                                     updated = EXCLUDED.updated`,
		text, s.now())
	return err
}

// NewsArticle is one row of array 0 and array 100, which share a schema.
type NewsArticle struct {
	ID       int64
	Category int64
	Headline string
	Body     string
	Author   Quad
	Created  int64
	Updated  int64
}

// NewsArticles lists a category, or every category when category is 0.
func (s *Store) NewsArticles(ctx context.Context, category int64, limit int) ([]NewsArticle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, category, headline, body, author_guid, created, updated
		  FROM news_articles
		 WHERE $1 = 0 OR category = $1
		 ORDER BY created DESC
		 LIMIT $2`, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		a      NewsArticle
		author string
	}
	var pending []raw
	for rows.Next() {
		var v raw
		if err := rows.Scan(&v.a.ID, &v.a.Category, &v.a.Headline, &v.a.Body,
			&v.author, &v.a.Created, &v.a.Updated); err != nil {
			return nil, err
		}
		pending = append(pending, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []NewsArticle{}
	for _, v := range pending {
		if v.a.Author, err = s.Quad(ctx, v.author); err != nil {
			return nil, err
		}
		out = append(out, v.a)
	}
	return out, nil
}

func (s *Store) PostNews(ctx context.Context, guid string, category int64, headline, body string) error {
	now := s.now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO news_articles (category, headline, body, author_guid, created, updated)
		VALUES ($1, $2, $3, $4, $5, $5)`, category, headline, body, guid, now)
	return err
}

func (s *Store) EditNews(ctx context.Context, id int64, category int64, headline, body string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE news_articles SET category = $2, headline = $3, body = $4, updated = $5
		 WHERE id = $1`, id, category, headline, body, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return refuse("no such article")
	}
	return nil
}

func (s *Store) DeleteNews(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM news_articles WHERE id = $1`, id)
	return err
}

//-----------------------------------------------------------------------------
// Forums
//-----------------------------------------------------------------------------

type Forum struct {
	ID       int64
	Name     string
	Flag     int
	Security int
}

func (s *Store) Forums(ctx context.Context) ([]Forum, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, flag, security FROM forums ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Forum{}
	for rows.Next() {
		var f Forum
		if err := rows.Scan(&f.ID, &f.Name, &f.Flag, &f.Security); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type Topic struct {
	ID          int64
	ForumID     int64
	Subject     string
	Author      string
	Created     int64
	Updated     int64
	Posts       int
	HasDeletes  bool
	Security    int
	MaxPostID   int64
	LockedState bool
}

func (s *Store) Topics(ctx context.Context, forumID int64, limit int) ([]Topic, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.forum_id, t.subject, COALESCE(a.name, '(unknown)'),
		       t.created, t.updated, t.locked, f.security,
		       COUNT(p.id), COALESCE(MAX(p.id), 0),
		       BOOL_OR(p.deleted)
		  FROM forum_topics t
		  JOIN forums f ON f.id = t.forum_id
		  LEFT JOIN accounts a ON a.guid = t.author_guid
		  LEFT JOIN forum_posts p ON p.topic_id = t.id
		 WHERE t.forum_id = $1 AND NOT t.deleted
		 GROUP BY t.id, a.name, f.security
		 ORDER BY t.updated DESC
		 LIMIT $2`, forumID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Topic{}
	for rows.Next() {
		var (
			t          Topic
			hasDeletes *bool
		)
		if err := rows.Scan(&t.ID, &t.ForumID, &t.Subject, &t.Author,
			&t.Created, &t.Updated, &t.LockedState, &t.Security,
			&t.Posts, &t.MaxPostID, &hasDeletes); err != nil {
			return nil, err
		}
		t.HasDeletes = hasDeletes != nil && *hasDeletes
		out = append(out, t)
	}
	return out, rows.Err()
}

// Post is one row of array 9.
type Post struct {
	ID       int64
	ParentID int64
	Author   Quad
	Subject  string
	Body     string
	Created  int64
	Deleted  bool
	IsAuthor bool
}

// PostsSince answers array 9: everything in a topic newer than the id the pane
// already has.
func (s *Store) PostsSince(ctx context.Context, topicID, since int64, caller string) ([]Post, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, author_guid, subject, body, created, deleted
		  FROM forum_posts
		 WHERE topic_id = $1 AND id > $2
		 ORDER BY id`, topicID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		p      Post
		author string
	}
	var pending []raw
	for rows.Next() {
		var v raw
		if err := rows.Scan(&v.p.ID, &v.p.ParentID, &v.author, &v.p.Subject,
			&v.p.Body, &v.p.Created, &v.p.Deleted); err != nil {
			return nil, err
		}
		pending = append(pending, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []Post{}
	for _, v := range pending {
		if v.p.Author, err = s.Quad(ctx, v.author); err != nil {
			return nil, err
		}
		v.p.IsAuthor = v.author == caller
		out = append(out, v.p)
	}
	return out, nil
}

// PostTopic starts a thread when topicID is 0, and replies otherwise. The
// client sends both through the same ordinal, distinguished only by whether it
// has a topic id yet.
func (s *Store) PostTopic(ctx context.Context, guid string, forumID, topicID, parentID int64, subject, body string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		now := s.now()

		if topicID == 0 {
			if err := tx.QueryRow(ctx, `
				INSERT INTO forum_topics (forum_id, subject, author_guid, created, updated)
				VALUES ($1, $2, $3, $4, $4) RETURNING id`,
				forumID, subject, guid, now).Scan(&topicID); err != nil {
				return err
			}
		} else {
			var locked bool
			if err := tx.QueryRow(ctx,
				`SELECT locked FROM forum_topics WHERE id = $1`, topicID).Scan(&locked); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return refuse("no such topic")
				}
				return err
			}
			if locked {
				return refuse("that topic is locked")
			}
			if _, err := tx.Exec(ctx,
				`UPDATE forum_topics SET updated = $2 WHERE id = $1`, topicID, now); err != nil {
				return err
			}
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO forum_posts (topic_id, parent_id, author_guid, subject, body, created)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			topicID, parentID, guid, subject, body, now)
		return err
	})
}

func (s *Store) EditPost(ctx context.Context, guid string, postID int64, subject, body string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var author string
		if err := tx.QueryRow(ctx,
			`SELECT author_guid FROM forum_posts WHERE id = $1`, postID).Scan(&author); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse("no such post")
			}
			return err
		}
		if author != guid {
			staff, err := s.IsStaff(ctx, guid)
			if err != nil {
				return err
			}
			if !staff {
				return refuse("that is not your post")
			}
		}
		_, err := tx.Exec(ctx,
			`UPDATE forum_posts SET subject = $2, body = $3 WHERE id = $1`,
			postID, subject, body)
		return err
	})
}

// DeletePost marks rather than removes: field 12 of an array 9 row is an
// isDeleted flag the client reads, so a removed row would vanish from a pane
// that expects to be told.
func (s *Store) DeletePost(ctx context.Context, guid string, postID int64) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var author string
		if err := tx.QueryRow(ctx,
			`SELECT author_guid FROM forum_posts WHERE id = $1`, postID).Scan(&author); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse("no such post")
			}
			return err
		}
		if author != guid {
			staff, err := s.IsStaff(ctx, guid)
			if err != nil {
				return err
			}
			if !staff {
				return refuse("that is not your post")
			}
		}
		_, err := tx.Exec(ctx, `UPDATE forum_posts SET deleted = TRUE WHERE id = $1`, postID)
		return err
	})
}

func (s *Store) SetTopicLocked(ctx context.Context, guid string, topicID int64, locked bool) error {
	staff, err := s.IsStaff(ctx, guid)
	if err != nil {
		return err
	}
	if !staff {
		return refuse("only forum staff may do that")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forum_topics SET locked = $2 WHERE id = $1`, topicID, locked)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return refuse("no such topic")
	}
	return nil
}

func (s *Store) MoveTopic(ctx context.Context, guid string, topicID, forumID int64) error {
	staff, err := s.IsStaff(ctx, guid)
	if err != nil {
		return err
	}
	if !staff {
		return refuse("only forum staff may do that")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE forum_topics SET forum_id = $2 WHERE id = $1`, topicID, forumID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return refuse("no such topic")
	}
	return nil
}

func (s *Store) RemoveTopic(ctx context.Context, guid string, topicID int64) error {
	staff, err := s.IsStaff(ctx, guid)
	if err != nil {
		return err
	}
	if !staff {
		return refuse("only forum staff may do that")
	}
	_, err = s.pool.Exec(ctx, `UPDATE forum_topics SET deleted = TRUE WHERE id = $1`, topicID)
	return err
}

//-----------------------------------------------------------------------------
// Web links
//-----------------------------------------------------------------------------

type WebLink struct {
	Name    string
	Address string
}

func (s *Store) WebLinks(ctx context.Context) ([]WebLink, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, address FROM web_links ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []WebLink{}
	for rows.Next() {
		var l WebLink
		if err := rows.Scan(&l.Name, &l.Address); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
