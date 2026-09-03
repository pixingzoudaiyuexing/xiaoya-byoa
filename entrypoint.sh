#!/bin/sh

umask ${UMASK}

if [ "$1" = "version" ]; then
  ./alist version
else
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
    fi
    BYOA_COOKIE_KEY="$(tr -d '\r\n ' < "$key_file")"
    export BYOA_COOKIE_KEY
  fi

  # BYOA Xiaoya 首次启动：使用官方公开数据生成丰富目录，不要求服务器私人 Token。
  # bootstrap 内部会自行降级处理“已有数据库 + 远端暂时不可用”等可恢复场景；
  # 如果它最终仍返回失败，说明当前数据库没有完成安全归一化，不能继续启动服务。
  if [ "${BYOA_XIAOYA_BOOTSTRAP:-true}" = "true" ] && [ -x /byoa-xiaoya-bootstrap.sh ]; then
    if ! /byoa-xiaoya-bootstrap.sh; then
      echo "Error: BYOA Xiaoya bootstrap failed" >&2
      exit 1
    fi
  fi

  # Check file of /opt/openlist/data permissions for current user
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

  # Define the target directory path for aria2 service
  ARIA2_DIR="/opt/service/start/aria2"
  if [ "$RUN_ARIA2" = "true" ]; then
    # If aria2 should run and target directory doesn't exist, copy it
    if [ ! -d "$ARIA2_DIR" ]; then
      mkdir -p "$ARIA2_DIR"
      cp -r /opt/service/stop/aria2/* "$ARIA2_DIR" 2>/dev/null
    fi
    runsvdir /opt/service/start &
  else
    # If aria2 should NOT run and target directory exists, remove it
    if [ -d "$ARIA2_DIR" ]; then
      rm -rf "$ARIA2_DIR"
    fi
  fi
  exec ./alist server --no-prefix
fi
