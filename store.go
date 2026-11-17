package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"strings"
	"time"
)

type User struct {
	ID       int64
	Username string
}
type Category struct {
	ID         int64
	Name, Slug string
}
type Post struct {
	ID                                    int64
	Title, Body, Author, CreatedAt        string
	Categories                            []Category
	Likes, Dislikes, Comments, MyReaction int
}
type Comment struct {
	ID                          int64
	Body, Author, CreatedAt     string
	Likes, Dislikes, MyReaction int
}

const schema = `
CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE COLLATE NOCASE, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS categories(id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, slug TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS posts(id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 120), body TEXT NOT NULL CHECK(length(body) BETWEEN 1 AND 5000), created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS post_categories(post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE, category_id INTEGER NOT NULL REFERENCES categories(id), PRIMARY KEY(post_id,category_id));
CREATE TABLE IF NOT EXISTS comments(id INTEGER PRIMARY KEY, post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE, user_id INTEGER NOT NULL REFERENCES users(id), body TEXT NOT NULL CHECK(length(body) BETWEEN 1 AND 5000), created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS post_reactions(user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE, value INTEGER NOT NULL CHECK(value IN(-1,1)), PRIMARY KEY(user_id,post_id));
CREATE TABLE IF NOT EXISTS comment_reactions(user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE, value INTEGER NOT NULL CHECK(value IN(-1,1)), PRIMARY KEY(user_id,comment_id));
CREATE INDEX IF NOT EXISTS idx_posts_time ON posts(created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_comments_post ON comments(post_id,id);
INSERT OR IGNORE INTO categories(name,slug) VALUES ('Go','go'),('Web','web'),('Databases','databases'),('Career','career');
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func (a *App) categories() ([]Category, error) {
	rows, err := a.db.Query("SELECT id,name,slug FROM categories ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.Name, &c.Slug); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (a *App) postCategories(id int64) ([]Category, error) {
	rows, err := a.db.Query("SELECT c.id,c.name,c.slug FROM categories c JOIN post_categories pc ON pc.category_id=c.id WHERE pc.post_id=? ORDER BY c.id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.Name, &c.Slug); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func displayTime(n int64) string { return time.Unix(n, 0).UTC().Format("02 Jan 2006 · 15:04 UTC") }
func (a *App) posts(userID int64, category, filter, q string, id int64, page int) ([]Post, error) {
	query := `SELECT p.id,p.title,p.body,u.username,p.created_at,
 (SELECT count(*) FROM post_reactions r WHERE r.post_id=p.id AND r.value=1),
 (SELECT count(*) FROM post_reactions r WHERE r.post_id=p.id AND r.value=-1),
 (SELECT count(*) FROM comments c WHERE c.post_id=p.id),
 coalesce((SELECT value FROM post_reactions r WHERE r.post_id=p.id AND r.user_id=?),0)
 FROM posts p JOIN users u ON u.id=p.user_id WHERE 1=1`
	args := []any{userID}
	if id > 0 {
		query += " AND p.id=?"
		args = append(args, id)
	}
	if category != "" {
		query += " AND EXISTS(SELECT 1 FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id AND c.slug=?)"
		args = append(args, category)
	}
	if filter == "mine" {
		query += " AND p.user_id=?"
		args = append(args, userID)
	}
	if filter == "liked" {
		query += " AND EXISTS(SELECT 1 FROM post_reactions r WHERE r.post_id=p.id AND r.user_id=? AND r.value=1)"
		args = append(args, userID)
	}
	if q != "" {
		query += " AND (instr(lower(p.title),lower(?))>0 OR instr(lower(p.body),lower(?))>0)"
		args = append(args, q, q)
	}
	query += " ORDER BY p.created_at DESC,p.id DESC LIMIT 21 OFFSET ?"
	args = append(args, (page-1)*20)
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	out := []Post{}
	for rows.Next() {
		var p Post
		var ts int64
		if err = rows.Scan(&p.ID, &p.Title, &p.Body, &p.Author, &ts, &p.Likes, &p.Dislikes, &p.Comments, &p.MyReaction); err != nil {
			rows.Close()
			return nil, err
		}
		p.CreatedAt = displayTime(ts)
		out = append(out, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Categories, err = a.postCategories(out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (a *App) comments(id, uid int64, pages ...int) ([]Comment, error) {
	page := 1
	if len(pages) > 0 {
		page = pages[0]
	}
	rows, err := a.db.Query(`SELECT c.id,c.body,u.username,c.created_at,
 (SELECT count(*) FROM comment_reactions r WHERE r.comment_id=c.id AND r.value=1),
 (SELECT count(*) FROM comment_reactions r WHERE r.comment_id=c.id AND r.value=-1),
 coalesce((SELECT value FROM comment_reactions r WHERE r.comment_id=c.id AND r.user_id=?),0)
 FROM comments c JOIN users u ON u.id=c.user_id WHERE c.post_id=? ORDER BY c.id LIMIT 51 OFFSET ?`, uid, id, (page-1)*50)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		var ts int64
		if err = rows.Scan(&c.ID, &c.Body, &c.Author, &ts, &c.Likes, &c.Dislikes, &c.MyReaction); err != nil {
			return nil, err
		}
		c.CreatedAt = displayTime(ts)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (a *App) createPost(uid int64, title, body string, catIDs []int64) (int64, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT INTO posts(user_id,title,body,created_at) VALUES(?,?,?,?)", uid, title, body, a.now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, cat := range catIDs {
		if _, err = tx.Exec("INSERT INTO post_categories(post_id,category_id) VALUES(?,?)", id, cat); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}
func (a *App) react(uid, id int64, kind string, value int) (int64, error) {
	table, col := "post_reactions", "post_id"
	postID := id
	if kind == "comment" {
		table, col = "comment_reactions", "comment_id"
		if err := a.db.QueryRow("SELECT post_id FROM comments WHERE id=?", id).Scan(&postID); err != nil {
			return 0, err
		}
	} else {
		var found int64
		if err := a.db.QueryRow("SELECT id FROM posts WHERE id=?", id).Scan(&found); err != nil {
			return 0, err
		}
	}
	var err error
	if value == 0 {
		_, err = a.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE user_id=? AND %s=?", table, col), uid, id)
	} else {
		_, err = a.db.Exec(fmt.Sprintf("INSERT INTO %s(user_id,%s,value) VALUES(?,?,?) ON CONFLICT(user_id,%s) DO UPDATE SET value=excluded.value", table, col, col), uid, id, value)
	}
	return postID, err
}
func isConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
