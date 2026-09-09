# Xiaoya BYOA 正式部署说明

> **RELEASE APPROVED**
>
> RealityPanel 固定使用 `ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest`。`main` 是正式稳定代码；推送 `main` 后自动构建并发布 `latest` 与 `sha-<short-sha>` 多架构镜像。

## Production Compose

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

首次部署或更新：

```bash
docker compose pull
docker compose up -d
```

普通更新禁止使用 `docker compose down -v`。必须保留 `/opt/alist/data`，其中包括 `data.db`、`byoa_cookie.key` 和 `xiaoya_data.version`。

## RealityPanel Contract

```text
Image: ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
Endpoint: 127.0.0.1:5244
```

5244 仅 loopback；RealityPanel 不需要随 BYOA 版本修改镜像地址。

## HTTPS Reverse Proxy

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

链路为 `normal HTTPS -> fallback -> Xiaoya BYOA`；Reality core service 与 Xiaoya fallback 故障解耦。公网反代隐藏 `/@manage` 和 `/api/admin*`，但不能阻断 `/api/fs/*`、`/api/public/byoa/*`、`/d/*`、`/p/*`。

## Accepted Risk

BYOA same-path requests may reuse request-derived Link state through OpenList global Link Cache / singleflight，可能造成同一路径访客间播放能力或 Quark 凭据复用。该问题是 **KNOWN SECURITY FINDINGS: ACCEPTED RISK**，仅延期到未来 hardening，不阻塞当前低流量伪装站正式发布；此前 A/B 验收未覆盖 exact same-path cache reuse。

## Pre-production Checklist

正式公网部署前仍需取得以下证据：

```text
[ ] Production Reality E2E
[ ] Real public TLS and browser trust
[ ] Secure Cookie in a real browser
[ ] /@manage and /api/admin* hidden publicly
[ ] Reality traffic unaffected by fallback failure
[ ] Normal HTTPS reaches Xiaoya BYOA fallback
[ ] 5244 remains loopback-only
[ ] Credentials absent from DB, Storage, volume, logs and cache
```

Production Reality E2E 未运行不撤销当前 release approval，但必须在预生产部署验收阶段完成。外部 CVE、DAST、渗透测试以及 Aliyun 非视频/音频能力仍未验证。

功能修复状态：Aliyun 2-minute preview bug **FIXED**；首页 README 500 visual error **FIXED**。Known same-path cross-user Link reuse 仍为 **ACCEPTED RISK / DEFERRED**，本次未修复。

## License

本项目基于 AGPL-3.0 组件。通过网络提供修改后的程序时，应履行对应源代码提供义务。
