package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type browser struct {
	t      *testing.T
	client *http.Client
	base   string
}

func setup(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseGlob("web/templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, templates: tmpl, now: time.Now, attempts: map[string]attempt{}}
	s := httptest.NewServer(a.routes())
	t.Cleanup(func() { s.Close(); db.Close() })
	return a, s
}
func newBrowser(t *testing.T, s *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, s.URL}
}
func (b *browser) get(path string, status int) string {
	b.t.Helper()
	r, e := b.client.Get(b.base + path)
	if e != nil {
		b.t.Fatal(e)
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != status {
		b.t.Fatalf("GET %s = %d want %d: %s", path, r.StatusCode, status, string(body))
	}
	return string(body)
}
func (b *browser) csrf() string {
	u, _ := url.Parse(b.base)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == "forum_csrf" {
			return c.Value
		}
	}
	return ""
}
func (b *browser) post(path string, v url.Values, status int) *http.Response {
	b.t.Helper()
	if v == nil {
		v = url.Values{}
	}
	if _, ok := v["csrf"]; !ok {
		v.Set("csrf", b.csrf())
	}
	r, e := b.client.PostForm(b.base+path, v)
	if e != nil {
		b.t.Fatal(e)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != status {
		b.t.Fatalf("POST %s=%d want %d: %s", path, r.StatusCode, status, string(body))
	}
	return r
}
func (b *browser) register(name string) {
	b.get("/register", 200)
	b.post("/register", url.Values{"email": {name + "@example.com"}, "username": {name}, "password": {"a-long-test-password"}}, 303)
}
func (b *browser) newPost() string {
	r := b.post("/posts", url.Values{"title": {"Go handlers"}, "body": {"How should I structure handlers?"}, "categories": {"1", "2"}}, 303)
	return r.Header.Get("Location")
}
func TestFullWorkflow(t *testing.T) {
	a, s := setup(t)
	guest := newBrowser(t, s)
	guest.get("/", 200)
	guest.get("/?filter=mine", 401)
	guest.post("/posts", url.Values{"title": {"denied"}}, 401)
	guest.post("/react", url.Values{"target_type": {"post"}, "target_id": {"1"}, "value": {"1"}}, 401)
	alice := newBrowser(t, s)
	alice.register("alice")
	path := alice.newPost()
	guest.get(path, 200)
	alice.post(path+"/comments", url.Values{"body": {"A useful answer."}}, 303)
	alice.post("/react", url.Values{"target_type": {"post"}, "target_id": {"1"}, "value": {"1"}}, 303)
	alice.post("/react", url.Values{"target_type": {"post"}, "target_id": {"1"}, "value": {"1"}}, 303)
	ps, e := a.posts(1, "", "", "", 1, 1)
	if e != nil || ps[0].Likes != 1 || ps[0].Comments != 1 {
		t.Fatalf("aggregate: %+v %v", ps, e)
	}
	if !strings.Contains(alice.get("/?category=go&filter=liked", 200), "Go handlers") {
		t.Fatal("liked AND category missing")
	}
	alice.post("/react", url.Values{"target_type": {"post"}, "target_id": {"1"}, "value": {"-1"}}, 303)
	ps, _ = a.posts(1, "", "", "", 1, 1)
	if ps[0].Likes != 0 || ps[0].Dislikes != 1 {
		t.Fatal("switch failed")
	}
	alice.post("/react", url.Values{"target_type": {"comment"}, "target_id": {"1"}, "value": {"1"}}, 303)
	cs, _ := a.comments(1, 0)
	if cs[0].Likes != 1 {
		t.Fatal("comment reaction failed")
	}
	alice.post("/react", url.Values{"target_type": {"comment"}, "target_id": {"1"}, "value": {"0"}}, 303)
	cs, _ = a.comments(1, 0)
	if cs[0].Likes != 0 {
		t.Fatal("clear failed")
	}
	alice.post("/logout", nil, 303)
	alice.get("/new", 401)
	guest.get(path, 200)
}
func TestRegistrationAndSessionReplacement(t *testing.T) {
	a, s := setup(t)
	one := newBrowser(t, s)
	one.register("alice")
	two := newBrowser(t, s)
	two.get("/register", 200)
	two.post("/register", url.Values{"email": {"ALICE@example.com"}, "username": {"other"}, "password": {"a-long-test-password"}}, 409)
	two.post("/register", url.Values{"email": {"other@example.com"}, "username": {"ALICE"}, "password": {"a-long-test-password"}}, 409)
	two.post("/login", url.Values{"email": {"alice@example.com"}, "password": {"wrong-password"}}, 401)
	two.post("/login", url.Values{"email": {"alice@example.com"}, "password": {"a-long-test-password"}}, 303)
	one.get("/new", 401)
	two.get("/new", 200)
	if _, e := a.db.Exec("UPDATE sessions SET expires_at=0"); e != nil {
		t.Fatal(e)
	}
	two.get("/new", 401)
	var stored string
	a.db.QueryRow("SELECT password_hash FROM users LIMIT 1").Scan(&stored)
	if !strings.HasPrefix(stored, "$2a$") || strings.Contains(stored, "test-password") {
		t.Fatal("password not bcrypt")
	}
}
func TestValidationCSRFAndEscaping(t *testing.T) {
	a, s := setup(t)
	b := newBrowser(t, s)
	b.register("alice")
	b.post("/posts", url.Values{"title": {"Valid"}, "body": {"body"}, "categories": {"1", "999"}}, 422)
	b.post("/posts", url.Values{"title": {" "}, "body": {"body"}, "categories": {"1"}}, 422)
	b.post("/posts", url.Values{"title": {"Valid"}, "body": {"body"}}, 422)
	b.post("/posts", url.Values{"title": {"Valid"}, "body": {"body"}, "categories": {"1"}, "csrf": {"forged"}}, 403)
	var count int
	a.db.QueryRow("SELECT count(*) FROM posts").Scan(&count)
	if count != 0 {
		t.Fatal("invalid post persisted")
	}
	r := b.post("/posts", url.Values{"title": {"<script>alert(1)</script>"}, "body": {"<img src=x onerror=alert(1)> '); DROP TABLE users; --"}, "categories": {"1"}}, 303)
	html := b.get(r.Header.Get("Location"), 200)
	if strings.Contains(html, "<script>") || strings.Contains(html, "<img src=x") {
		t.Fatal("unescaped hostile content")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("escaped text missing")
	}
	b.post("/post/1/comments", url.Values{"body": {" "}}, 422)
	b.post("/post/999/comments", url.Values{"body": {"hello"}}, 404)
	b.post("/react", url.Values{"target_type": {"users"}, "target_id": {"1"}, "value": {"1"}}, 400)
	b.post("/react", url.Values{"target_type": {"post"}, "target_id": {"999"}, "value": {"1"}}, 404)
	req, _ := http.NewRequest("POST", b.base+"/posts", strings.NewReader(url.Values{"csrf": {b.csrf()}, "title": {"x"}, "body": {"y"}, "categories": {"1"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, e := b.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin accepted")
	}
	b.post("/posts", url.Values{"body": {strings.Repeat("x", 70<<10)}}, 413)
	b.get("/healthz", 200)
	b.get("/missing", 404)
	b.get("/?filter=bad", 400)
	b.get("/?page=-1", 400)
}
func TestAtomicPostAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	db, e := openDB(path)
	if e != nil {
		t.Fatal(e)
	}
	a := &App{db: db, now: time.Now}
	db.Exec("INSERT INTO users(email,username,password_hash,created_at) VALUES('a@b.co','alice','test-only',0)")
	if _, e = a.createPost(1, "title", "body", []int64{1, 999}); e == nil {
		t.Fatal("invalid category accepted")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM posts").Scan(&count)
	if count != 0 {
		t.Fatal("not atomic")
	}
	id, e := a.createPost(1, "persist", "body", []int64{1})
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = openDB(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var title string
	if e = db.QueryRow("SELECT title FROM posts WHERE id=?", id).Scan(&title); e != nil || title != "persist" {
		t.Fatal("persistence failed", e)
	}
}
func TestFiltersAndCounts(t *testing.T) {
	a, s := setup(t)
	a1 := newBrowser(t, s)
	a1.register("alice")
	a1.newPost()
	a2 := newBrowser(t, s)
	a2.register("bob")
	a2.newPost()
	for i := 0; i < 3; i++ {
		a2.post("/post/1/comments", url.Values{"body": {fmt.Sprintf("reply %d", i)}}, 303)
	}
	a2.post("/react", url.Values{"target_type": {"post"}, "target_id": {"1"}, "value": {"1"}}, 303)
	ps, e := a.posts(2, "go", "liked", "", 0, 1)
	if e != nil || len(ps) != 1 || ps[0].Likes != 1 || ps[0].Comments != 3 || len(ps[0].Categories) != 2 {
		t.Fatalf("counts multiplied %+v %v", ps, e)
	}
	ps, _ = a.posts(2, "databases", "liked", "", 0, 1)
	if len(ps) != 0 {
		t.Fatal("filters not AND")
	}
	ps, _ = a.posts(2, "", "mine", "", 0, 1)
	if len(ps) != 1 || ps[0].Author != "bob" {
		t.Fatal("mine leaks")
	}
	ps, _ = a.posts(0, "", "", "handlers", 0, 1)
	if len(ps) != 2 {
		t.Fatal("search failed")
	}
}
func TestHeadersAndAuthLimit(t *testing.T) {
	_, s := setup(t)
	b := newBrowser(t, s)
	r, e := b.client.Get(b.base + "/")
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing headers")
	}
	for i := 0; i < 20; i++ {
		b.post("/login", url.Values{"email": {"nobody@example.com"}, "password": {"wrong-password"}}, 401)
	}
	b.post("/login", nil, 429)
}

func TestReactionTransitions(t *testing.T) {
	a, s := setup(t)
	b := newBrowser(t, s)
	b.register("alice")
	b.newPost()
	b.post("/post/1/comments", url.Values{"body": {"test reply"}}, 303)
	for _, kind := range []string{"post", "comment"} {
		for _, from := range []int{-1, 0, 1} {
			for _, to := range []int{-1, 0, 1} {
				t.Run(fmt.Sprintf("%s_%d_to_%d", kind, from, to), func(t *testing.T) {
					if _, e := a.react(1, 1, kind, from); e != nil {
						t.Fatal(e)
					}
					if _, e := a.react(1, 1, kind, to); e != nil {
						t.Fatal(e)
					}
					var likes, dislikes int
					if kind == "post" {
						ps, e := a.posts(1, "", "", "", 1, 1)
						if e != nil {
							t.Fatal(e)
						}
						likes, dislikes = ps[0].Likes, ps[0].Dislikes
					} else {
						cs, e := a.comments(1, 1)
						if e != nil {
							t.Fatal(e)
						}
						likes, dislikes = cs[0].Likes, cs[0].Dislikes
					}
					wantL, wantD := 0, 0
					if to == 1 {
						wantL = 1
					}
					if to == -1 {
						wantD = 1
					}
					if likes != wantL || dislikes != wantD {
						t.Fatalf("got %d/%d", likes, dislikes)
					}
				})
			}
		}
	}
}
func TestPaginationAndDatabaseFailure(t *testing.T) {
	a, s := setup(t)
	b := newBrowser(t, s)
	b.register("alice")
	for i := 0; i < 21; i++ {
		if _, e := a.createPost(1, fmt.Sprintf("Post %02d", i), "body", []int64{1}); e != nil {
			t.Fatal(e)
		}
	}
	html := b.get("/", 200)
	if !strings.Contains(html, "page=2") {
		t.Fatal("next page missing")
	}
	b.get("/?page=2", 200)
	a.db.Close()
	b.get("/healthz", 503)
	b.get("/", 503)
}
func TestDuplicateRegistrationRace(t *testing.T) {
	_, s := setup(t)
	one := newBrowser(t, s)
	two := newBrowser(t, s)
	one.get("/register", 200)
	two.get("/register", 200)
	results := make(chan int, 2)
	for _, b := range []*browser{one, two} {
		go func(b *browser) {
			r, e := b.client.PostForm(b.base+"/register", url.Values{"email": {"race@example.com"}, "username": {"racer"}, "password": {"a-long-test-password"}, "csrf": {b.csrf()}})
			if e != nil {
				results <- 0
				return
			}
			r.Body.Close()
			results <- r.StatusCode
		}(b)
	}
	x, y := <-results, <-results
	if !(x == 303 && y == 409 || x == 409 && y == 303) {
		t.Fatalf("race statuses %d %d", x, y)
	}
}

func TestFirstFeedAndCommentPagination(t *testing.T) {
	a, s := setup(t)
	b := newBrowser(t, s)
	if html := b.get("/", 200); strings.Contains(html, "page=0") {
		t.Fatal("empty feed links page zero")
	}
	b.register("alice")
	b.newPost()
	if html := b.get("/", 200); strings.Contains(html, "page=0") {
		t.Fatal("first feed links page zero")
	}
	tx, e := a.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 500; i++ {
		if _, e = tx.Exec("INSERT INTO comments(post_id,user_id,body,created_at) VALUES(1,1,?,?)", fmt.Sprintf("Reply-number-%03d", i), a.now().Unix()); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	first := b.get("/post/1", 200)
	if !strings.Contains(first, "Reply-number-050") || strings.Contains(first, "Reply-number-051") || !strings.Contains(first, "cpage=2") {
		t.Fatal("first reply page incorrect")
	}
	second := b.get("/post/1?cpage=2", 200)
	if !strings.Contains(second, "Reply-number-051") || strings.Contains(second, "Reply-number-050") {
		t.Fatal("second reply page incorrect")
	}
	r := b.post("/post/1/comments", url.Values{"body": {"Visible reply number 501"}}, 303)
	if r.Header.Get("Location") != "/post/1?cpage=11#comments" {
		t.Fatal(r.Header.Get("Location"))
	}
	if !strings.Contains(b.get(r.Header.Get("Location"), 200), "Visible reply number 501") {
		t.Fatal("new reply inaccessible")
	}
	r = b.post("/react", url.Values{"target_type": {"comment"}, "target_id": {"51"}, "value": {"1"}}, 303)
	if r.Header.Get("Location") != "/post/1?cpage=2#comments" {
		t.Fatal("reaction loses comment page")
	}
	b.get("/post/1?cpage=0", 400)
	b.get("/post/1?cpage=no", 400)
}
