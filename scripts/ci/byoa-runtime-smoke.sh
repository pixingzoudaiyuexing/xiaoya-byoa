#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-xiaoya-byoa:ci}"
PORT="${BYOA_SMOKE_PORT:-15244}"
VOLUME="xiaoya-byoa-ci-data-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-0}"
CONTAINER="xiaoya-byoa-ci"
BASE_URL="http://127.0.0.1:${PORT}"
PHASE_FILE="${BYOA_SMOKE_PHASE_FILE:-/tmp/byoa-runtime-smoke-phase.txt}"

set_phase() {
  printf '%s\n' "$1" > "$PHASE_FILE"
  printf '=== BYOA smoke phase: %s ===\n' "$1"
}

cleanup() {
  docker logs "$CONTAINER" 2>/dev/null || true
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  docker volume rm -f "$VOLUME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker volume create "$VOLUME" >/dev/null

wait_for_ping() {
  local ok=0
  for _ in $(seq 1 90); do
    if curl -fsS "${BASE_URL}/ping" 2>/dev/null | grep -q 'pong'; then
      ok=1
      break
    fi
    if ! docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -q true; then
      echo 'Xiaoya BYOA container exited before health check passed' >&2
      docker logs "$CONTAINER" 2>/dev/null || true
      return 1
    fi
    sleep 1
  done
  if [ "$ok" -ne 1 ]; then
    echo 'Xiaoya BYOA /ping health check timed out' >&2
    docker logs "$CONTAINER" 2>/dev/null || true
    return 1
  fi
}

start_container() {
  local update_mode="${1:-if-newer}"
  docker run -d \
    --name "$CONTAINER" \
    -e BYOA_XIAOYA_STRICT=true \
    -e "BYOA_XIAOYA_UPDATE=${update_mode}" \
    -v "$VOLUME:/opt/alist/data" \
    -p "${PORT}:5244" \
    "$IMAGE" >/dev/null
  wait_for_ping
}

sql() {
  docker exec "$CONTAINER" sqlite3 /opt/alist/data/data.db "$1"
}

storage_snapshot() {
  local ali quark alias legacy
  ali="$(sql "select count(*) from x_storages where driver='AliyunShare';")"
  quark="$(sql "select count(*) from x_storages where driver='QuarkShare';")"
  alias="$(sql "select count(*) from x_storages where driver='Alias';")"
  legacy="$(sql "select count(*) from x_storages where driver in ('AliyundriveShare','AliyundriveShare2Open','AliyundriveShare2Pan115','AliyundriveCron','115 Share','115 Cloud','UCShare','PikPakShare','PikPak');")"
  echo "${ali}|${quark}|${alias}|${legacy}"
}

assert_storage_snapshot() {
  local snapshot="$1"
  local ali quark alias legacy
  IFS='|' read -r ali quark alias legacy <<<"$snapshot"
  echo "AliyunShare storages: $ali"
  echo "QuarkShare storages: $quark"
  echo "Alias storages: $alias"
  echo "Legacy/account-bound storages: $legacy"
  test "$ali" -gt 0
  test "$quark" -gt 0
  test "$alias" -gt 0
  test "$legacy" -eq 0
}

key_hash() {
  docker exec "$CONTAINER" sh -c '
    set -e
    test -s /opt/alist/data/byoa_cookie.key
    mode="$(stat -c %a /opt/alist/data/byoa_cookie.key)"
    test "$mode" = "600"
    sha256sum /opt/alist/data/byoa_cookie.key | awk "{print \$1}"
  '
}

admin_hash() {
  docker exec "$CONTAINER" sqlite3 -separator '|' /opt/alist/data/data.db \
    "select * from x_users where id=1;" | sha256sum | awk '{print $1}'
}

data_version() {
  sql "select value from byoa_state where key='xiaoya_data_version' limit 1;"
}

assert_config_migration_state() {
  local state
  state="$(sql "select value from byoa_state where key='openlist_v4_config_migrated' limit 1;")"
  test "$state" = "1"
  echo 'OpenList v4 config migration marker: 1'
}

assert_guest_catalog() {
  local guest_body
  guest_body="$(curl -fsS \
    -H 'Content-Type: application/json' \
    -d '{"path":"/","password":"","page":1,"per_page":0,"refresh":false}' \
    "${BASE_URL}/api/fs/list")"
  GUEST_BODY="$guest_body" python3 - <<'PY'
import json
import os
body = json.loads(os.environ["GUEST_BODY"])
assert body.get("code") == 200, body
content = (body.get("data") or {}).get("content") or []
assert len(content) > 0, body
print(f"Guest catalog entries: {len(content)}")
print("Guest root names:", ", ".join(x.get("name", "") for x in content[:20]))
PY
}

assert_qr_protocols() {
  BYOA_BASE_URL="$BASE_URL" BYOA_SMOKE_PHASE_FILE="$PHASE_FILE" python3 - <<'PY'
import json
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request

base = os.environ["BYOA_BASE_URL"]
phase_file = os.environ["BYOA_SMOKE_PHASE_FILE"]

def phase(name):
    with open(phase_file, "w", encoding="utf-8") as handle:
        handle.write(name + "\n")
    print(f"=== BYOA smoke phase: {name} ===")

def get_json(path, attempts=3):
    error = None
    for attempt in range(attempts):
        try:
            request = urllib.request.Request(base + path, method="GET")
            with urllib.request.urlopen(request, timeout=20) as response:
                body = json.loads(response.read().decode("utf-8"))
                body["_http_status"] = response.status
                return body
        except urllib.error.HTTPError as exc:
            try:
                body = json.loads(exc.read().decode("utf-8"))
                body["_http_status"] = exc.code
                return body
            except Exception:
                error = RuntimeError(f"local BYOA endpoint returned non-JSON HTTP {exc.code}")
        except Exception as exc:
            error = exc
        if attempt + 1 < attempts:
            time.sleep(1)
    raise error

def assert_start(provider, required):
    body = get_json(f"/api/public/byoa/{provider}/start")
    assert body.get("code") == 200, {
        "provider": provider,
        "operation": "start",
        "code": body.get("code"),
        "http_status": body.get("_http_status"),
    }
    data = body.get("data") or {}
    for key in required:
        assert data.get(key), (provider, key, "missing")
    assert str(data.get("qr_image", "")).startswith("data:image/png;base64,"), (provider, "qr_image", "invalid")
    print(f"{provider} QR start passed")
    return data

def classify_aliyun_start_failure(body):
    error_code = str((body.get("data") or {}).get("error_code") or "")
    structured = {
        "aliyun_network": "network",
        "aliyun_http_403": "upstream-http-403",
        "aliyun_http_429": "upstream-http-429",
        "aliyun_http_5xx": "upstream-http-5xx",
        "aliyun_http_other": "upstream-http-other",
        "aliyun_result_code_100": "upstream-result-100",
        "aliyun_result_code_other": "upstream-result-other",
        "aliyun_invalid_response": "invalid-response",
        "aliyun_qr_encode": "qr-encode",
    }
    if error_code in structured:
        return structured[error_code]

    # Backward-compatible fallback for images built before structured error codes.
    message = str(body.get("message") or "")
    if "aliyun QR generate result code: 100" in message:
        return "upstream-result-100"
    match = re.search(r"aliyun QR generate http status: ([0-9]{3})", message)
    if match:
        status = int(match.group(1))
        if status == 403:
            return "upstream-http-403"
        if status == 429:
            return "upstream-http-429"
        if 500 <= status <= 599:
            return "upstream-http-5xx"
        return "upstream-http-other"
    if "invalid aliyun QR response" in message:
        return "invalid-response"
    return "other"

phase("qr-quark-start")
quark = assert_start("quark", ("token", "qr_url", "qr_image"))
quark_query = urllib.parse.urlencode({"token": quark["token"]})
phase("qr-quark-status")
quark_status = get_json("/api/public/byoa/quark/status?" + quark_query)
assert quark_status.get("code") == 200, {
    "provider": "quark",
    "operation": "status",
    "code": quark_status.get("code"),
    "http_status": quark_status.get("_http_status"),
}
assert (quark_status.get("data") or {}).get("status") in {"pending", "scanned", "expired"}, {
    "provider": "quark",
    "operation": "status",
    "status": (quark_status.get("data") or {}).get("status"),
}
print("quark QR status poll passed:", (quark_status.get("data") or {}).get("status"))

phase("qr-aliyun-start")
aliyun_body = get_json("/api/public/byoa/aliyun/start")
if aliyun_body.get("code") != 200:
    classification = classify_aliyun_start_failure(aliyun_body)
    phase("qr-aliyun-start-" + classification)
    raise AssertionError({
        "provider": "aliyun",
        "operation": "start",
        "classification": classification,
        "code": aliyun_body.get("code"),
        "http_status": aliyun_body.get("_http_status"),
    })
aliyun = aliyun_body.get("data") or {}
for key in ("ck", "t", "qr_url", "qr_image"):
    assert aliyun.get(key), ("aliyun", key, "missing")
assert str(aliyun.get("qr_image", "")).startswith("data:image/png;base64,"), ("aliyun", "qr_image", "invalid")
print("aliyun QR start passed")
aliyun_query = urllib.parse.urlencode({"ck": aliyun["ck"], "t": aliyun["t"]})
phase("qr-aliyun-status")
aliyun_status = get_json("/api/public/byoa/aliyun/status?" + aliyun_query)
assert aliyun_status.get("code") == 200, {
    "provider": "aliyun",
    "operation": "status",
    "code": aliyun_status.get("code"),
    "http_status": aliyun_status.get("_http_status"),
}
assert (aliyun_status.get("data") or {}).get("status") in {"pending", "scanned", "expired", "canceled"}, {
    "provider": "aliyun",
    "operation": "status",
    "status": (aliyun_status.get("data") or {}).get("status"),
}
print("aliyun QR status poll passed:", (aliyun_status.get("data") or {}).get("status"))
phase("first-qr-protocols-complete")
PY
}

assert_anonymous_provider_browse_and_auth() {
  docker exec "$CONTAINER" sqlite3 /opt/alist/data/data.db \
    "select mount_path from x_storages where driver='AliyunShare' order by id limit 8;" \
    > /tmp/ali_paths.txt
  docker exec "$CONTAINER" sqlite3 /opt/alist/data/data.db \
    "select mount_path from x_storages where driver='QuarkShare' order by id limit 6;" \
    > /tmp/quark_paths.txt

  BYOA_BASE_URL="$BASE_URL" python3 - <<'PY'
import collections
import json
import os
import posixpath
import urllib.request

base = os.environ["BYOA_BASE_URL"] + "/api/fs"
media_exts = {
    ".mp4", ".mkv", ".ts", ".m2ts", ".avi", ".mov", ".flv", ".wmv",
    ".mp3", ".flac", ".aac", ".m4a", ".wav", ".wma", ".ape", ".ogg",
    ".strm", ".iso",
}

def post_json(endpoint, payload):
    request = urllib.request.Request(
        endpoint,
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    # Deliberately no CookieJar/Cookie header: anonymous browser.
    with urllib.request.urlopen(request, timeout=20) as response:
        return json.loads(response.read().decode("utf-8"))

def list_path(path):
    return post_json(base + "/list", {
        "path": path,
        "password": "",
        "page": 1,
        "per_page": 0,
        "refresh": False,
    })

def get_path(path):
    return post_json(base + "/get", {"path": path, "password": ""})

def load_mounts(filename):
    with open(filename, "r", encoding="utf-8") as handle:
        return [line.strip() for line in handle if line.strip()]

def first_browsable(provider, mounts):
    errors = []
    for mount in mounts:
        try:
            body = list_path(mount)
            content = (body.get("data") or {}).get("content") or []
            if body.get("code") == 200 and content:
                print(f"{provider} anonymous list passed: {mount} ({len(content)} entries)")
                print(f"{provider} sample: " + ", ".join(x.get("name", "") for x in content[:5]))
                return mount
            errors.append(f"{mount}: code={body.get('code')} entries={len(content)}")
        except Exception as exc:
            errors.append(f"{mount}: {type(exc).__name__}: {exc}")
    raise AssertionError(provider + " anonymous list failed: " + " | ".join(errors))

def find_media_file(provider, mounts, max_depth=4, max_lists=45):
    failures = []
    lists = 0
    for mount in mounts:
        queue = collections.deque([(mount, 0)])
        seen = set()
        while queue and lists < max_lists:
            current, depth = queue.popleft()
            if current in seen:
                continue
            seen.add(current)
            try:
                body = list_path(current)
                lists += 1
            except Exception as exc:
                failures.append(f"{current}: {type(exc).__name__}: {exc}")
                continue
            content = (body.get("data") or {}).get("content") or []
            if body.get("code") != 200:
                failures.append(f"{current}: code={body.get('code')} message={body.get('message')}")
                continue
            normal_files = []
            for item in content:
                name = item.get("name") or ""
                child = posixpath.join(current.rstrip("/"), name)
                if not child.startswith("/"):
                    child = "/" + child
                if item.get("is_dir"):
                    if depth < max_depth:
                        queue.append((child, depth + 1))
                    continue
                if posixpath.splitext(name.lower())[1] in media_exts:
                    print(f"{provider} media file found: {child}")
                    return child
                normal_files.append(child)
            if normal_files:
                print(f"{provider} fallback file found: {normal_files[0]}")
                return normal_files[0]
    raise AssertionError(
        f"{provider}: no real file found after {lists} list requests; " + " | ".join(failures[-8:])
    )

def assert_missing_auth(provider, file_path):
    body = get_path(file_path)
    data = body.get("data") or {}
    assert body.get("code") == 401, body
    assert data.get("byoa_auth_required") is True, body
    assert data.get("provider") == provider, body
    print(f"{provider} missing-auth trigger passed: {file_path}")

ali_mounts = load_mounts("/tmp/ali_paths.txt")
quark_mounts = load_mounts("/tmp/quark_paths.txt")
assert ali_mounts, "AliyunShare: no mount paths"
assert quark_mounts, "QuarkShare: no mount paths"
first_browsable("AliyunShare", ali_mounts)
first_browsable("QuarkShare", quark_mounts)
ali_file = find_media_file("AliyunShare", ali_mounts, max_depth=2, max_lists=16)
quark_file = find_media_file("QuarkShare", quark_mounts, max_depth=5, max_lists=50)
assert_missing_auth("aliyun", ali_file)
assert_missing_auth("quark", quark_file)
PY
}

set_phase first-start
echo '=== First normal start from an empty data volume ==='
start_container if-newer

set_phase first-storage-snapshot
first_snapshot="$(storage_snapshot)"
assert_storage_snapshot "$first_snapshot"

set_phase first-persistent-state
first_key_hash="$(key_hash)"
first_admin_hash="$(admin_hash)"
first_version="$(data_version)"
[[ "$first_version" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]
assert_config_migration_state
echo "Xiaoya data version: ${first_version}"
echo "BYOA key hash (first start): ${first_key_hash}"

set_phase first-guest-catalog
assert_guest_catalog

set_phase first-qr-protocols
assert_qr_protocols

set_phase first-provider-browse-auth
assert_anonymous_provider_browse_and_auth

set_phase restart-start
echo '=== Recreate container with the same persistent data volume ==='
docker rm -f "$CONTAINER" >/dev/null
start_container if-newer

set_phase restart-persistence
second_snapshot="$(storage_snapshot)"
assert_storage_snapshot "$second_snapshot"
second_key_hash="$(key_hash)"
second_admin_hash="$(admin_hash)"
second_version="$(data_version)"
test "$second_snapshot" = "$first_snapshot"
test "$second_key_hash" = "$first_key_hash"
test "$second_admin_hash" = "$first_admin_hash"
test "$second_version" = "$first_version"
assert_config_migration_state

set_phase restart-guest-catalog
assert_guest_catalog

set_phase refresh-mark-old-version
echo '=== Force an old content version and verify safe refresh ==='
sql "create table if not exists byoa_state (key text primary key, value text not null); insert or replace into byoa_state (key,value) values ('xiaoya_data_version','0.0.0');"
docker rm -f "$CONTAINER" >/dev/null

set_phase refresh-start
start_container if-newer

set_phase refresh-persistence
refresh_snapshot="$(storage_snapshot)"
assert_storage_snapshot "$refresh_snapshot"
refresh_key_hash="$(key_hash)"
refresh_admin_hash="$(admin_hash)"
refresh_version="$(data_version)"

[[ "$refresh_version" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]
test "$refresh_version" != "0.0.0"
test "$refresh_key_hash" = "$first_key_hash"
test "$refresh_admin_hash" = "$first_admin_hash"
assert_config_migration_state

set_phase refresh-guest-catalog
assert_guest_catalog

echo "Xiaoya data version after forced refresh: ${refresh_version}"
echo 'BYOA first-start, QR, persistence and content-refresh smoke passed'

set_phase complete
docker rm -f "$CONTAINER" >/dev/null
trap - EXIT
docker volume rm -f "$VOLUME" >/dev/null
