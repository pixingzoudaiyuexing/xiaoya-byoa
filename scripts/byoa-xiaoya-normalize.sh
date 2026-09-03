#!/bin/sh

# Xiaoya BYOA 数据归一化：
# - 与 stock /updateall 控制流完全解耦；
# - 只保留 MVP 所需的阿里云盘 + 夸克公开分享目录；
# - 把旧 AliyundriveShare 系列统一切换为支持浏览器 BYOA 的 AliyunShare；
# - 删除服务端账号型驱动与历史私人凭据字段；
# - 将 Xiaoya 内容版本写入持久化 data.db。
#
# 不使用 set -e：SQLite/网络失败全部显式捕获，避免首启时无日志退出。
set -u

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
  printf '%s' "$1" | grep -Eq '^[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*$'
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

  candidate="$(curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 30 "${XIAOYA_DATA_URL}/version.txt" 2>/dev/null | tr -d '\r\n ' || true)"
  if [ -n "$candidate" ] && valid_version "$candidate"; then
    printf '%s' "$candidate"
    return 0
  fi
  return 1
}

sql_scalar() {
  query="$1"
  sqlite3 "$DB_PATH" "$query" 2>/dev/null | tr -d '\r\n '
}

log "normalize start strict=${STRICT_MODE} db=${DB_PATH}"

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db"
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  exit 0
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
  warn "运行时缺少 sqlite3，无法归一化 Xiaoya 数据库"
  exit 1
fi

log "检查 Xiaoya 数据库基础表"
for required_table in x_storages x_users; do
  table_exists="$(sql_scalar "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='${required_table}';" || true)"
  if [ "$table_exists" != "1" ]; then
    warn "Xiaoya seed data.db 缺少必要表：${required_table}"
    exit 1
  fi
done
log "数据库基础表检查通过"

# 如更新流程留下 admin 备份，先恢复。
admin_backup_exists="$(sql_scalar "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='byoa_admin_backup';" || true)"
if [ "$admin_backup_exists" = "1" ]; then
  log "检测到 admin 备份，开始恢复"
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

log "开始转换 BYOA 存储驱动并清理私人凭据字段"
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
  warn "Xiaoya BYOA 数据归一化 SQL 执行失败"
  exit 1
fi
log "BYOA 存储驱动归一化 SQL 已完成"

version="$(resolve_version || true)"
if [ -n "$version" ] && valid_version "$version"; then
  log "解析到 Xiaoya 数据版本：${version}"
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
    state_table_exists="$(sql_scalar "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='byoa_state';" || true)"
    existing=""
    if [ "$state_table_exists" = "1" ]; then
      existing="$(sql_scalar "SELECT value FROM byoa_state WHERE key='xiaoya_data_version' LIMIT 1;" || true)"
    fi
    if [ -z "$existing" ] || ! valid_version "$existing"; then
      warn "严格模式下没有可用的 Xiaoya 数据版本标记"
      exit 1
    fi
  fi
fi

ali_count="$(sql_scalar "SELECT count(*) FROM x_storages WHERE driver='AliyunShare';" || true)"
quark_count="$(sql_scalar "SELECT count(*) FROM x_storages WHERE driver='QuarkShare';" || true)"
alias_count="$(sql_scalar "SELECT count(*) FROM x_storages WHERE driver='Alias';" || true)"
legacy_count="$(sql_scalar "SELECT count(*) FROM x_storages WHERE driver IN ('AliyundriveShare','AliyundriveShare2Open','AliyundriveShare2Pan115','AliyundriveCron','115 Share','115 Cloud','UCShare','PikPakShare','PikPak');" || true)"

for count_name in ali_count quark_count alias_count legacy_count; do
  eval "count_value=\${${count_name}}"
  case "$count_value" in
    ''|*[!0-9]*)
      warn "读取存储统计失败：${count_name}=${count_value:-empty}"
      exit 1
      ;;
  esac
done

log "目录归一化完成：AliyunShare=${ali_count} QuarkShare=${quark_count} Alias=${alias_count} Legacy=${legacy_count}"

if [ "$STRICT_MODE" = true ]; then
  if [ "$ali_count" -le 0 ]; then
    warn "严格模式失败：没有 AliyunShare 目录"
    exit 1
  fi
  if [ "$quark_count" -le 0 ]; then
    warn "严格模式失败：没有 QuarkShare 目录"
    exit 1
  fi
  if [ "$legacy_count" -ne 0 ]; then
    warn "严格模式失败：仍存在 ${legacy_count} 个旧账号型驱动"
    exit 1
  fi
fi

log "normalize success"
exit 0
