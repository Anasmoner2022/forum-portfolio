package main

import (
	"database/sql"
	"time"
)

// seedDemo is enabled only by the explicit -demo flag. A transaction and marker
// keep repeated startup idempotent. Sample authors have no usable password.
func seedDemo(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS demo_seed(version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRow("SELECT count(*) FROM demo_seed WHERE version=1").Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return tx.Commit()
	}
	res, err := tx.Exec("INSERT INTO users(email,username,password_hash,created_at) VALUES(?,?,?,?)", "sample-author@demo.invalid", "Sample_author", "!disabled-synthetic-account", time.Now().Unix())
	if err != nil {
		return err
	}
	uid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	samples := []struct {
		title, body string
		cats        []int
	}{
		{"How do you keep Go HTTP handlers small?", "Synthetic example discussion: I keep request parsing and validation in handlers, database operations in a separate layer, and templates focused on presentation. What trade-offs have you found when testing each layer?", []int{1, 2}},
		{"SQLite transactions: making a post and its categories atomic", "Synthetic example discussion: A post and its categories should either save together or not save at all. A transaction is useful for avoiding partially-created discussions when a category is invalid.", []int{3, 1}},
		{"What makes a portfolio project easy to review?", "Synthetic example discussion: Clear setup instructions, focused tests, an honest verification record, and a small working demo help reviewers understand a project. This forum is a learning project, not a production service.", []int{4}},
	}
	for i, p := range samples {
		res, err = tx.Exec("INSERT INTO posts(user_id,title,body,created_at) VALUES(?,?,?,?)", uid, p.title, p.body, time.Now().Add(-time.Duration(i+1)*time.Hour).Unix())
		if err != nil {
			return err
		}
		pid, e := res.LastInsertId()
		if e != nil {
			return e
		}
		for _, cat := range p.cats {
			if _, err = tx.Exec("INSERT INTO post_categories(post_id,category_id) VALUES(?,?)", pid, cat); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("INSERT INTO comments(post_id,user_id,body,created_at) VALUES(?,?,?,?)", pid, uid, "Sample reply: this content is illustrative. You can browse the implementation and tests in the GitHub repository.", time.Now().Unix()); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO demo_seed(version) VALUES(1)"); err != nil {
		return err
	}
	return tx.Commit()
}
