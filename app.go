package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const sessionTTL = 8 * time.Hour
const maxBody = 64 << 10

type View struct {
	Demo                                              bool
	Page, Title, CSRF, Error, Filter, Category, Query string
	Status                                            int
	User                                              *User
	Categories                                        []Category
	Posts                                             []Post
	Post                                              *Post
	Comments                                          []Comment
	Form                                              map[string]string
	PageNum                                           int
	HasNext                                           bool
	NextURL, PrevURL                                  string
	CommentNextURL, CommentPrevURL                    string
	CommentPage                                       int
}
type attempt struct {
	count int
	until time.Time
}
type App struct {
	demo      bool
	db        *sql.DB
	templates *template.Template
	secure    bool
	now       func() time.Time
	mu        sync.Mutex
	attempts  map[string]attempt
}

func token() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func (a *App) user(r *http.Request) (*User, error) {
	c, err := r.Cookie("forum_session")
	if err != nil {
		return nil, nil
	}
	if len(c.Value) != 64 {
		return nil, nil
	}
	var u User
	err = a.db.QueryRow("SELECT u.id,u.username FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?", digest(c.Value), a.now().Unix()).Scan(&u.ID, &u.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func uid(u *User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}
func (a *App) cookie(w http.ResponseWriter, name, value string, seconds int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: seconds, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
}
func (a *App) csrf(w http.ResponseWriter, r *http.Request) string {
	if c, e := r.Cookie("forum_csrf"); e == nil && len(c.Value) == 64 {
		return c.Value
	}
	v := token()
	a.cookie(w, "forum_csrf", v, int(sessionTTL.Seconds()))
	return v
}
func (a *App) form(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			a.error(w, r, 413, "That submission is too large.")
		} else {
			a.error(w, r, 400, "Could not read this form.")
		}
		return false
	}
	c, err := r.Cookie("forum_csrf")
	v := r.PostForm.Get("csrf")
	if err != nil || len(v) != 64 || subtle.ConstantTimeCompare([]byte(c.Value), []byte(v)) != 1 {
		a.error(w, r, 403, "This form expired. Refresh the page and try again.")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			a.error(w, r, 403, "This request came from a different site.")
			return false
		}
	}
	return true
}
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, v View) {
	v.Demo = a.demo
	var err error
	if v.User == nil {
		v.User, err = a.user(r)
		if err != nil {
			http.Error(w, "Service temporarily unavailable", 503)
			return
		}
	}
	if v.Categories == nil {
		v.Categories, err = a.categories()
		if err != nil {
			http.Error(w, "Service temporarily unavailable", 503)
			return
		}
	}
	v.CSRF = a.csrf(w, r)
	v.Status = status
	if v.Form == nil {
		v.Form = map[string]string{}
	}
	var b bytes.Buffer
	if err = a.templates.ExecuteTemplate(&b, "page", v); err != nil {
		log.Printf("render error: %v", err)
		http.Error(w, "Unable to display this page", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func (a *App) error(w http.ResponseWriter, r *http.Request, status int, msg string) {
	a.render(w, r, status, View{Page: "error", Title: http.StatusText(status), Error: msg})
}
func (a *App) requireUser(w http.ResponseWriter, r *http.Request) *User {
	u, err := a.user(r)
	if err != nil {
		a.error(w, r, 503, "Please try again shortly.")
		return nil
	}
	if u == nil {
		a.error(w, r, 401, "Sign in to continue. Your session may have expired.")
	}
	return u
}
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.feed)
	mux.HandleFunc("GET /post/{id}", a.thread)
	mux.HandleFunc("GET /new", func(w http.ResponseWriter, r *http.Request) {
		if u := a.requireUser(w, r); u != nil {
			a.render(w, r, 200, View{Page: "compose", Title: "Start a discussion", User: u})
		}
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, 200, View{Page: "login", Title: "Welcome back"})
	})
	mux.HandleFunc("GET /register", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, r, 200, View{Page: "register", Title: "Join the conversation"})
	})
	mux.HandleFunc("POST /register", a.register)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("POST /posts", a.newPost)
	mux.HandleFunc("POST /post/{id}/comments", a.newComment)
	mux.HandleFunc("POST /react", a.reaction)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.PingContext(r.Context()); err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if a.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		_, pattern := mux.Handler(r)
		if pattern == "" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead { // Preserve standard ServeMux method responses.
				mux.ServeHTTP(w, r)
				return
			}
			a.error(w, r, 404, "We could not find that page.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) feed(w http.ResponseWriter, r *http.Request) {
	u, err := a.user(r)
	if err != nil {
		a.error(w, r, 503, "Please try again shortly.")
		return
	}
	filter := r.URL.Query().Get("filter")
	if filter != "" && filter != "mine" && filter != "liked" {
		a.error(w, r, 400, "Unknown filter.")
		return
	}
	if filter != "" && u == nil {
		a.error(w, r, 401, "Sign in to view your discussions and likes.")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 120 {
		a.error(w, r, 400, "Search is limited to 120 characters.")
		return
	}
	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		page, err = strconv.Atoi(s)
		if err != nil || page < 1 || page > 10000 {
			a.error(w, r, 400, "Invalid page.")
			return
		}
	}
	cat := r.URL.Query().Get("category")
	ps, err := a.posts(uid(u), cat, filter, q, 0, page)
	if err != nil {
		a.error(w, r, 500, "Could not load discussions.")
		return
	}
	more := len(ps) > 20
	if more {
		ps = ps[:20]
	}
	values := r.URL.Query()
	values.Set("page", strconv.Itoa(page+1))
	next := "/?" + values.Encode()
	values.Set("page", strconv.Itoa(page-1))
	prev := ""
	if page > 1 {
		prev = "/?" + values.Encode()
	}
	a.render(w, r, 200, View{Page: "feed", Title: "Discussions", User: u, Posts: ps, Filter: filter, Category: cat, Query: q, PageNum: page, HasNext: more, NextURL: next, PrevURL: prev})
}
func pathID(r *http.Request) (int64, error) {
	n, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if n < 1 {
		return 0, errors.New("invalid id")
	}
	return n, e
}
func (a *App) thread(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.error(w, r, 404, "Discussion not found.")
		return
	}
	u, err := a.user(r)
	if err != nil {
		a.error(w, r, 503, "Please try again shortly.")
		return
	}
	ps, err := a.posts(uid(u), "", "", "", id, 1)
	if err != nil {
		a.error(w, r, 500, "Could not load this discussion.")
		return
	}
	if len(ps) == 0 {
		a.error(w, r, 404, "Discussion not found.")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("cpage"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 10000 {
			a.error(w, r, 400, "Invalid comment page.")
			return
		}
	}
	cs, err := a.comments(id, uid(u), page)
	if err != nil {
		a.error(w, r, 500, "Could not load replies.")
		return
	}
	next, prev := "", ""
	if len(cs) > 50 {
		cs = cs[:50]
		next = fmt.Sprintf("/post/%d?cpage=%d#comments", id, page+1)
	}
	if page > 1 {
		prev = fmt.Sprintf("/post/%d?cpage=%d#comments", id, page-1)
	}
	a.render(w, r, 200, View{Page: "thread", Title: ps[0].Title, User: u, Post: &ps[0], Comments: cs, CommentNextURL: next, CommentPrevURL: prev, CommentPage: page})
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-only-not-an-account-password"), bcrypt.DefaultCost)

func (a *App) allowAuth(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for k, v := range a.attempts {
		if !v.until.After(now) {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[host]
	if v.count >= 20 {
		return false
	}
	if v.count == 0 {
		v.until = now.Add(15 * time.Minute)
	}
	v.count++
	a.attempts[host] = v
	return true
}
func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (a *App) register(w http.ResponseWriter, r *http.Request) {
	if !a.form(w, r) {
		return
	}
	if !a.allowAuth(r) {
		a.error(w, r, 429, "Too many attempts. Try again in 15 minutes.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	name := strings.TrimSpace(r.PostForm.Get("username"))
	pass := r.PostForm.Get("password")
	v := View{Page: "register", Title: "Join the conversation", Form: map[string]string{"email": email, "username": name}}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 || !validUsername(name) || len(pass) < 10 || len(pass) > 72 {
		v.Error = "Use a valid email, a 3–32 character username (letters, numbers, _ or -), and a password of 10–72 bytes."
		a.render(w, r, 422, v)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		a.error(w, r, 500, "Could not create your account.")
		return
	}
	res, err := a.db.Exec("INSERT INTO users(email,username,password_hash,created_at) VALUES(?,?,?,?)", email, name, string(hash), a.now().Unix())
	if isConflict(err) {
		v.Error = "That email or username is already registered."
		a.render(w, r, 409, v)
		return
	}
	if err != nil {
		a.error(w, r, 500, "Could not create your account.")
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		a.error(w, r, 500, "Please sign in to your new account.")
		return
	}
	if !a.startSession(w, r, id) {
		return
	}
	http.Redirect(w, r, "/", 303)
}
func (a *App) startSession(w http.ResponseWriter, r *http.Request, id int64) bool {
	t := token()
	_, err := a.db.Exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET token_hash=excluded.token_hash, expires_at=excluded.expires_at", digest(t), id, a.now().Add(sessionTTL).Unix())
	if err != nil {
		a.error(w, r, 500, "Could not start your session.")
		return false
	}
	a.cookie(w, "forum_session", t, int(sessionTTL.Seconds()))
	a.cookie(w, "forum_csrf", token(), int(sessionTTL.Seconds()))
	return true
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.form(w, r) {
		return
	}
	if !a.allowAuth(r) {
		a.error(w, r, 429, "Too many attempts. Try again in 15 minutes.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	pass := r.PostForm.Get("password")
	var id int64
	var hash string
	err := a.db.QueryRow("SELECT id,password_hash FROM users WHERE email=?", email).Scan(&id, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.error(w, r, 503, "Please try again shortly.")
		return
	}
	if hash == "" {
		hash = string(dummyHash)
	}
	ok := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) == nil
	if err != nil || !ok {
		a.render(w, r, 401, View{Page: "login", Title: "Welcome back", Error: "Email or password is incorrect.", Form: map[string]string{"email": email}})
		return
	}
	if a.startSession(w, r, id) {
		http.Redirect(w, r, "/", 303)
	}
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if !a.form(w, r) {
		return
	}
	if c, e := r.Cookie("forum_session"); e == nil {
		if _, err := a.db.Exec("DELETE FROM sessions WHERE token_hash=?", digest(c.Value)); err != nil {
			a.error(w, r, 503, "Could not sign out. Please try again.")
			return
		}
	}
	a.cookie(w, "forum_session", "", -1)
	a.cookie(w, "forum_csrf", "", -1)
	http.Redirect(w, r, "/", 303)
}
func validText(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= max
}
func (a *App) newPost(w http.ResponseWriter, r *http.Request) {
	u := a.requireUser(w, r)
	if u == nil || !a.form(w, r) {
		return
	}
	title := strings.TrimSpace(r.PostForm.Get("title"))
	body := strings.TrimSpace(r.PostForm.Get("body"))
	raw := r.PostForm["categories"]
	v := View{Page: "compose", Title: "Start a discussion", User: u, Form: map[string]string{"title": title, "body": body, "categories": strings.Join(raw, ",")}}
	cats, err := a.categories()
	if err != nil {
		a.error(w, r, 500, "Could not load categories.")
		return
	}
	allowed := map[int64]bool{}
	for _, c := range cats {
		allowed[c.ID] = true
	}
	ids := []int64{}
	seen := map[int64]bool{}
	valid := len(raw) > 0 && len(raw) <= 5
	for _, s := range raw {
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil || !allowed[n] || seen[n] {
			valid = false
		}
		seen[n] = true
		ids = append(ids, n)
	}
	if !validText(title, 120) || !validText(body, 5000) || !valid {
		v.Error = "Enter a title (1–120 characters), body (1–5000), and select 1–5 different categories."
		a.render(w, r, 422, v)
		return
	}
	id, err := a.createPost(u.ID, title, body, ids)
	if err != nil {
		a.error(w, r, 500, "Could not publish your discussion.")
		return
	}
	http.Redirect(w, r, "/post/"+strconv.FormatInt(id, 10), 303)
}
func (a *App) newComment(w http.ResponseWriter, r *http.Request) {
	u := a.requireUser(w, r)
	if u == nil || !a.form(w, r) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		a.error(w, r, 404, "Discussion not found.")
		return
	}
	var found int64
	if err = a.db.QueryRow("SELECT id FROM posts WHERE id=?", id).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		a.error(w, r, 404, "Discussion not found.")
		return
	} else if err != nil {
		a.error(w, r, 500, "Could not add your reply.")
		return
	}
	body := strings.TrimSpace(r.PostForm.Get("body"))
	if !validText(body, 5000) {
		a.error(w, r, 422, "A reply needs 1–5000 characters. Go back to edit your reply.")
		return
	}
	res, err := a.db.Exec("INSERT INTO comments(post_id,user_id,body,created_at) VALUES(?,?,?,?)", id, u.ID, body, a.now().Unix())
	if err != nil {
		a.error(w, r, 500, "Could not add your reply.")
		return
	}
	commentID, err := res.LastInsertId()
	if err != nil {
		a.error(w, r, 500, "Your reply was saved. Open the discussion again.")
		return
	}
	var count int
	if err := a.db.QueryRow("SELECT count(*) FROM comments WHERE post_id=? AND id<=?", id, commentID).Scan(&count); err != nil {
		a.error(w, r, 500, "Your reply was saved, but could not load its page. Open the discussion again.")
		return
	}
	page := (count + 49) / 50
	http.Redirect(w, r, fmt.Sprintf("/post/%d?cpage=%d#comments", id, page), 303)
}
func (a *App) reaction(w http.ResponseWriter, r *http.Request) {
	u := a.requireUser(w, r)
	if u == nil || !a.form(w, r) {
		return
	}
	id, e1 := strconv.ParseInt(r.PostForm.Get("target_id"), 10, 64)
	val, e2 := strconv.Atoi(r.PostForm.Get("value"))
	kind := r.PostForm.Get("target_type")
	if e1 != nil || e2 != nil || id < 1 || (val != 1 && val != -1 && val != 0) || (kind != "post" && kind != "comment") {
		a.error(w, r, 400, "Invalid reaction.")
		return
	}
	pid, err := a.react(u.ID, id, kind, val)
	if errors.Is(err, sql.ErrNoRows) {
		a.error(w, r, 404, "This discussion or reply was not found.")
		return
	}
	if err != nil {
		a.error(w, r, 500, "Could not save your reaction.")
		return
	}
	dest := "/post/" + strconv.FormatInt(pid, 10)
	if kind == "comment" {
		var count int
		if err := a.db.QueryRow("SELECT count(*) FROM comments WHERE post_id=? AND id<=?", pid, id).Scan(&count); err != nil {
			a.error(w, r, 500, "Reaction saved, but could not load its page.")
			return
		}
		dest = fmt.Sprintf("/post/%d?cpage=%d#comments", pid, (count+49)/50)
	}
	http.Redirect(w, r, dest, 303)
}
