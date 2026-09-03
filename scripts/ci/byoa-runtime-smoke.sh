#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-xiaoya-byoa:ci}"
PORT="${BYOA_SMOKE_PORT:-15244}"
VOLUME="xiaoya-byoa-ci-data-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-0}"
CONTAINER="xiaoya-byoa-ci"
BASE_URL="http://127.0.0.1:${PORT}"

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
  docker run -d \
    --name "$CONTAINER" \
    -e BYOA_XIAOYA_STRICT=true \
    -e BYOA_XIAOYA_UPDATE=if-missing \
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

echo '=== First normal start from an empty data volume ==='
start_container
first_snapshot="$(storage_snapshot)"
assert_storage_snapshot "$first_snapshot"
first_key_hash="$(key_hash)"
echo "BYOA key hash (first start): ${first_key_hash}"
assert_guest_catalog
assert_anonymous_provider_browse_and_auth

echo '=== Recreate container with the same persistent data volume ==='
docker rm -f "$CONTAINER" >/dev/null
start_container
second_snapshot="$(storage_snapshot)"
assert_storage_snapshot "$second_snapshot"
second_key_hash="$(key_hash)"
echo "BYOA key hash (restart): ${second_key_hash}"

test "$second_snapshot" = "$first_snapshot"
test "$second_key_hash" = "$first_key_hash"
assert_guest_catalog

echo 'BYOA first-start and persistence smoke passed'

docker rm -f "$CONTAINER" >/dev/null
trap - EXIT
docker volume rm -f "$VOLUME" >/dev/null
