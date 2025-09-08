Minimal Git HTTP (Smart) Server in Go

Overview
- Implements a very small subset of the Git smart protocol with an HTTP server focused on protocol v2.
- Supports cloning (upload-pack) and pushing (receive-pack) to in-memory repositories.
- No third-party dependencies; only Go standard library.

Status and Limitations
- Protocol v2: Implements ls-refs, fetch (sends a single full pack with all objects), and push (parses updates and consumes the incoming packfile). Sectioning is minimal; clients that require extra sections may not be supported.
- Protocol v0 fallback: Basic info/refs advertisement and simplified upload-pack/receive-pack flows to improve compatibility.
- Packfiles: Reader supports commit/tree/blob/tag and both OFS_DELTA and REF_DELTA (using previously decoded objects or objects already in the repo). Writer always emits full objects (no deltas). Checksums are written but not validated on read.
- Storage: In-memory only by default; APIs are abstracted so a persistent backend can be added later.
- This is intentionally minimal; it trades performance/features for clarity and small size.

Run
- Start the server with an in-memory repo named `demo`:
  - `go run ./cmd/minigitd -addr :8080 -repos demo`
- Seed from a remote origin on startup:
  - `go run ./cmd/minigitd -addr :8080 -repos demo -seed demo=https://github.com/user/repo.git`
  - Multiple pairs supported: `-seed a=url1,b=url2`

Clone
- From another shell:
  - `git -c protocol.version=2 clone http://localhost:8080/demo`
  - Note: Repos are empty until you push. You can also let Git fall back to v0 if v2 negotiation fails.

Push
- Initialize a local repo and push a branch:
  - `git init . && git commit --allow-empty -m init`
  - `git remote add origin http://localhost:8080/demo`
  - `git push origin HEAD:refs/heads/main`

Design
- `internal/git/pktline.go`: pkt-line reader/writer (flush and delim supported).
- `internal/git/pack.go`: packfile reader/writer; applies deltas; emits full-object packs.
- `internal/server/http.go`: HTTP handlers for v2 (upload-pack/receive-pack) and minimal v0 fallback.
- `internal/storage`: storage interfaces and types.
- `internal/storage/memstore`: in-memory implementation of objects and refs.

Next Improvements (if needed)
- Add persistent storage backend (filesystem) implementing `storage.Repo`.
- Validate and compute pack trailer checksum on read.
- Implement more complete v2 sections (acknowledgments, shallow-info) and server options.
- Smarter upload-pack to honor wants/haves and send smaller packs.

E2E Test
- Run a local end-to-end test that seeds from a temporary local origin, then clone and push back:
  - `bash scripts/e2e.sh`
  - Requires `git`, `go`, and `curl` in PATH.

