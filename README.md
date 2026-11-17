# Forum

A server-rendered community forum built with **Go, SQLite and plain HTML/CSS**. The design pairs editorial typography with a responsive teal/navy interface. It is a portfolio and learning project, not a production-hosted service.

## Features

- Public discussion feed, thread pages, comments and separate like/dislike counts.
- Email/username/password registration, bcrypt hashing and expiring cookie sessions. Signing in again revokes the previous session; logout is a POST.
- Posts with 1–5 distinct categories, comments, and like/dislike/clear on posts and comments. Repeated reaction submissions are idempotent; opposite reactions switch.
- Category, My posts and Liked posts filters, combined with AND; literal text search and 20-item feed pagination.
- SQLite transactions and foreign-key constraints; safe server-side HTML escaping, CSRF protection, parameterized SQL, request limits and simple auth attempt throttling.
- Accessible labelled forms, keyboard focus states, responsive desktop/mobile layout and meaningful empty/error states. No JavaScript dependency or frontend framework.

## Run locally

Requires Go 1.25+ and a C compiler for `github.com/mattn/go-sqlite3` (CGO). On Windows use a compatible GCC toolchain or run inside WSL/Docker.

```sh
go mod download
go run .
```

Open **http://127.0.0.1:8080**. Register your own account; no default login or seeded real users exist. The app starts with four categories and no discussions. Create two accounts to try sessions and reactions.

```sh
go run . -addr 127.0.0.1:8081 -db data/my-forum.db
```

Sessions expire after 8 hours, enforced server-side. Passwords require 10–72 bytes (bcrypt limit); usernames allow 3–32 ASCII letters, numbers, `_` and `-`. Email and username uniqueness are case-insensitive. Twenty registration/login attempts per IP are allowed per 15 minutes; this in-memory limit is single-instance only, resets on restart and deliberately does not trust forwarded IP headers.

## Docker

```sh
docker compose up --build -d
docker compose logs -f
# Stop without removing the database volume:
docker compose down
# Start the same persistent data again:
docker compose up -d
```

The application runs as UID/GID 10001, listens on loopback at the host, and uses a named volume for SQLite. **Do not use `down -v` unless you intend to delete your forum data.** Docker build/runtime testing status is recorded in `VERIFICATION.md`; configuration alone is not runtime evidence. Base image version tags are specified but not immutable digests.

For HTTPS behind a reverse proxy use `-secure-cookie=true` and terminate TLS at the proxy. Do not enable this on plain HTTP or the browser will not send session cookies. Never expose this learning app to the Internet without your own deployment review, monitoring, durable backups and abuse controls.

## Tests

```sh
go test -race -cover ./...
go vet ./...
go build ./...
```

The tests use temporary real SQLite databases and HTTP servers. They cover registration conflicts, session replacement/expiry, public access, write authorization, CSRF and cross-origin requests, atomic post creation, reactions, filters, counts, escaping, oversized inputs, persistence and auth throttling. See `VERIFICATION.md` for actual run results and limitations.

## Architecture

- `main.go`: startup, configuration, template helpers and graceful shutdown.
- `app.go`: HTTP routing, validation, authentication and response rendering.
- `store.go`: relational schema and explicit parameterized SQL.
- `web/templates/page.html`: server-rendered views.
- `web/static/style.css`: responsive design system; local assets only.
- `app_test.go`: HTTP integration and persistence tests.

The app uses `net/http`, `html/template` and `database/sql`, plus the SQLite driver and `golang.org/x/crypto/bcrypt`. No ORM.

## Data model

```text
users 1---0..1 sessions
users 1---* posts 1---* comments *---1 users
posts *---* categories (post_categories)
users *---* posts      (post_reactions: value -1 or 1)
users *---* comments   (comment_reactions: value -1 or 1)
```

IDs are SQLite integer primary keys. Email/username are unique; session tokens are random 256-bit opaque values stored only as SHA-256 hashes in SQLite. Session rows are unique per user. Post/category joins and reactions have compound primary keys to prevent duplicate counts. Content is stored as plain text, not interpreted HTML/Markdown. Timestamps are UTC seconds. Schema creation is idempotent for this initial version; future schema changes require explicit migrations.

No delete endpoint is exposed. Database cascades remove dependent sessions/reactions if an administrator performs a separately authorized deletion; author references protect existing content. Do not edit the database manually while the application is active.

## Backup and restore (local)

For a simple consistent backup, stop the application first, then copy the **whole `data` directory**, including any SQLite WAL/SHM companions. Keep the copy private: it contains email addresses, password hashes and session state. For Compose, stop the service and copy the named volume using your normal trusted Docker backup procedure. Never share a database file in Git.

Restore into a **new** directory/volume, not over the original. Start the app against that restored copy, check users/posts/comments and invalidate restored sessions before use (delete `sessions` rows while stopped using a SQLite admin tool). Test your restore process before relying on backups. Container replacement and backup/restore were not verified unless explicitly stated in `VERIFICATION.md`.

## Scope and limitations

- No OAuth, email verification, password reset, notifications, direct messages, moderation or realtime chat.
- Replies are paginated at 50 per page; a new reply or comment reaction redirects to the relevant reply page.
- Search is SQLite text containment, not ranked full-text search.
- No claim of production security audit, load capacity or deployed uptime.
- AI-assisted implementation. The owner should review, understand and be able to explain the code before presenting it in interviews; do not claim unaided authorship or unverified production outcomes.

Inspired by the [01-edu Forum brief](https://github.com/01-edu/public/tree/master/subjects/forum). This repository is a separate implementation; the existing `forum-hub` teaching repository is unchanged.
