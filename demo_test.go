package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestEphemeralDemo(t *testing.T) {
	a, s := setup(t)
	a.demo = true
	if err := seedDemo(a.db); err != nil {
		t.Fatal(err)
	}
	if err := seedDemo(a.db); err != nil {
		t.Fatal(err)
	}
	var posts, comments int
	a.db.QueryRow("SELECT count(*) FROM posts").Scan(&posts)
	a.db.QueryRow("SELECT count(*) FROM comments").Scan(&comments)
	if posts != 3 || comments != 3 {
		t.Fatalf("non-idempotent demo %d/%d", posts, comments)
	}
	b := newBrowser(t, s)
	html := b.get("/", 200)
	if !strings.Contains(html, "Portfolio demo") || !strings.Contains(html, "Go HTTP handlers") {
		t.Fatal("missing demo notice/content")
	}
	b.get("/post/1", 200)
	b.get("/?category=go", 200)
	b.get("/login", 200)
	b.get("/register", 200)
	b.post("/login", url.Values{"email": {"sample-author@demo.invalid"}, "password": {"!disabled-synthetic-account"}}, 401)
	b.register("demovisitor")
	b.newPost()
	fresh, server := setup(t)
	fresh.demo = true
	if err := seedDemo(fresh.db); err != nil {
		t.Fatal(err)
	}
	freshBrowser := newBrowser(t, server)
	if strings.Contains(freshBrowser.get("/", 200), "Go handlers") {
		t.Fatal("separate demo unexpectedly shares data")
	}
	if !strings.Contains(freshBrowser.get("/", 200), "Go HTTP handlers") {
		t.Fatal("fresh demo not viewable")
	}
}

func TestDemoIsOptIn(t *testing.T) {
	_, s := setup(t)
	html := newBrowser(t, s).get("/", 200)
	if strings.Contains(html, "Portfolio demo") || strings.Contains(html, "Go HTTP handlers") {
		t.Fatal("normal app polluted with demo")
	}
}
