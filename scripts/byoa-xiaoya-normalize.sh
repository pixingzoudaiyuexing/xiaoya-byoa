#!/bin/sh

# Xiaoya BYOA 数据归一化：
# - 与 stock /updateall 控制流完全解耦；
# - 只保留 MVP 所需的阿里云盘 + 夸克公开分享目录；
# - 把旧 AliyundriveShare 系列统一切换为支持浏览器 BYOA 的 AliyunShare；
# - 删除服务端账号型驱动与历史私人凭据字段；
# - 丢弃带 Xiaoya ALIST_* 占位符的旧 config.json，让 OpenList v4 自行生成兼容配置；
# - 将 Xiaoya 内容版本写入持久化 data.db。
#
# 所有关键失败都显式处理，避免 Alpine / BusyBox shell 因复合命令状态静默退出。

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
CONFIG_PATH="${DATA_DIR}/config.json"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"
VERSION_TMP="/tmp/byoa-xiaoya-version.$$"

log() {
  printf '[BYOA Xiaoya] %s\n' "$*"
}

warn() {
  printf '[BYOA Xiaoya] WARN: %s\n' "$*" >&2
}

# 只接受三段纯数字版本。与 bootstrap 使用相同的纯 shell 校验，
# 避免 Xiaoya/Alpine 运行时 grep 实现差异导致合法版本被误判。
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

sql_scalar() {
  query="$1"
  sqlite3 "$DB_PATH" "$query" 2>/dev/null | tr -d '\r\n '
}

cleanup() {
  rm -f "$VERSION_TMP" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

log "normalize start strict=${STRICT_MODE} db=${DB_PATH}"

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db"
  if [ "$STRICT_MODE" = true ]; then
    exit 1
  fi
  exit 0
fi

# Xiaoya seed 中的 config.json 可能包含未展开的 ALIST_* shell 占位符。
# OpenList v4 使用严格 JSON 解析，遇到这类值会在连接数据库前直接退出。
# BYOA 不依赖 Xiaoya 的运行时配置；只在确认存在 legacy 占位符时删除该文件，
# 让 OpenList v4 在同一持久化 data 目录生成自己的兼容配置。
# 不输出 config 内容，避免 jwt_secret 或其他敏感字段进入日志。
if [ -s "$CONFIG_PATH" ] && grep -Eq 'ALIST_[A-Z0-9_]+' "$CONFIG_PATH"; then
  if rm -f "$CONFIG_PATH"; then
    log "已移除不兼容的 Xiaoya legacy config；OpenList v4 将生成兼容配置"
  else
    warn "无法移除不兼容的 Xiaoya legacy config"
    exit 1
  fi
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

# 版本解析故意不用“函数 + command substitution”。
# 先检查镜像/数据目录现成版本文件，再显式下载官方 version.txt。
log "开始解析 Xiaoya 数据版本"
version=""
version_source=""
for candidate_file in /version.txt /www/data/version.txt /data/version.txt; do
  if [ -s "$candidate_file" ]; then
    candidate="$(tr -d '\r\n ' < "$candidate_file" 2>/dev/null || true)"
    if [ -n "$candidate" ] && valid_version "$candidate"; then
      version="$candidate"
      version_source="$candidate_file"
      break
    fi
  fi
done

if [ -z "$version" ]; then
  rm -f "$VERSION_TMP"
  if curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 90 \
      "${XIAOYA_DATA_URL}/version.txt" -o "$VERSION_TMP"; then
    candidate="$(tr -d '\r\n ' < "$VERSION_TMP" 2>/dev/null || true)"
    if [ -n "$candidate" ] && valid_version "$candidate"; then
      version="$candidate"
      version_source="${XIAOYA_DATA_URL}/version.txt"
    else
      warn "官方 version.txt 内容无效：${candidate:-empty}"
    fi
  else
    warn "下载官方 version.txt 失败"
  fi
fi

log "版本解析完成：value=${version:-empty} source=${version_source:-none}"

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
