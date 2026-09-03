#!/bin/sh

# Xiaoya BYOA 数据归一化：
# - 与 /updateall 控制流完全解耦；
# - 只保留 MVP 所需的阿里云盘 + 夸克公开分享目录；
# - 把旧 AliyundriveShare 系列统一切换为支持浏览器 BYOA 的 AliyunShare；
# - 删除服务端账号型驱动与历史私人凭据字段；
# - 如 bootstrap 在更新期间备份过 admin，则在这里可靠恢复；
# - 将 Xiaoya 内容版本写入持久化 data.db。
set -eu

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"

log() {
  printf '[BYOA Xiaoya] %s\n' "$*"
}

warn() {
  printf '[BYOA Xiaoya] WARN: %s\n' "$*" >&2
}

valid_version() {
  printf '%s' "$1" | grep -Eq '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$'
}

resolve_version() {
  for candidate_file in /version.txt /www/data/version.txt /data/version.txt; do
    if [ -s "$candidate_file" ]; then
      candidate="$(tr -d '\r\n ' < "$candidate_file")"
      if [ -n "$candidate" ] && valid_version "$candidate"; then
        printf '%s' "$candidate"
        return 0
      fi
    fi
  done

  candidate="$(curl -fsSL --retry 2 --max-time 8 "${XIAOYA_DATA_URL}/version.txt" 2>/dev/null | tr -d '\r\n ' || true)"
  if [ -n "$candidate" ] && valid_version "$candidate"; then
    printf '%s' "$candidate"
    return 0
  fi
  return 1
}

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db"
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  exit 0
fi

# 如果 /updateall 期间 bootstrap 已备份 admin，但在后续步骤异常退出，
# 归一化阶段负责兜底恢复，避免更新把本地管理账号覆盖掉。
admin_backup_exists="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='byoa_admin_backup';" 2>/dev/null || echo 0)"
if [ "$admin_backup_exists" = "1" ]; then
  if ! sqlite3 "$DB_PATH" <<'SQL'
DELETE FROM x_users WHERE id = 1;
INSERT INTO x_users SELECT * FROM byoa_admin_backup;
DROP TABLE byoa_admin_backup;
SQL
  then
    warn "恢复本地 admin 账号失败"
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
  else
    log "已恢复本地 admin 账号"
  fi
fi

if ! sqlite3 "$DB_PATH" <<'SQL'
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
WHERE driver = 'AliyunShare' AND json_valid(addition) = 1;

UPDATE x_storages
SET addition = json_remove(addition, '$.cookie')
WHERE driver = 'QuarkShare' AND json_valid(addition) = 1;

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
then
  warn "Xiaoya BYOA 数据归一化失败"
  exit 1
fi

version="$(resolve_version || true)"
if [ -n "$version" ] && valid_version "$version"; then
  if sqlite3 "$DB_PATH" <<SQL
CREATE TABLE IF NOT EXISTS byoa_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR REPLACE INTO byoa_state (key, value)
VALUES ('xiaoya_data_version', '$version');
SQL
  then
    log "已记录 Xiaoya 数据版本：${version}"
  else
    warn "写入 Xiaoya 数据版本失败"
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
  fi
else
  warn "无法取得有效 Xiaoya 数据版本，保留现有版本标记"
  if [ "$STRICT_MODE" = true ]; then
    existing="$(sqlite3 "$DB_PATH" "SELECT value FROM byoa_state WHERE key='xiaoya_data_version' LIMIT 1;" 2>/dev/null | tr -d '\r\n ' || true)"
    if [ -z "$existing" ] || ! valid_version "$existing"; then
      exit 1
    fi
  fi
fi

ali_count="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM x_storages WHERE driver='AliyunShare';")"
quark_count="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM x_storages WHERE driver='QuarkShare';")"
alias_count="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM x_storages WHERE driver='Alias';")"
legacy_count="$(sqlite3 "$DB_PATH" "SELECT count(*) FROM x_storages WHERE driver IN ('AliyundriveShare','AliyundriveShare2Open','AliyundriveShare2Pan115','AliyundriveCron','115 Share','115 Cloud','UCShare','PikPakShare','PikPak');")"

log "目录归一化完成：AliyunShare=${ali_count} QuarkShare=${quark_count} Alias=${alias_count} Legacy=${legacy_count}"

if [ "$STRICT_MODE" = true ]; then
  [ "$ali_count" -gt 0 ]
  [ "$quark_count" -gt 0 ]
  [ "$legacy_count" -eq 0 ]
fi
