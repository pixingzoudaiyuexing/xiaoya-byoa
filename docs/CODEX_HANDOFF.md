# Xiaoya BYOA MVP Final Acceptance / Pre-production Handoff

> **STATUS: COMPLETED**
>
> 当前事实来源：[`docs/BYOA_MVP_STATUS.md`](BYOA_MVP_STATUS.md)。本文件不再列出已完成的扫码、播放或 CI 待办。

## Repository

```text
Repo: pixingzoudaiyuexing/xiaoya-byoa
Branch: feature/byoa-mvp
PR: #1 feat: bootstrap BYOA MVP（OPEN / Draft）
Final HEAD: d8187c01c99fd670f0d7d03c9b0bcd6245d3c79a
```

`main` 未合并，生产未部署。

## Completed Acceptance

```text
GitHub Actions: PASS
Docker clean build: PASS
Empty/persistent volume: PASS
Xiaoya anonymous browse: PASS
Aliyun NEED_AUTH / real QR / video playback / expiration-rescan: PASS
Quark NEED_AUTH / real QR / playback / expiration-rescan: PASS
Browser A/B isolation: PASS
if-newer no-op/update/key/admin preservation: PASS
Local HTTPS/fallback/Secure Cookie/management hiding: PASS
Database/storage/volume/log/cache/frontend security audit: PASS
```

真实凭据、Cookie、完整 Authorization 和 admin 密码未写入 Git、PR、测试夹具或报告。

## Known Limits

```text
Production Reality E2E: NOT RUN
Two distinct provider-account identity test: NOT RUN
Aliyun non-video original download/audio support: NOT RUN
External CVE scanner: NOT RUN
External DAST: NOT RUN
Production penetration test: NOT RUN
```

`Production Reality E2E` 已延期至 **Pre-production deployment acceptance**，因此：

```text
MVP READY
PRODUCTION READY: NO
```

## Pre-production Handoff

部署前必须由人工/独立审查完成：

1. 不连接旧测试服务器，不改变生产 Reality Panel；使用隔离的预生产节点和域名。
2. 接入真实公有 TLS，确认真实浏览器下 `xy_byoa_aliyun` 与 `xy_byoa_quark` 为 Secure、HttpOnly、SameSite=Lax、Path=/。
3. 确认公网 443 的 Reality 握手仍进入 Reality core，普通 HTTPS 才进入 fallback。
4. 确认 `5244` 只绑定 `127.0.0.1`，公网反代隐藏 `/@manage` 与 `/api/admin*`。
5. 故障演练：Xiaoya fallback 停止时 Reality core 流量不受影响；恢复后普通 HTTPS 可用。
6. 再做一次生产节点日志、数据库、volume、缓存和 HTTP 响应的敏感数据审计。
7. 完成独立 Gemini Security & Architecture Review 后，再决定 PR #1 是否从 Draft 改为 Ready。

## Hard Boundaries

- 不合并 `main`。
- 不部署生产。
- 不恢复服务器全局 Aliyun/Quark 私人账号。
- 不增加用户注册、Redis、Session DB 或账号池。
- 不把 BYOA 访客凭据写入 Storage 或跨用户 Link Cache。
