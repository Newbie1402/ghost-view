#!/usr/bin/env bash
set -u
set -o pipefail
cd "$(dirname "$0")/.."
workspace=$(mktemp -d)
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$workspace"
}
trap cleanup EXIT
port=${ACCEPTANCE_PORT:-$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')}
base="http://127.0.0.1:$port"
if ! go build -trimpath -o "$workspace/ghostview" ./cmd/server; then exit 1; fi
ADDR="127.0.0.1:$port" PROVIDER_MODE=mock RATE_LIMIT_PER_MINUTE=1000 "$workspace/ghostview" >"$workspace/server.log" 2>&1 &
server_pid=$!
ready=0
for _ in $(seq 1 100); do
  if ! kill -0 "$server_pid" 2>/dev/null; then cat "$workspace/server.log"; exit 1; fi
  if curl --silent --fail "$base/api/v1/health" >"$workspace/health.json"; then ready=1; break; fi
  sleep 0.1
done
if [[ "$ready" != 1 ]]; then cat "$workspace/server.log"; printf 'Health readiness failed\n'; exit 1; fi
passed=0
failed=0
request() {
  code=$(curl --silent --show-error --max-time 10 -D "$workspace/headers" -o "$workspace/body" -w '%{http_code}' "$base$1") || return 1
  [[ "$code" == "$2" ]]
}
json_assert() {
  python3 - "$workspace/body" "$1" <<'PY'
import json, sys
with open(sys.argv[1]) as f: body = json.load(f)
assert eval(sys.argv[2], {"__builtins__": {}, "body": body, "len": len, "all": all}), body
PY
}
health() { request '/api/v1/health' 200 && json_assert 'body["error"] is None and body["data"]["status"] == "ok" and body["data"]["mode"] == "mock"'; }
exact() { request '/api/v1/search?platform=tiktok&q=%40alex.morgan' 200 && json_assert 'body["error"] is None and body["data"]["dataSource"] == "MOCK" and len(body["data"]["profiles"]) == 1 and body["data"]["profiles"][0]["username"] == "alex.morgan" and body["data"]["profiles"][0]["dataSource"] == "MOCK"'; }
ambiguous() { request '/api/v1/search?platform=instagram&q=alex' 200 && json_assert 'len(body["data"]["profiles"]) == 2'; }
public() { request '/api/v1/profiles/instagram/alex.morgan' 200 && json_assert 'body["error"] is None and body["data"]["dataSource"] == "MOCK" and body["data"]["username"] == "alex.morgan" and body["data"]["accessStatus"] == "PUBLIC"'; }
private() {
  request '/api/v1/profiles/instagram/private.user' 200 && json_assert 'body["data"]["accessStatus"] == "PRIVATE" and "postCount" not in body["data"]' &&
  request '/api/v1/profiles/instagram/private.user/posts' 403 && json_assert 'body["data"] is None and body["error"]["code"] == "PRIVATE"' &&
  request '/api/v1/profiles/instagram/private.user/stories' 403 && json_assert 'body["error"]["code"] == "PRIVATE"'
}
posts() {
  request '/api/v1/profiles/facebook/alex.morgan/posts' 200 && json_assert 'len(body["data"]["items"]) == 4 and body["data"]["nextCursor"] == "4"' &&
  request '/api/v1/profiles/facebook/alex.morgan/posts?cursor=4' 200 && json_assert 'len(body["data"]["items"]) == 4 and not body["data"].get("nextCursor")'
}
stories() {
  request '/api/v1/profiles/instagram/alex.morgan/stories' 200 && json_assert 'len(body["data"]) == 2 and body["data"][0]["type"] == "STORY" and body["data"][1]["duration"] > 0' &&
  request '/api/v1/profiles/instagram/alex.morgan/highlights' 200 && json_assert 'len(body["data"]) == 1 and len(body["data"][0]["items"]) == 2'
}
download() {
  request '/api/v1/media/instagram/post-1/download' 200 &&
  python3 - "$workspace/body" "$workspace/headers" <<'PY'
import sys, xml.etree.ElementTree as ET
with open(sys.argv[1], 'rb') as f: content=f.read()
with open(sys.argv[2]) as f: headers=f.read().lower()
assert 'content-disposition: attachment;' in headers
assert 'content-type: image/svg+xml' in headers
assert ET.fromstring(content).tag == '{http://www.w3.org/2000/svg}svg'
assert b'GhostView coast abstract demo artwork' in content
PY
}
ssrf() {
  local url
  for url in 'https%3A%2F%2Flocalhost%2Fa' 'https%3A%2F%2F127.0.0.1%2Fa' 'https%3A%2F%2F%5B%3A%3A1%5D%2Fa' 'https%3A%2F%2F10.0.0.1%2Fa' 'https%3A%2F%2F169.254.169.254%2Flatest%2Fmeta-data' 'https%3A%2F%2Fevil.example%2Fa'; do
    request "/api/v1/search?platform=tiktok&q=$url" 400 && json_assert 'body["data"] is None and body["error"]["code"] == "INVALID_INPUT"' || return 1
  done
  request '/api/v1/media/instagram/http%3A%2F%2F127.0.0.1/download' 400 && json_assert 'body["data"] is None'
}
headers() {
  request '/' 200 && python3 - "$workspace/headers" <<'PY'
import sys
with open(sys.argv[1]) as f: lines=f.read().splitlines()
headers={k.strip().lower(): v.strip() for line in lines if ':' in line for k,v in [line.split(':',1)]}
assert headers['x-content-type-options']=='nosniff'
assert "default-src 'none'" in headers['content-security-policy']
assert "script-src 'self'" in headers['content-security-policy']
assert "frame-ancestors 'none'" in headers['content-security-policy']
assert headers['referrer-policy']=='no-referrer'
assert 'camera=()' in headers['permissions-policy']
PY
  [[ "$?" == 0 ]] || return 1
  python3 -c 'print("x" * 4097, end="")' >"$workspace/oversize-body"
  code=$(curl --silent --show-error --max-time 10 -X GET -H 'Transfer-Encoding: chunked' --data-binary "@$workspace/oversize-body" -o "$workspace/body" -w '%{http_code}' "$base/api/v1/health") || return 1
  [[ "$code" == 413 ]] && json_assert 'body["data"] is None and body["error"]["code"] == "REQUEST_TOO_LARGE"'
}
check() {
  local label=$1
  shift
  if "$@"; then printf '[PASS] %s\n' "$label"; passed=$((passed+1)); else printf '[FAIL] %s\n' "$label"; failed=$((failed+1)); fi
}
printf '=================================\nGhostView Acceptance Test\n=================================\n'
check 'Health endpoint' health
check 'Exact username search' exact
check 'Ambiguous profile search' ambiguous
check 'Public profile' public
check 'Private profile protected' private
check 'Posts' posts
check 'Stories and highlights' stories
check 'Download' download
check 'SSRF protection' ssrf
check 'Security headers and request limits' headers
printf '\n%d passed\n%d failed\n' "$passed" "$failed"
[[ "$failed" == 0 ]]
