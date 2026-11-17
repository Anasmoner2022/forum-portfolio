package main

import (
	"context"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"excerpt": func(s string, lengths ...int) string {
			n := 180
			if len(lengths) > 0 {
				n = lengths[0]
			}
			r := []rune(s)
			if len(r) > n {
				return string(r[:n]) + "…"
			}
			return s
		},
		"initial": func(s string) string {
			r := []rune(s)
			if len(r) == 0 {
				return "?"
			}
			return strings.ToUpper(string(r[0]))
		},
		"hasCategory": func(s string, id int64) bool {
			for _, v := range strings.Split(s, ",") {
				if v == strconv.FormatInt(id, 10) {
					return true
				}
			}
			return false
		},
		"date": func(s string) string { return s },
	}
}
func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (use 0.0.0.0:8080 in a container)")
	dbPath := flag.String("db", "data/forum.db", "SQLite database file")
	demo := flag.Bool("demo", false, "seed synthetic demo content and display ephemeral-data warning")
	secure := flag.Bool("secure-cookie", false, "require HTTPS for session cookies")
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		log.Fatal(err)
	}
	db, err := openDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if *demo {
		if err := seedDemo(db); err != nil {
			log.Fatal(err)
		}
	}
	if err := os.Chmod(*dbPath, 0600); err != nil {
		log.Fatal(err)
	}
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseGlob("web/templates/*.html")
	if err != nil {
		log.Fatal(err)
	}
	app := &App{demo: *demo, db: db, templates: tmpl, secure: *secure, now: time.Now, attempts: make(map[string]attempt)}
	server := &http.Server{Addr: *addr, Handler: app.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("Forum listening on %s (HTTPS cookies: %t)", *addr, *secure)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
