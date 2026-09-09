# Xiaoya BYOA

Xiaoya BYOA 面向 Reality fallback 伪装站：访客可匿名浏览公开目录，播放 Aliyun 或 Quark 媒体时由当前浏览器扫码授权。

正式镜像：`ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest`，支持 `linux/amd64` 和 `linux/arm64`。RealityPanel 固定使用 `latest`。

## Docker 部署

### GHCR 正式镜像

创建 `docker-compose.yml`：

```yaml
services:
  xiaoya-byoa:
    image: ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
    pull_policy: always
    container_name: xiaoya-byoa
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      BYOA_XIAOYA_BOOTSTRAP: "true"
      BYOA_XIAOYA_UPDATE: if-newer
      BYOA_XIAOYA_STRICT: "false"
    ports:
      - "127.0.0.1:5244:5244"
    volumes:
      - xiaoya_byoa_data:/opt/alist/data

volumes:
  xiaoya_byoa_data:
```

启动并检查：

```bash
docker compose pull
docker compose up -d
curl -fsS http://127.0.0.1:5244/ping
```

返回 `pong` 表示容器已响应。5244 只绑定本机，公网访问必须经过 HTTPS fallback 或反向代理。

### 使用仓库内的正式 Compose

```bash
git clone https://github.com/pixingzoudaiyuexing/xiaoya-byoa.git
cd xiaoya-byoa
docker compose -f docker-compose.byoa.release.yml pull
docker compose -f docker-compose.byoa.release.yml up -d
```

### 本地源码构建

```bash
git clone https://github.com/pixingzoudaiyuexing/xiaoya-byoa.git
cd xiaoya-byoa
docker compose -f docker-compose.byoa.yml up -d --build
```

本地构建使用 `Dockerfile.byoa`；正式部署优先使用 GHCR 镜像。

## 更新与持久化

普通更新：

```bash
docker compose pull
docker compose up -d
```

必须保留 `xiaoya_byoa_data` 数据卷。`/opt/alist/data` 包含 `data.db`、`byoa_cookie.key` 和 `xiaoya_data.version`。普通更新不要执行 `docker compose down -v`，否则会删除卷和实例状态。

## HTTPS 反向代理

生产必须使用真实公有 TLS，并传递 `X-Forwarded-Proto=https`：

```nginx
location / {
    proxy_pass http://127.0.0.1:5244;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
}
```

请求链路：

```text
normal HTTPS -> fallback / reverse proxy -> Xiaoya BYOA 127.0.0.1:5244
Reality handshake -> Reality core service
```

公网反代应隐藏 `/@manage` 和 `/api/admin*`，但不能阻断 `/api/fs/*`、`/api/public/byoa/*`、`/d/*` 或 `/p/*`。Reality core 与 Xiaoya fallback 应保持故障解耦。

## 支持范围与限制

- Aliyun：已验证视频预览播放；`get_share_link_download_url` 返回 HTTP 410 时回退到视频预览接口。非视频原文件下载和音频未验证。
- Quark：使用当前浏览器扫码凭据进行播放。
- BYOA same-path Link Cache / singleflight 可能导致同路径跨用户 Link 状态复用，这是当前低流量伪装站用途下的 **KNOWN SECURITY FINDINGS: ACCEPTED RISK**，已延期到后续 hardening。
- 不要在日志、Issue、PR 或配置中提交 Token、Cookie、Authorization 或 admin 密码。

## 文档

- [正式部署说明](docs/BYOA_DEPLOY.md)
- [MVP / Release 状态](docs/BYOA_MVP_STATUS.md)
- [最终交接](docs/CODEX_HANDOFF.md)
- [许可证](LICENSE)

本项目基于 AGPL-3.0 组件。通过网络提供修改后的程序时，应履行对应源代码提供义务。
