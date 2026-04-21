// Package history persiste un journal des posts effectués dans un SQLite local.
package history

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Entry struct {
	ID          int64  `json:"id"`
	Timestamp   string `json:"timestamp"`
	Type        string `json:"type"`        // "torrent" | "nzb" | "ddl"
	TitleID     int    `json:"title_id"`
	TitleName   string `json:"title_name"`
	Saison      int    `json:"saison"`
	Episode     int    `json:"episode"`
	Qualite     int    `json:"qualite"`
	QualiteName string `json:"qualite_name"`
	HydrackerID int    `json:"hydracker_id"`
	Filename    string `json:"filename"`
	Links       string `json:"links"`
	Status      string `json:"status"` // "ok" | "error"
	Error       string `json:"error"`
}

type Store struct {
	db *sql.DB
}

func Open() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "go-post-tools")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			type TEXT NOT NULL,
			title_id INTEGER,
			title_name TEXT,
			saison INTEGER,
			episode INTEGER,
			qualite INTEGER,
			qualite_name TEXT,
			hydracker_id INTEGER,
			filename TEXT,
			links TEXT,
			status TEXT,
			error TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_posts_timestamp ON posts(timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_posts_title ON posts(title_id);
		CREATE INDEX IF NOT EXISTS idx_posts_type ON posts(type);
	`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) Add(e Entry) error {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().Format(time.RFC3339)
	}
	_, err := s.db.Exec(
		`INSERT INTO posts (timestamp, type, title_id, title_name, saison, episode, qualite, qualite_name, hydracker_id, filename, links, status, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp, e.Type, e.TitleID, e.TitleName, e.Saison, e.Episode, e.Qualite, e.QualiteName,
		e.HydrackerID, e.Filename, e.Links, e.Status, e.Error,
	)
	return err
}

// List retourne les entrées triées par date décroissante, filtrables par type/title_id/texte.
func (s *Store) List(filterType, searchQuery string, titleID, limit int) ([]Entry, error) {
	query := `SELECT id, timestamp, type, title_id, title_name, saison, episode, qualite, qualite_name,
	          hydracker_id, filename, links, status, error FROM posts WHERE 1=1`
	args := []any{}
	if filterType != "" {
		query += ` AND type = ?`
		args = append(args, filterType)
	}
	if titleID > 0 {
		query += ` AND title_id = ?`
		args = append(args, titleID)
	}
	if searchQuery != "" {
		q := "%" + searchQuery + "%"
		query += ` AND (title_name LIKE ? OR filename LIKE ? OR links LIKE ?)`
		args = append(args, q, q, q)
	}
	query += ` ORDER BY timestamp DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.Type, &e.TitleID, &e.TitleName,
			&e.Saison, &e.Episode, &e.Qualite, &e.QualiteName,
			&e.HydrackerID, &e.Filename, &e.Links, &e.Status, &e.Error,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM posts WHERE id = ?`, id)
	return err
}

// HasTitleEpisode retourne true si un post OK existe déjà pour (title_id, saison, episode, qualite, type).
// Utilisé pour détecter les doublons avant upload.
func (s *Store) HasTitleEpisode(titleID, saison, episode, qualite int, postType string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM posts WHERE title_id = ? AND saison = ? AND episode = ? AND qualite = ? AND type = ? AND status = 'ok'`,
		titleID, saison, episode, qualite, postType,
	).Scan(&count)
	return count > 0, err
}

func (s *Store) Stats() (map[string]int, error) {
	out := map[string]int{"total": 0, "ok": 0, "error": 0, "torrent": 0, "nzb": 0, "ddl": 0}
	rows, err := s.db.Query(`SELECT type, status, COUNT(*) FROM posts GROUP BY type, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t, st string
		var c int
		if err := rows.Scan(&t, &st, &c); err != nil {
			return nil, err
		}
		out["total"] += c
		out[st] += c
		out[t] += c
	}
	return out, nil
}
