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

valid_version() {
  printf '%s' "$1" | grep -Eq '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$'
}

fetch_remote_version() {
  value="$(curl -fsSL --retry 2 --max-time 8 "${XIAOYA_DATA_URL}/version.txt" 2>/dev/null | tr -d '\r\n ' || true)"
  if [ -n "$value" ] && valid_version "$value"; then
    printf '%s' "$value"
    return 0
  fi
  return 1
}

read_local_version() {
  [ -s "$DB_PATH" ] || return 1
  value="$(sqlite3 "$DB_PATH" "SELECT value FROM byoa_state WHERE key='xiaoya_data_version' LIMIT 1;" 2>/dev/null | tr -d '\r\n ' || true)"
  if [ -n "$value" ] && valid_version "$value"; then
    printf '%s' "$value"
    return 0
  fi
  return 1
}

download_public_file() {
  name="$1"
  dest="$2"
  curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 90 \
    "${XIAOYA_DATA_URL}/${name}" -o "$dest"
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

  # 与 Xiaoya stock updater 保持必要的数据兼容处理，但不执行其进程/服务副作用。
  sed -i 's#:120,#:300,#g' "$update_sql"

  fixed_token='alist-09ceb38a-f143-47f7-b255-c3eec819cd7b0lSmqjgBRIMJakAkbJIE2KzO6h2CUVBuEkqrLiA5cJJzOzYxJtCTIGBXXnhrg7Av'
  auth_token="$(ensure_auth_token)"
  escaped_token="$(printf '%s' "$auth_token" | sed 's/[&|]/\\&/g')"
  sed -i "s|${fixed_token}|${escaped_token}|g" "$update_sql"

  # 当前 MVP 不支持 115/UC/PikPak；在 SQL 导入前直接过滤明显的 PikPakShare 行，
  # 其余账号型驱动由 normalize 统一删除。
  sed -i '/PikPakShare/d' "$update_sql"

  cp "$DB_PATH" "$db_backup"
  rm -f "${DB_PATH}-shm" "${DB_PATH}-wal"

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

# 公开 Quark 分享清单持久化到 data volume，同时兼容 Xiaoya 传统 /data 路径。
if download_public_file quarkshare_list.txt "${DATA_DIR}/quarkshare_list.txt"; then
  cp "${DATA_DIR}/quarkshare_list.txt" /data/quarkshare_list.txt
else
  warn "下载 quarkshare_list.txt 失败"
  if [ "$STRICT_MODE" = true ] && [ ! -s "${DATA_DIR}/quarkshare_list.txt" ]; then
    exit 1
  fi
  if [ -s "${DATA_DIR}/quarkshare_list.txt" ]; then
    cp "${DATA_DIR}/quarkshare_list.txt" /data/quarkshare_list.txt
  fi
fi

if ! ensure_seed_db; then
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  warn "无法初始化 Xiaoya seed data.db"
  exit 0
fi

need_update=false
case "$UPDATE_MODE" in
  always)
    need_update=true
    REMOTE_VERSION="$(fetch_remote_version || true)"
    ;;
  if-missing)
    LOCAL_VERSION="$(read_local_version || true)"
    if [ -z "$LOCAL_VERSION" ]; then
      need_update=true
      REMOTE_VERSION="$(fetch_remote_version || true)"
    fi
    ;;
  if-newer)
    REMOTE_VERSION="$(fetch_remote_version || true)"
    if [ -z "$REMOTE_VERSION" ]; then
      warn "无法检查 Xiaoya 远端版本，继续使用本地内容"
    else
      LOCAL_VERSION="$(read_local_version || true)"
      if [ -z "$LOCAL_VERSION" ] || [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
        log "发现 Xiaoya 数据版本变化：${LOCAL_VERSION:-unknown} -> ${REMOTE_VERSION}"
        need_update=true
      else
        log "Xiaoya 数据已是最新版本：${LOCAL_VERSION}"
      fi
    fi
    ;;
  never)
    ;;
  *)
    warn "未知 BYOA_XIAOYA_UPDATE=$UPDATE_MODE，按 if-newer 处理"
    REMOTE_VERSION="$(fetch_remote_version || true)"
    LOCAL_VERSION="$(read_local_version || true)"
    if [ -n "$REMOTE_VERSION" ] && [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
      need_update=true
    fi
    ;;
esac

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
