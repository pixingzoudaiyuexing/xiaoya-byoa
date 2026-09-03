#!/bin/sh

# Xiaoya BYOA 数据获取 / 更新：
# - 只使用 Xiaoya 官方公开数据；
# - 不要求服务器保存阿里/夸克私人凭据；
# - 首次启动生成 Xiaoya 丰富目录；
# - 后续仅在数据版本变化时调用 Xiaoya /updateall；
# - 本脚本只负责“取得/更新 data.db”，不再承担驱动转换、版本落库、账号恢复；
# - 所有 BYOA 安全归一化统一交给 /byoa-xiaoya-normalize.sh。
set -eu

DATA_DIR="${BYOA_DATA_DIR:-/opt/alist/data}"
DB_PATH="${DATA_DIR}/data.db"
XIAOYA_DATA_URL="${BYOA_XIAOYA_DATA_URL:-https://raw.githubusercontent.com/xiaoyaDev/data/main}"
UPDATE_MODE="${BYOA_XIAOYA_UPDATE:-if-newer}"
STRICT_MODE="${BYOA_XIAOYA_STRICT:-false}"
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
  log "已备份本地 admin，待归一化阶段恢复"
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

  # Xiaoya 的 QuarkShare 生成依赖公开分享清单。这里只预置公开清单，绝不注入服务端 Cookie。
  if ! curl -fsSL --retry 3 "${XIAOYA_DATA_URL}/quarkshare_list.txt" -o /data/quarkshare_list.txt; then
    warn "下载 quarkshare_list.txt 失败"
    if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
      exit 1
    fi
    warn "跳过本次 Xiaoya 内容更新，继续使用现有数据库"
    update_ready=false
  fi

  # 已有实例更新前只负责备份 admin；恢复由 normalize 脚本统一完成。
  if [ "$update_ready" = true ] && [ -s "$DB_PATH" ]; then
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
      if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
        exit 1
      fi
    else
      # /updateall 是 Xiaoya 自解压运行器。这里只把它当成独立的数据生成器：
      # 它返回后不再做任何版本解析或数据库转换，避免其 shell/trap 行为影响后续逻辑。
      set +e
      ( /updateall )
      update_status=$?
      set -e
      log "Xiaoya /updateall 返回状态：${update_status}"

      if [ "$update_status" -ne 0 ]; then
        warn "Xiaoya /updateall 执行失败：${update_status}"
        if [ "$STRICT_MODE" = true ] || [ ! -s "$DB_PATH" ]; then
          exit 1
        fi
      fi
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
