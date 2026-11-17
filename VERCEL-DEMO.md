# Optional ephemeral Vercel preview

This branch preserves the normal Go + SQLite application and adds an explicit **non-durable portfolio demo**. It is not a production deployment.

## Changes
- `Dockerfile.vercel` selects Vercel's Container Runtime, retaining the existing CGO build and server-rendered pages.
- `vercel.json` selects the Container framework and `PORT=8080`, matching the listening server.
- The demo writes SQLite + WAL files only to `/tmp/forum-demo/`, not the read-only application image.
- HTTPS-only cookies are enabled in the Vercel command.
- `-demo` transactionally adds three synthetic discussions and replies once per local database. The sample author cannot sign in.
- Every page visibly warns that content/accounts/sessions may vanish or differ across requests and asks visitors to use only made-up details.
- The usual Dockerfile, SQLite app, and default local command remain unchanged.

## Expected limitations
Vercel containers run as auto-scaling Functions. Local data is not durable or shared. A visitor may need to register again, lose a session immediately, or see a missing post after routing to another instance. Guest sample discussions remain reproducible in each freshly initialized instance. Do not claim shared-database consistency, reliable writes, global single-session enforcement, or production readiness. Auth attempt throttling is process-local.

This preview is for a reviewer to explore the interface. A real deployment needs durable storage and additional deployment review. Never put actual personal information, reused passwords or important content here.

## Local test
With Go and a C compiler installed, from this directory:

```sh
go run . -demo -db /tmp/forum-preview/forum.db
# Plain local HTTP: leave -secure-cookie off.
go test -race -cover ./...
go vet ./...
```

The Dockerfile.vercel command intentionally enables secure cookies for hosted HTTPS, so login will not persist if that command is tested only through plain HTTP.

## Deployment status
Prepared, not deployed. No Vercel project/account, billing, domain, or cloud database was created. Docker build and Vercel runtime startup remain unverified in this environment; the Docker CLI/engine is unavailable. Source/integration tests are recorded in VERIFICATION.md; these do not prove a cloud deployment succeeded.

## Official references checked November 17, 2026
- [Vercel Container Runtime](https://vercel.com/docs/functions/runtimes/container): root Dockerfile support, beta on all plans, HTTP port configuration, automatic scale-in.
- [Local SQLite limitations](https://vercel.com/kb/guide/is-sqlite-supported-in-vercel): local storage is ephemeral and not shared across instances.

## Container qualification in CI
The workflow builds the preview image separately and runs `bash scripts/smoke-vercel-container.sh forum-vercel-test`. This exercises its actual command, read-only root and writable temporary directory, guest pages, cookie flag, UID/file permissions, and fresh-container re-seeding. A passing run must be observed before calling the container tested; this does not by itself verify Vercel routing or runtime.
