#!/usr/bin/env bash
set -euo pipefail

app_name="alist"
frontend_version="${BYOA_FRONTEND_VERSION:-4.2.5}"
frontend_tag="v${frontend_version#v}"
frontend_asset="openlist-frontend-dist-${frontend_tag}.tar.gz"
frontend_url="https://github.com/OpenListTeam/OpenList-Frontend/releases/download/${frontend_tag}/${frontend_asset}"

case "${frontend_version#v}" in
  4.2.5)
    frontend_sha256="78e957e5b8855e30d00767458ab90ec11125a0344a238cc1e1990f46553d4308"
    ;;
  *)
    echo "Unsupported BYOA frontend version: ${frontend_version}. Update the pinned SHA-256 first." >&2
    exit 1
    ;;
esac

built_at="$(date +'%F %T %z')"
git_author="power721"
git_commit="$(git log --pretty=format:'%h' -1)"
version="$(git describe --abbrev=0 --tags 2>/dev/null || echo 'v0.0.0')"

printf 'BYOA backend version: %s\n' "$version"
printf 'BYOA frontend version: %s\n' "$frontend_version"

rm -f /tmp/byoa-frontend.tar.gz
curl -fL \
  --retry 4 \
  --retry-all-errors \
  --retry-delay 2 \
  --connect-timeout 15 \
  --max-time 180 \
  "$frontend_url" \
  -o /tmp/byoa-frontend.tar.gz

echo "${frontend_sha256}  /tmp/byoa-frontend.tar.gz" | sha256sum -c -
rm -rf public/dist
mkdir -p public/dist
tar -xzf /tmp/byoa-frontend.tar.gz -C public/dist
rm -f /tmp/byoa-frontend.tar.gz

test -f public/dist/index.html
mkdir -p bin

ldflags="-w -s \
-X 'github.com/OpenListTeam/OpenList/v4/internal/conf.BuiltAt=${built_at}' \
-X 'github.com/OpenListTeam/OpenList/v4/internal/conf.GitAuthor=${git_author}' \
-X 'github.com/OpenListTeam/OpenList/v4/internal/conf.GitCommit=${git_commit}' \
-X 'github.com/OpenListTeam/OpenList/v4/internal/conf.Version=${version}' \
-X 'github.com/OpenListTeam/OpenList/v4/internal/conf.WebVersion=${frontend_version}'"

go build -o "./bin/${app_name}" -ldflags="$ldflags" -tags=jsoniter .
