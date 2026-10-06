#!/usr/bin/env bash
# Opt-in real network verification. Restrictions and upstream failures fail the
# check; this script never substitutes demo fixtures for live responses.
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
platform=${LIVE_PLATFORM:-instagram}
username=${LIVE_USERNAME:-marcmarquez93}
case "$platform" in
  tiktok) search_input=${LIVE_INPUT:-"https://www.tiktok.com/@$username?_r=1&_t=ZS-9AKf9BKqZhS"} ;;
  instagram) search_input=${LIVE_INPUT:-"https://www.instagram.com/$username/"} ;;
  *) printf 'Set LIVE_PLATFORM=tiktok or instagram and a known public LIVE_USERNAME.\n'; exit 1 ;;
esac
port=${LIVE_ACCEPTANCE_PORT:-$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')}
base="http://127.0.0.1:$port"
go build -trimpath -o "$workspace/ghostview" ./cmd/server || exit 1
ADDR="127.0.0.1:$port" PROVIDER_MODE=live PROVIDER_TIMEOUT=12s RATE_LIMIT_PER_MINUTE=1000 "$workspace/ghostview" >"$workspace/server.log" 2>&1 &
server_pid=$!
ready=0
for _ in $(seq 1 100); do
  if ! kill -0 "$server_pid" 2>/dev/null; then cat "$workspace/server.log"; exit 1; fi
  if curl --silent --fail "$base/api/v1/health" >"$workspace/health"; then ready=1; break; fi
  sleep 0.1
done
if [[ "$ready" != 1 ]]; then cat "$workspace/server.log"; exit 1; fi
passed=0
failed=0
request() {
  local endpoint=$1 expected=$2
  shift 2
  code=$(curl --silent --show-error --max-time 30 -o "$workspace/body" -w '%{http_code}' "$@" "$base$endpoint") || return 1
  if [[ "$code" != "$expected" ]]; then printf 'Expected HTTP %s, received %s: ' "$expected" "$code"; cat "$workspace/body"; printf '\n'; return 1; fi
}
json_assert() {
  python3 - "$workspace/body" "$1" "$platform" "$username" <<'PY'
import json, sys
with open(sys.argv[1]) as f: body=json.load(f)
assert eval(sys.argv[2], {"__builtins__": {}, "body": body, "platform":sys.argv[3], "username":sys.argv[4], "len":len, "all":all}), {'error':body.get('error'), 'platform':sys.argv[3]}
PY
}
health() { request '/api/v1/health' 200 && json_assert 'body["data"]["status"] == "ok" and body["data"]["mode"] == "live"'; }
metadata() { request '/api/v1/providers' 200 && json_assert 'all(item["dataSource"] == "REAL" and item["status"] != "MOCK" for item in body["data"]) and len(body["data"]) == 3'; }
search() { request '/api/v1/search' 200 --get --data-urlencode "platform=instagram" --data-urlencode "q=$search_input" && json_assert 'body["error"] is None and body["data"]["dataSource"] == "REAL" and body["data"]["platform"] == platform and len(body["data"]["profiles"]) == 1 and body["data"]["profiles"][0]["username"] == username and body["data"]["profiles"][0]["dataSource"] == "REAL"'; }
profile() {
  request "/api/v1/profiles/$platform/$username" 200 && json_assert 'body["error"] is None and body["data"]["dataSource"] == "REAL" and body["data"]["accessStatus"] == "PUBLIC" and body["data"]["username"] == username and len(body["data"]["displayName"]) > 0' &&
  python3 - "$workspace/body" <<'PY'
import json, sys
from urllib.parse import urlparse
with open(sys.argv[1]) as f: profile=json.load(f)['data']
url=urlparse(profile['profileURL'])
assert url.scheme=='https' and not url.username and not url.password and not url.query
print('Actual live profile:', profile['username'], repr(profile['displayName']), 'followers=',profile.get('followerCount','unavailable'))
PY
}
posts() {
  if [[ "$platform" == tiktok ]]; then request "/api/v1/profiles/$platform/$username/posts" 422 && json_assert 'body["error"]["code"] == "UNSUPPORTED_CAPABILITY"';
  else
    request "/api/v1/profiles/$platform/$username/posts" 200 && json_assert 'len(body["data"]["items"]) > 0 and all(item["type"] == "IMAGE" and item["previewOnly"] is True and item["downloadable"] is False and item["mediaURL"] for item in body["data"]["items"])' || return 1
    python3 - "$workspace/body" "$workspace/media-request" <<'PY'
import ipaddress, json, socket, sys
from urllib.parse import urlparse
with open(sys.argv[1]) as f: items=json.load(f)['data']['items']
for item in items:
    parsed=urlparse(item['mediaURL'])
    assert parsed.scheme=='https' and not parsed.username and not parsed.password and parsed.port in (None,443)
    assert parsed.hostname.endswith(('.cdninstagram.com','.fbcdn.net'))
    assert '\r' not in item['mediaURL'] and '\n' not in item['mediaURL']
first=items[0]['mediaURL']
host=urlparse(first).hostname
addresses={record[4][0] for record in socket.getaddrinfo(host,443,type=socket.SOCK_STREAM)}
assert addresses and all(ipaddress.ip_address(address).is_global for address in addresses)
address=next((address for address in addresses if ':' not in address),next(iter(addresses)))
if ':' in address: address='['+address+']'
with open(sys.argv[2],'w') as f: f.write(first+'\n'+host+'\n'+address+'\n')
print('Actual live posts:',len(items),'public image previews')
PY
    [[ "$?" == 0 ]] || return 1
    local media_url media_host media_ip
    { IFS= read -r media_url; IFS= read -r media_host; IFS= read -r media_ip; } <"$workspace/media-request"
    code=$(curl --silent --show-error --noproxy '*' --proto '=https' --max-time 20 --max-filesize 26214400 --resolve "$media_host:443:$media_ip" -D "$workspace/media-headers" -o "$workspace/media.jpg" -w '%{http_code}' "$media_url") || return 1
    [[ "$code" == 200 ]] || return 1
    python3 - "$workspace/media.jpg" "$workspace/media-headers" <<'PY'
import hashlib, sys
with open(sys.argv[1],'rb') as f: content=f.read()
with open(sys.argv[2]) as f: lines=f.read().splitlines()
headers={key.lower():value.strip().lower() for line in lines if ':' in line for key,value in [line.split(':',1)]}
assert headers.get('content-type','').split(';')[0]=='image/jpeg'
assert content.startswith(b'\xff\xd8\xff') and len(content)>1000
print('Actual public JPEG:',len(content),'bytes; SHA256',hashlib.sha256(content).hexdigest())
PY
  fi
}
unsupported() {
  local resource
  for resource in stories highlights; do request "/api/v1/profiles/$platform/$username/$resource" 422 && json_assert 'body["data"] is None and body["error"]["code"] == "UNSUPPORTED_CAPABILITY"' || return 1; done
  request "/api/v1/media/$platform/unsupported-media/download" 422 && json_assert 'body["data"] is None and body["error"]["code"] == "UNSUPPORTED_CAPABILITY"'
}
ssrf() { request '/api/v1/search' 400 --get --data-urlencode 'platform=tiktok' --data-urlencode 'q=https://169.254.169.254/latest/meta-data' && json_assert 'body["data"] is None and body["error"]["code"] == "INVALID_INPUT"'; }
check() { local label=$1; shift; if "$@"; then printf '[PASS] %s\n' "$label"; passed=$((passed+1)); else printf '[FAIL] %s\n' "$label"; failed=$((failed+1)); fi; }
printf '=================================\nGhostView Live Acceptance (%s / %s)\n=================================\n' "$platform" "$username"
check 'Live health' health
check 'Real provider metadata' metadata
check 'Real exact profile search and URL normalization' search
check 'Real public profile' profile
if [[ "$platform" == tiktok ]]; then check 'Posts explicitly unsupported' posts; else check 'Real image posts and actual public JPEG' posts; fi
check 'Unsupported operations remain unavailable' unsupported
check 'SSRF protection' ssrf
printf '\n%d passed\n%d failed\n' "$passed" "$failed"
[[ "$failed" == 0 ]]
