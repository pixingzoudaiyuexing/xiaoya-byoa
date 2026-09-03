#!/bin/sh

# Xiaoya BYOA 数据初始化 / 更新：
# - 只使用 Xiaoya 官方公开数据；
# - 不要求服务器保存阿里/夸克私人凭据；
# - 首次启动生成 Xiaoya 丰富目录；
# - 后续启动只在 Xiaoya 数据版本变化时更新，远端不可用则继续使用本地库；
# - 将旧 AliyundriveShare 统一切到支持 BYOA 的 AliyunShare；
# - 当前 MVP 仅保留阿里 + 夸克，清理依赖服务端账号的 115/UC/PikPak 存储。
set -eu

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
UPDATE_MODE="${BYOA_XIAOYA_UPDATE:-if-newer}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"
REMOTE_VERSION=""
ADMIN_BACKED_UP=false
UPDATE_MARKER="${DATA_DIR}/.byoa_xiaoya_update"
rm -f "$UPDATE_MARKER"

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

resolve_updated_version() {
  candidate_version="$REMOTE_VERSION"

  if [ -z "$candidate_version" ] || ! valid_version "$candidate_version"; then
    for candidate_file in /version.txt /www/data/version.txt /data/version.txt; do
      if [ -s "$candidate_file" ]; then
        file_version="$(tr -d '\r\n ' < "$candidate_file")"
        if [ -n "$file_version" ] && valid_version "$file_version"; then
          candidate_version="$file_version"
          break
        fi
      fi
    done
  fi

  if [ -z "$candidate_version" ] || ! valid_version "$candidate_version"; then
    candidate_version="$(fetch_remote_version || true)"
  fi

  if [ -n "$candidate_version" ] && valid_version "$candidate_version"; then
    printf '%s' "$candidate_version"
    return 0
  fi
  return 1
}

store_local_version() {
  version="$1"
  valid_version "$version" || return 1
  sqlite3 "$DB_PATH" <<SQL
CREATE TABLE IF NOT EXISTS byoa_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR REPLACE INTO byoa_state (key, value)
VALUES ('xiaoya_data_version', '$version');
SQL
}

backup_admin() {
  [ -s "$DB_PATH" ] || return 0
  if ! sqlite3 "$DB_PATH" <<'SQL'
DROP TABLE IF EXISTS byoa_admin_backup;
CREATE TABLE byoa_admin_backup AS SELECT * FROM x_users WHERE 0;
INSERT INTO byoa_admin_backup SELECT * FROM x_users WHERE id = 1;
SQL
  then
    warn "备份本地 admin 账号失败"
    return 1
  fi
  ADMIN_BACKED_UP=true
}

restore_admin() {
  [ "$ADMIN_BACKED_UP" = true ] || return 0
  if ! sqlite3 "$DB_PATH" <<'SQL'
DELETE FROM x_users WHERE id = 1;
INSERT INTO x_users SELECT * FROM byoa_admin_backup;
DROP TABLE byoa_admin_backup;
SQL
  then
    warn "恢复本地 admin 账号失败"
    return 1
  fi
  ADMIN_BACKED_UP=false
}

mkdir -p "$DATA_DIR" /data /www/data

