#!/bin/sh

# Xiaoya BYOA 数据初始化 / 更新：
# - 只使用 xiaoyaDev/data 官方公开数据；
# - 不执行 stock /updateall，避免其自解压运行器/进程控制影响容器 PID 1；
# - 首次启动从 Xiaoya 基础镜像自带 /var/lib/data.zip 取得兼容 seed data.db；
# - 直接导入官方 update.zip，并维护 Xiaoya index.zip；
# - 不要求服务器保存阿里/夸克私人凭据；
# - 驱动转换、私人字段清理、版本落库统一交给 /byoa-xiaoya-normalize.sh。
set -eu

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
XIAOYA_VERSION_API_URL="${BYOA_XIAOYA_VERSION_API_URL:-https://api.github.com/repos/xiaoyaDev/data/contents/version.txt?ref=main}"
UPDATE_MODE="${BYOA_XIAOYA_UPDATE:-if-newer}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"
WORK_DIR="/tmp/byoa-xiaoya"
SEED_ARCHIVE="${BYOA_XIAOYA_SEED_ARCHIVE:-/var/lib/data.zip}"
AUTH_TOKEN_FILE="${DATA_DIR}/byoa_alist_auth_token.txt"
REMOTE_VERSION=""

log() {
  printf '[BYOA Xiaoya] %s\n' "$*"
}

warn() {
  printf '[BYOA Xiaoya] WARN: %s\n' "$*" >&2
}

# 只接受三段纯数字版本。避免依赖不同 BusyBox/grep 版本的 ERE 行为。
valid_version() {
  version_value="$1"
  old_ifs="$IFS"
  IFS='.'
  set -- $version_value
  IFS="$old_ifs"
  [ "$#" -eq 3 ] || return 1
  for version_part in "$@"; do
    case "$version_part" in
      ''|*[!0-9]*) return 1 ;;
    esac
  done
  return 0
}

download_public_file() {
  name="$1"
  dest="$2"
  curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 90 \
    "${XIAOYA_DATA_URL}/${name}" -o "$dest"
}

fetch_remote_version() {
  version_file="${WORK_DIR}/version.txt"
  rm -f "$version_file"

  # 第一来源：xiaoyaDev/data 官方 raw 文件。
  if download_public_file version.txt "$version_file"; then
    value="$(tr -d '\r\n ' < "$version_file" 2>/dev/null || true)"
    if [ -n "$value" ] && valid_version "$value"; then
      printf '%s' "$value"
      return 0
    fi
    warn "Xiaoya raw version.txt 内容无效：${value:-empty}"
  else
    warn "下载 Xiaoya raw version.txt 失败，尝试 GitHub Contents API"
  fi

  # 第二来源仍然是同一官方 GitHub 仓库；只切换 GitHub 的传输入口。
  rm -f "$version_file"
  if curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 90 \
      -H 'Accept: application/vnd.github.raw+json' \
      -H 'User-Agent: xiaoya-byoa' \
      "$XIAOYA_VERSION_API_URL" -o "$version_file"; then
    value="$(tr -d '\r\n ' < "$version_file" 2>/dev/null || true)"
    if [ -n "$value" ] && valid_version "$value"; then
      printf '%s' "$value"
      return 0
    fi
    warn "Xiaoya API version.txt 内容无效：${value:-empty}"
  else
    warn "通过 GitHub Contents API 下载 Xiaoya version.txt 失败"
  fi

  return 1
}

read_local_version() {
  [ -s "$DB_PATH" ] || return 1
  table_exists="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='byoa_state';" 2>/dev/null | tr -d '\r\n ' || true)"
  [ "$table_exists" = "1" ] || return 1
  value="$(sqlite3 "$DB_PATH" "SELECT value FROM byoa_state WHERE key='xiaoya_data_version' LIMIT 1;" 2>/dev/null | tr -d '\r\n ' || true)"
  if [ -n "$value" ] && valid_version "$value"; then
    printf '%s' "$value"
    return 0
  fi
  return 1
}

