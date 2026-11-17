#!/usr/bin/env bash
# Exercise the actual preview image with a read-only root and temporary /tmp.
# No volumes, external accounts or real user data are used.
set -euo pipefail
image="${1:-forum-vercel-test}"
name="forum-vercel-smoke-${GITHUB_RUN_ID:-local}-$$"
work="$(mktemp -d)"
cleanup() {
  docker logs "$name" 2>/dev/null || true
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
start() {
  docker run -d --name "$name" --read-only \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m,mode=1777 \
    --cap-drop ALL --security-opt no-new-privileges:true \
    -p 127.0.0.1::8080 -e PORT=8080 "$image" >/dev/null
  local address
  address="$(docker port "$name" 8080/tcp)"
  base="http://$address"
  for attempt in $(seq 1 30); do
    if curl --fail --silent "$base/healthz" >"$work/health"; then
      grep -qx ok "$work/health"
      return
    fi
    sleep 1
  done
  echo "Container did not become healthy" >&2
  return 1
}
start
curl --fail --silent --show-error -D "$work/headers" "$base/" >"$work/home"
grep -q 'Portfolio demo' "$work/home"
grep -q 'Go HTTP handlers' "$work/home"
grep -qi 'set-cookie:.*forum_csrf=.*Secure' "$work/headers"
curl --fail --silent --show-error "$base/post/1" >"$work/thread"
grep -q 'Sample reply' "$work/thread"
curl --fail --silent --show-error "$base/static/style.css" >"$work/style"
test -s "$work/style"
test "$(docker exec "$name" id -u)" = 10001
test "$(docker exec "$name" stat -c %a /tmp/forum-demo/forum.db)" = 600

# Recreate from scratch, not restart: no durable volume may be required.
docker rm -f "$name" >/dev/null
start
curl --fail --silent --show-error "$base/" >"$work/fresh"
grep -q 'Go HTTP handlers' "$work/fresh"
echo "Ephemeral container smoke passed"
