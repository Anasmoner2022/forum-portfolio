# Verification record

Date: 2026-11-17. Environment: Linux, Go 1.25.5, CGO SQLite.

## Verified locally

- `go test ./...`: passed.
- `go test -race -cover ./...`: passed, **77.1% statement coverage**, 30.047 seconds.
- `go vet ./...`: passed.
- `go build .`: passed.
- `gofmt`: all Go source formatted.
- Integration tests use actual SQLite and HTTP handlers, not mocked persistence.
- Ten top-level tests, including 18 reaction-transition subtests.
- Tested unique username/email, simultaneous duplicate signup, bcrypt login, one active session, expiry/logout, CSRF/origin checks, guest write rejection, filters/counts, escaped hostile input, atomic categories, database reopening, and database-error responses.
- Regression: first/empty feed does not link page zero. Replies paginate at 50, reply 501 is visible on page 11, and reacting to an older reply returns to its page.
- Independent static review found and prompted fixes for first-page navigation and the former comment cap.
- Independent live desktop browser QA verified registration, post creation, like, comment and populated feed, with synthetic data in a separate temporary database. The rendered style was consistent with the approved cream/teal editorial direction.

- Independent uncached race test and vet also passed: 30.048 seconds, 77.1% coverage.

## Not verified / limits

- Docker engine was unavailable in the build environment. Dockerfile and Compose are provided, but build, container startup, restart persistence and container backup/restore are **not yet runtime-verified**.
- GitHub Actions has not run as of this record. A configured workflow is not a passing CI result.
- Mobile responsive CSS is present, but a trustworthy narrow-viewport visual run was unavailable because the QA browser disabled developer tools. No mobile screenshot pass is claimed.
- Final browser recheck used the rebuilt binary: first-page Previous link removed, singular comment label correct, SVG Join arrow rendered, signout worked, and existing synthetic content persisted across process restart.
- No deployment, production load test, external penetration test, email delivery, or production readiness claim.
- Generated concept boards contain illustrative content. The working app starts empty; they are not evidence of seeded users or live adoption.

All test accounts/content were synthetic. No database, credentials, session tokens or private teaching files are included in this repository.