ensure_seed_db() {
  [ -s "$DB_PATH" ] && return 0

  if [ ! -s "$SEED_ARCHIVE" ]; then
    warn "Xiaoya 基础镜像缺少 seed archive：${SEED_ARCHIVE}"
    return 1
  fi

  seed_dir="${WORK_DIR}/seed"
  rm -rf "$seed_dir"
  mkdir -p "$seed_dir"
  if ! unzip -q "$SEED_ARCHIVE" -d "$seed_dir"; then
    warn "解压 Xiaoya seed archive 失败"
    return 1
  fi

  seed_db="$(find "$seed_dir" -type f -name data.db | head -n 1 || true)"
  if [ -z "$seed_db" ] || [ ! -s "$seed_db" ]; then
    warn "Xiaoya seed archive 中未找到 data.db"
    return 1
  fi

  cp "$seed_db" "$DB_PATH"

  if [ ! -s "${DATA_DIR}/config.json" ]; then
    seed_config="$(find "$seed_dir" -type f -name config.json | head -n 1 || true)"
    if [ -n "$seed_config" ] && [ -s "$seed_config" ]; then
      cp "$seed_config" "${DATA_DIR}/config.json"
    fi
  fi

  log "已从 Xiaoya 基础镜像初始化 seed data.db"
  return 0
}

ensure_auth_token() {
  if [ -s "$AUTH_TOKEN_FILE" ]; then
    token="$(tr -d '\r\n ' < "$AUTH_TOKEN_FILE")"
    if [ -n "$token" ]; then
      printf '%s' "$token"
      return 0
    fi
  fi

  random_tail="$(head -c 64 /dev/urandom | sha256sum | awk '{print $1}')"
  token="alist-09ceb38a-f143-47f7-b255-c3eec819cd7b${random_tail}"
  old_umask="$(umask)"
  umask 077
  printf '%s\n' "$token" > "$AUTH_TOKEN_FILE"
  umask "$old_umask"
  chmod 600 "$AUTH_TOKEN_FILE"
  printf '%s' "$token"
}

refresh_index() {
  index_zip="${WORK_DIR}/index.zip"
  index_dir="${WORK_DIR}/index"

  log "开始下载 Xiaoya index.zip"
  if ! download_public_file index.zip "$index_zip"; then
    warn "下载 index.zip 失败"
    return 1
  fi

  rm -rf "$index_dir"
  mkdir -p "$index_dir" /index
  if ! unzip -o -q -P abcd "$index_zip" -d "$index_dir"; then
    warn "解压 index.zip 失败"
    return 1
  fi

  if [ -f "${index_dir}/index.video.txt" ] && [ -s "${DATA_DIR}/quarkshare_list.txt" ] && [ -f "${index_dir}/index.quark.txt" ]; then
    cat "${index_dir}/index.quark.txt" >> "${index_dir}/index.video.txt"
  fi

  : > "${index_dir}/index.txt"
  for part in index.video.txt index.book.txt index.music.txt index.non.video.txt; do
    if [ -f "${index_dir}/${part}" ]; then
      cat "${index_dir}/${part}" >> "${index_dir}/index.txt"
    fi
  done

  cp "${index_dir}"/index*.txt /index/ 2>/dev/null || true
  if [ -n "$REMOTE_VERSION" ] && valid_version "$REMOTE_VERSION"; then
    printf '%s\n' "$REMOTE_VERSION" > /version.txt
  fi
  log "Xiaoya 搜索索引已更新"
}

apply_public_update() {
  update_zip="${WORK_DIR}/update.zip"
  update_dir="${WORK_DIR}/update"
  db_backup="${WORK_DIR}/data.db.before-update"

  log "开始下载 Xiaoya update.zip"
  if ! download_public_file update.zip "$update_zip"; then
    warn "下载 update.zip 失败"
    return 1
  fi

  rm -rf "$update_dir"
  mkdir -p "$update_dir"
  if ! unzip -o -q -P abcd "$update_zip" -d "$update_dir"; then
    warn "解压 update.zip 失败"
    return 1
  fi

  update_sql="$(find "$update_dir" -type f -name update.sql | head -n 1 || true)"
  if [ -z "$update_sql" ] || [ ! -s "$update_sql" ]; then
    warn "update.zip 中未找到 update.sql"
    return 1
  fi

  sed -i 's#:120,#:300,#g' "$update_sql"

  fixed_token='alist-09ceb38a-f143-47f7-b255-c3eec819cd7b0lSmqjgBRIMJakAkbJIE2KzO6h2CUVBuEkqrLiA5cJJzOzYxJtCTIGBXXnhrg7Av'
  auth_token="$(ensure_auth_token)"
  escaped_token="$(printf '%s' "$auth_token" | sed 's/[&|]/\\&/g')"
  sed -i "s|${fixed_token}|${escaped_token}|g" "$update_sql"
  sed -i '/PikPakShare/d' "$update_sql"

  cp "$DB_PATH" "$db_backup"
  rm -f "${DB_PATH}-shm" "${DB_PATH}-wal"

  log "开始导入 Xiaoya update.sql"
  set +e
  sqlite3 "$DB_PATH" <<SQL
DROP TABLE IF EXISTS x_storages;
DROP TABLE IF EXISTS x_meta;
DROP TABLE IF EXISTS x_setting_items;
.read $update_sql
SQL
  update_status=$?
  set -e

  if [ "$update_status" -ne 0 ]; then
    warn "导入 Xiaoya update.sql 失败：${update_status}，恢复更新前数据库"
    cp "$db_backup" "$DB_PATH"
    rm -f "${DB_PATH}-shm" "${DB_PATH}-wal"
    return 1
  fi

  rm -f "$db_backup"
  entries="$(sqlite3 "$DB_PATH" 'SELECT count(*) FROM x_storages;' 2>/dev/null || echo 0)"
  log "Xiaoya 官方目录已导入：${entries} records"
  return 0
}

