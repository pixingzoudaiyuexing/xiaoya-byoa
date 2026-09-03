#!/bin/sh

# Xiaoya BYOA 数据初始化：
# - 只使用 Xiaoya 官方公开数据；
# - 不要求服务器保存阿里/夸克私人凭据；
# - 首次启动生成 Xiaoya 丰富目录，之后默认复用持久化 data.db；
# - 将旧 AliyundriveShare 统一切到支持 BYOA 的 AliyunShare；
# - 当前 MVP 仅保留阿里 + 夸克，清理依赖服务端账号的 115/UC/PikPak 存储。
set -eu

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
UPDATE_MODE="${BYOA_XIAOYA_UPDATE:-if-missing}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"

log() {
  printf '[BYOA Xiaoya] %s\n' "$*"
}

warn() {
  printf '[BYOA Xiaoya] WARN: %s\n' "$*" >&2
}

mkdir -p "$DATA_DIR" /data /www/data

need_update=false
case "$UPDATE_MODE" in
  always)
    need_update=true
    ;;
  if-missing)
    if [ ! -s "$DB_PATH" ]; then
      need_update=true
    fi
    ;;
  never)
    ;;
  *)
    warn "未知 BYOA_XIAOYA_UPDATE=$UPDATE_MODE，按 if-missing 处理"
    if [ ! -s "$DB_PATH" ]; then
      need_update=true
    fi
    ;;
esac

if [ "$need_update" = true ]; then
  log "准备无私人 Token 的 Xiaoya 数据初始化"

  # QuarkShare 不需要服务器 Cookie 浏览公开分享；只需要官方公开分享清单生成挂载。
  if ! curl -fsSL --retry 3 "${XIAOYA_DATA_URL}/quarkshare_list.txt" -o /data/quarkshare_list.txt; then
    warn "下载 quarkshare_list.txt 失败"
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
  fi

  if [ ! -x /updateall ]; then
    warn "基础镜像缺少 /updateall"
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
  elif ! /updateall; then
    warn "Xiaoya /updateall 执行失败"
    if [ "$STRICT_MODE" = true ]; then
      exit 1
    fi
  fi
fi

if [ ! -s "$DB_PATH" ]; then
  warn "尚未生成 Xiaoya data.db，继续启动 PowerList 空库"
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

ali_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='AliyunShare';")"
quark_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='QuarkShare';")"
alias_count="$(sqlite3 "$DB_PATH" "select count(*) from x_storages where driver='Alias';")"

log "目录初始化完成：AliyunShare=${ali_count} QuarkShare=${quark_count} Alias=${alias_count}"

if [ "$STRICT_MODE" = true ]; then
  [ "$ali_count" -gt 0 ]
  [ "$quark_count" -gt 0 ]
fi
