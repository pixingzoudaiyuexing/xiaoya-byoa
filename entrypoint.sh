#!/bin/sh

umask "${UMASK:-022}"

if [ "${1:-}" = "version" ]; then
  ./alist version
  exit $?
fi

echo "[BYOA entrypoint] start update=${BYOA_XIAOYA_UPDATE:-if-newer} strict=${BYOA_XIAOYA_STRICT:-false} bootstrap=${BYOA_XIAOYA_BOOTSTRAP:-true} args=$*"

# BYOA Cookie 使用一把服务实例级 AES-256 密钥加密。
# 该密钥只用于解密浏览器自行携带的密文，不保存任何用户 Session 或网盘凭据。
# 默认放在持久化 data volume，容器重启后浏览器无需重新扫码。
if [ -z "${BYOA_COOKIE_KEY:-}" ]; then
  key_file="${BYOA_COOKIE_KEY_FILE:-/opt/alist/data/byoa_cookie.key}"
  key_dir="$(dirname "$key_file")"
  mkdir -p "$key_dir"
  if [ ! -s "$key_file" ]; then
    old_umask="$(umask)"
    umask 077
    if ! dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\r\n ' > "$key_file"; then
      echo "Error: failed to generate BYOA cookie encryption key" >&2
      exit 1
    fi
    umask "$old_umask"
    chmod 600 "$key_file"
    echo "[BYOA entrypoint] generated persistent cookie key"
  else
    echo "[BYOA entrypoint] reusing persistent cookie key"
  fi
  BYOA_COOKIE_KEY="$(tr -d '\r\n ' < "$key_file")"
  export BYOA_COOKIE_KEY
else
  echo "[BYOA entrypoint] using cookie key from environment"
fi

# BYOA Xiaoya 首次启动/更新：只取得公开目录数据，不需要服务器私人 Token。
if [ "${BYOA_XIAOYA_BOOTSTRAP:-true}" = "true" ]; then
  if [ ! -x /byoa-xiaoya-bootstrap.sh ]; then
    echo "Error: /byoa-xiaoya-bootstrap.sh is missing or not executable" >&2
    exit 1
  fi
  echo "[BYOA entrypoint] bootstrap begin"
  /byoa-xiaoya-bootstrap.sh
  bootstrap_status=$?
  echo "[BYOA entrypoint] bootstrap end rc=${bootstrap_status}"
  if [ "$bootstrap_status" -ne 0 ]; then
    if [ -s /opt/alist/data/data.db ]; then
      echo "[BYOA Xiaoya] WARN: bootstrap 返回失败，但 data.db 已生成，继续执行独立归一化" >&2
    else
      echo "Error: BYOA Xiaoya bootstrap failed before data.db was generated" >&2
      exit 1
    fi
  fi
else
  echo "[BYOA entrypoint] bootstrap disabled"
fi

# 阿里驱动转换、夸克保留、旧账号型驱动清理、版本标记全部独立归一化。
if [ ! -x /byoa-xiaoya-normalize.sh ]; then
  echo "Error: /byoa-xiaoya-normalize.sh is missing or not executable" >&2
  exit 1
fi

echo "[BYOA entrypoint] normalize begin"
/byoa-xiaoya-normalize.sh
normalize_status=$?
echo "[BYOA entrypoint] normalize end rc=${normalize_status}"
if [ "$normalize_status" -ne 0 ]; then
  echo "Error: BYOA Xiaoya normalization failed" >&2
  exit 1
fi

# Check file of /opt/alist/data permissions for current user
# 检查当前用户是否有当前目录的写和执行权限
if [ -d ./data ]; then
  if ! [ -w ./data ] || ! [ -x ./data ]; then
cat <<EOF
Error: Current user does not have write and/or execute permissions for the ./data directory: $(pwd)/data
Please visit https://doc.oplist.org/guide/installation/docker#for-version-after-v4-1-0 for more information.
错误：当前用户没有 ./data 目录（$(pwd)/data）的写和/或执行权限。
请访问 https://doc.oplist.org/guide/installation/docker#v4-1-0-%E4%BB%A5%E5%90%8E%E7%89%88%E6%9C%AC 获取更多信息。
Exiting...
EOF
    exit 1
  fi
fi

ARIA2_DIR="/opt/service/start/aria2"
if [ "${RUN_ARIA2:-false}" = "true" ]; then
  if [ ! -d "$ARIA2_DIR" ]; then
    mkdir -p "$ARIA2_DIR"
    cp -r /opt/service/stop/aria2/* "$ARIA2_DIR" 2>/dev/null || true
  fi
  runsvdir /opt/service/start &
else
  if [ -d "$ARIA2_DIR" ]; then
    rm -rf "$ARIA2_DIR"
  fi
fi

# 诊断阶段：强制把 OpenList 日志同时输出到 stdout，并捕获退出码。
# 确认启动根因后恢复 exec 让 OpenList 成为容器 PID 1。
echo "[BYOA entrypoint] server begin (diagnostic log-std)"
./alist server --no-prefix --log-std
server_status=$?
echo "[BYOA entrypoint] server exited rc=${server_status}" >&2

if [ -f /opt/alist/data/log/log.log ]; then
  echo "[BYOA entrypoint] tail OpenList log file" >&2
  tail -n 80 /opt/alist/data/log/log.log >&2 || true
fi

exit "$server_status"
