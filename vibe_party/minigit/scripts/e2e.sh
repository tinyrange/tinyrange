#!/usr/bin/env bash
set -euo pipefail

echo "[e2e] starting"

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing $1"; exit 1; }; }
need git; need go; need curl

ROOT="$(pwd)"
BIN="$ROOT/bin/minigitd"
PORT=18080
ADDR=127.0.0.1:${PORT}

rm -rf "$ROOT/bin" && mkdir -p "$ROOT/bin"
echo "[e2e] building server"
GOFLAGS=${GOFLAGS:-}
go build $GOFLAGS -o "$BIN" ./cmd/minigitd

TMP="$(mktemp -d)"
cleanup() { kill "${PID:-}" >/dev/null 2>&1 || true; rm -rf "$TMP"; }
trap cleanup EXIT

echo "[e2e] preparing origin repo"
git init --bare "$TMP/origin.git" >/dev/null
git clone "$TMP/origin.git" "$TMP/work" >/dev/null
pushd "$TMP/work" >/dev/null
git config user.name e2e && git config user.email e2e@example.com
echo "hello" > hello.txt
git add hello.txt
git commit -m "init" >/dev/null
git branch -M main
git push origin main >/dev/null
popd >/dev/null

echo "[e2e] starting server"
"$BIN" -addr ":${PORT}" -repos demo -seed "demo=$TMP/origin.git" &
PID=$!

echo "[e2e] waiting for health"
for i in $(seq 1 60); do
  if curl -fsS "http://${ADDR}/healthz" >/dev/null; then break; fi
  sleep 0.2
done
curl -fsS "http://${ADDR}/healthz" >/dev/null

echo "[e2e] cloning from minigit"
git -c protocol.version=2 clone "http://${ADDR}/demo" "$TMP/clone" >/dev/null
test -f "$TMP/clone/hello.txt"

echo "[e2e] pushing new commit"
pushd "$TMP/clone" >/dev/null
git config user.name e2e && git config user.email e2e@example.com
echo "world" >> hello.txt
git add hello.txt
git commit -m "update" >/dev/null
git -c protocol.version=2 push origin HEAD:refs/heads/main >/dev/null
popd >/dev/null

echo "[e2e] verifying push by recloning"
git -c protocol.version=2 clone "http://${ADDR}/demo" "$TMP/clone2" >/dev/null
grep -q "world" "$TMP/clone2/hello.txt"

echo "[e2e] ok"

