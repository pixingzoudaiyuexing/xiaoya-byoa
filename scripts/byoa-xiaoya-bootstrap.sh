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
VERSION_FILE="${DATA_DIR}/xiaoya_data.version"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
UPDATE_MODE="${BYOA_XIAOYA_UPDATE:-if-newer}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"
REMOTE_VERSION=""
ADMIN_BACKED_UP=false
UPDATE_BLOCKED="/tmp/byoa-xiaoya-update-blocked.$$"
rm -f "$UPDATE_BLOCKED"

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
        LOCAL_VERSION=""
        if [ -s "$VERSION_FILE" ]; then
          LOCAL_VERSION="$(tr -d '\r\n ' < "$VERSION_FILE")"
        fi
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
      LOCAL_VERSION=""
      if [ -s "$VERSION_FILE" ]; then
        LOCAL_VERSION="$(tr -d '\r\n ' < "$VERSION_FILE")"
      fi
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
    : > "$UPDATE_BLOCKED"
    if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
      exit 1
    fi
    warn "跳过本次 Xiaoya 内容更新，继续使用现有数据库"
    update_ready=false
  fi

  if [ "$update_ready" = true ]; then
    # Xiaoya /updateall 会改写 admin 的兼容 password 字段。已有库更新时先完整备份 admin 行，
    # 更新后恢复，确保内容更新不改变本地管理账号。
    if [ -s "$DB_PATH" ]; then
      if ! backup_admin; then
        : > "$UPDATE_BLOCKED"
        if [ "$STRICT_MODE" = true ]; then
          exit 1
        fi
      fi
    fi

    if [ ! -x /updateall ]; then
      warn "基础镜像缺少 /updateall"
      : > "$UPDATE_BLOCKED"
      restore_admin || true
      if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
        exit 1
      fi
    elif ! /updateall; then
      warn "Xiaoya /updateall 执行失败"
      : > "$UPDATE_BLOCKED"
      restore_admin || true
      if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
        exit 1
      fi
    fi
  fi
fi

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db，继续启动 PowerList 空库"
  rm -f "$UPDATE_BLOCKED"
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

# 版本持久化只依赖最终文件/数据库状态，不再依赖 need_update 这类可能受上游脚本执行细节影响的 shell 状态。
# 如果本轮更新明确失败/跳过，UPDATE_BLOCKED 会阻止写入；正常启动时重复写入同一版本是安全的。
if [ ! -f "$UPDATE_BLOCKED" ]; then
  ACTUAL_VERSION=""
  for candidate in /version.txt /www/data/version.txt /data/version.txt; do
    if [ -s "$candidate" ]; then
      candidate_version="$(tr -d '\r\n ' < "$candidate")"
      if [ -n "$candidate_version" ] && valid_version "$candidate_version"; then
        ACTUAL_VERSION="$candidate_version"
        break
      fi
    fi
  done

  if [ -z "$ACTUAL_VERSION" ] || ! valid_version "$ACTUAL_VERSION"; then
    ACTUAL_VERSION="$REMOTE_VERSION"
  fi
  if { [ -z "$ACTUAL_VERSION" ] || ! valid_version "$ACTUAL_VERSION"; } && [ -s "$VERSION_FILE" ]; then
    ACTUAL_VERSION="$(tr -d '\r\n ' < "$VERSION_FILE")"
  fi
  if [ "$UPDATE_MODE" != "never" ] && { [ -z "$ACTUAL_VERSION" ] || ! valid_version "$ACTUAL_VERSION"; }; then
    ACTUAL_VERSION="$(fetch_remote_version || true)"
  fi

  if [ -n "$ACTUAL_VERSION" ] && valid_version "$ACTUAL_VERSION"; then
    printf '%s\n' "$ACTUAL_VERSION" > "$VERSION_FILE"
    log "已记录 Xiaoya 数据版本：${ACTUAL_VERSION}"
  elif [ "$UPDATE_MODE" != "never" ]; then
    warn "未取得有效 Xiaoya 数据版本；保留现有内容，下次启动继续检查"
  fi
fi

rm -f "$UPDATE_BLOCKED"