mkdir -p "$DATA_DIR" /data /www/data "$WORK_DIR"
log "bootstrap start mode=${UPDATE_MODE} strict=${STRICT_MODE} db=${DB_PATH}"

log "同步 Quark 公开分享清单"
if download_public_file quarkshare_list.txt "${DATA_DIR}/quarkshare_list.txt"; then
  cp "${DATA_DIR}/quarkshare_list.txt" /data/quarkshare_list.txt
  log "Quark 公开分享清单已就绪"
else
  warn "下载 quarkshare_list.txt 失败"
  if [ "$STRICT_MODE" = true ] && [ ! -s "${DATA_DIR}/quarkshare_list.txt" ]; then
    exit 1
  fi
  if [ -s "${DATA_DIR}/quarkshare_list.txt" ]; then
    cp "${DATA_DIR}/quarkshare_list.txt" /data/quarkshare_list.txt
  fi
fi

log "检查/初始化 seed data.db"
if ! ensure_seed_db; then
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  warn "无法初始化 Xiaoya seed data.db"
  exit 0
fi
log "seed data.db 检查完成"

need_update=false
log "计算内容更新策略 mode=${UPDATE_MODE}"
case "$UPDATE_MODE" in
  always)
    need_update=true
    REMOTE_VERSION="$(fetch_remote_version || true)"
    log "远端 Xiaoya 数据版本：${REMOTE_VERSION:-unavailable}"
    ;;
  if-missing)
    LOCAL_VERSION="$(read_local_version || true)"
    log "本地 Xiaoya 数据版本：${LOCAL_VERSION:-missing}"
    if [ -z "$LOCAL_VERSION" ]; then
      need_update=true
      REMOTE_VERSION="$(fetch_remote_version || true)"
      log "远端 Xiaoya 数据版本：${REMOTE_VERSION:-unavailable}"
    fi
    ;;
  if-newer)
    REMOTE_VERSION="$(fetch_remote_version || true)"
    log "远端 Xiaoya 数据版本：${REMOTE_VERSION:-unavailable}"
    if [ -z "$REMOTE_VERSION" ]; then
      warn "无法检查 Xiaoya 远端版本"
      if [ "$STRICT_MODE" = true ]; then
        exit 1
      fi
      warn "继续使用本地内容"
    else
      LOCAL_VERSION="$(read_local_version || true)"
      log "本地 Xiaoya 数据版本：${LOCAL_VERSION:-missing}"
      if [ -z "$LOCAL_VERSION" ] || [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
        log "发现 Xiaoya 数据版本变化：${LOCAL_VERSION:-unknown} -> ${REMOTE_VERSION}"
        need_update=true
      else
        log "Xiaoya 数据已是最新版本：${LOCAL_VERSION}"
      fi
    fi
    ;;
  never)
    log "已配置为跳过 Xiaoya 内容更新"
    ;;
  *)
    warn "未知 BYOA_XIAOYA_UPDATE=$UPDATE_MODE，按 if-newer 处理"
    REMOTE_VERSION="$(fetch_remote_version || true)"
    LOCAL_VERSION="$(read_local_version || true)"
    log "版本比较 local=${LOCAL_VERSION:-missing} remote=${REMOTE_VERSION:-unavailable}"
    if [ -n "$REMOTE_VERSION" ] && [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
      need_update=true
    fi
    ;;
esac

log "内容更新决策 need_update=${need_update}"
if [ "$need_update" = true ]; then
  log "准备无私人 Token 的 Xiaoya 官方数据更新"
  if ! apply_public_update; then
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
    warn "Xiaoya 数据更新失败，继续使用现有数据库"
  else
    if ! refresh_index; then
      if [ "$STRICT_MODE" = true ]; then
        exit 1
      fi
      warn "Xiaoya 数据库已更新，但搜索索引刷新失败"
    fi
  fi
fi

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db"
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  exit 0
fi

log "Xiaoya data.db 已就绪，交给独立 BYOA 归一化阶段"
exit 0