need_update=false
case "$UPDATE_MODE" in
  always)
    need_update=true
    REMOTE_VERSION="$(fetch_remote_version || true)"
    ;;
  if-missing)
    if [ ! -s "$DB_PATH" ]; then
      need_update=true
      REMOTE_VERSION="$(fetch_remote_version || true)"
    fi
    ;;
  if-newer)
    if [ ! -s "$DB_PATH" ]; then
      need_update=true
      REMOTE_VERSION="$(fetch_remote_version || true)"
    else
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
    fi
    ;;
  never)
    ;;
  *)
    warn "未知 BYOA_XIAOYA_UPDATE=$UPDATE_MODE，按 if-newer 处理"
    if [ ! -s "$DB_PATH" ]; then
      need_update=true
      REMOTE_VERSION="$(fetch_remote_version || true)"
    else
      REMOTE_VERSION="$(fetch_remote_version || true)"
      LOCAL_VERSION="$(read_local_version || true)"
      if [ -n "$REMOTE_VERSION" ] && [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
        need_update=true
      fi
    fi
    ;;
esac

if [ "$need_update" = true ]; then
  log "准备无私人 Token 的 Xiaoya 数据初始化/更新"
  update_ready=true

  # QuarkShare 不需要服务器 Cookie 浏览公开分享；只需要官方公开分享清单生成挂载。
  # 已有库更新时若这个清单暂时下载失败，宁可继续保留旧库，也不要运行 updateall 后丢失 QuarkShare。
  if ! curl -fsSL --retry 3 "${XIAOYA_DATA_URL}/quarkshare_list.txt" -o /data/quarkshare_list.txt; then
    warn "下载 quarkshare_list.txt 失败"
    if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
      exit 1
    fi
    warn "跳过本次 Xiaoya 内容更新，继续使用现有数据库"
    update_ready=false
  fi

  if [ "$update_ready" = true ] && [ -s "$DB_PATH" ]; then
    # Xiaoya /updateall 会改写 admin 的兼容 password 字段。已有库更新时先完整备份 admin 行。
    # 如果备份失败，非严格模式也跳过本次更新，宁可保留旧内容，不冒险改坏本地管理账号。
    if ! backup_admin; then
      if [ "$STRICT_MODE" = true ]; then
        exit 1
      fi
      warn "跳过本次 Xiaoya 内容更新，继续使用现有数据库"
      update_ready=false
    fi
  fi

  if [ "$update_ready" = true ]; then
    if [ ! -x /updateall ]; then
      warn "基础镜像缺少 /updateall"
      restore_admin || true
      rm -f "$UPDATE_MARKER"
      if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
        exit 1
      fi
    else
      # 标记放在持久化数据目录，而不是 /tmp；Xiaoya /updateall 会清理临时目录。
      # /updateall 成功时标记自然保留；失败路径明确删除。
      : > "$UPDATE_MARKER"
      if ! /updateall; then
        warn "Xiaoya /updateall 执行失败"
        restore_admin || true
        rm -f "$UPDATE_MARKER"
        if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
          exit 1
        fi
      fi
    fi
  fi
fi

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db，继续启动 PowerList 空库"
  rm -f "$UPDATE_MARKER"
  exit 0
fi

# Xiaoya update.sql 当前仍使用旧 AliyundriveShare；PowerList 的 AliyunShare
# 可匿名读取公开分享目录，并在 Link 阶段使用当前浏览器 BYOA Token。
# addition 中历史账号字段对新 Driver 无意义，顺便删除，避免误把服务端凭据设计带回来。
sqlite3 "$DB_PATH" <<'SQL'
UPDATE x_storages
SET driver = 'AliyunShare'
WHERE driver IN ('AliyundriveShare', 'AliyundriveShare2Open', 'AliyundriveShare2Pan115');

UPDATE x_storages
SET addition = json_remove(
  addition,
  '$.refresh_token',
  '$.RefreshToken',
  '$.RefreshTokenOpen',
  '$.TempTransferFolderID',
  '$.oauth_token_url',
  '$.client_id',
  '$.client_secret'
)
WHERE driver = 'AliyunShare';

UPDATE x_storages
SET addition = json_remove(addition, '$.cookie')
WHERE driver = 'QuarkShare';

DELETE FROM x_storages
WHERE driver IN (
  'AliyundriveCron',
  '115 Share',
  '115 Cloud',
  'UCShare',
  'PikPakShare',
  'PikPak'
);

UPDATE x_users
SET disabled = 0,
    permission = 368
WHERE id = 2;
SQL

# 已有实例更新完成后恢复 admin；首次初始化没有备份，不执行此步骤。
restore_admin

ali_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='AliyunShare';")"
quark_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='QuarkShare';")"
alias_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='Alias';")"

log "目录初始化完成：AliyunShare=${ali_count} QuarkShare=${quark_count} Alias=${alias_count}"

if [ "$STRICT_MODE" = true ]; then
  [ "$ali_count" -gt 0 ]
  [ "$quark_count" -gt 0 ]
fi

# 内容数据库本身就是持久状态，因此把 Xiaoya 数据版本放进独立 byoa_state 表。
# 只有 /updateall 未失败、且后续 BYOA 数据归一化/数量检查走到这里时，才提交版本。
if [ -f "$UPDATE_MARKER" ]; then
  committed_version="$(resolve_updated_version || true)"
  if [ -n "$committed_version" ] && valid_version "$committed_version"; then
    store_local_version "$committed_version"
    log "已记录 Xiaoya 数据版本：${committed_version}"
  else
    warn "Xiaoya 更新成功，但未取得有效数据版本；下次启动会重新检查"
  fi
fi

rm -f "$UPDATE_MARKER"
