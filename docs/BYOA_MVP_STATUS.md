# Xiaoya BYOA MVP 状态

> **STATUS: RELEASE APPROVED / SSOT**
>
> 本文件是当前正式发布状态与验收事实来源。

## Release

```text
Branch: main
PR: #1 feat: bootstrap BYOA MVP（release approved; merge pending）
Production image channel: ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
Feature release source: d23062f5
```

`main` 是正式稳定代码。每次推送 `main` 都会由 GitHub Actions 构建并发布多架构 `latest`，同时发布 `sha-<short-sha>` 追踪标签；版本 tag 还会发布对应的版本标签。RealityPanel 固定使用 `latest`，不使用 `mvp`。

## MVP 范围与凭据边界

当前 MVP 支持 Aliyun、Quark 的匿名目录浏览和浏览器扫码播放。服务器不保存访客私人网盘账号，不建设用户系统、Redis、Session 数据库或账号池。

浏览器 Cookie `xy_byoa_aliyun`、`xy_byoa_quark` 为 AES-256-GCM 密文，HttpOnly、SameSite=Lax、Path=/；HTTPS 请求下 Secure。持久密钥位于 `/opt/alist/data/byoa_cookie.key`，权限 0600。访客凭据不写入 Storage、数据库 Session、包级全局状态或日志。

## Playback

Aliyun 播放优先调用 `get_share_link_download_url`；该接口返回 HTTP 410 Gone 后，回退到 `get_share_link_video_preview_play_info`，使用 `preview_url`（兼容 `url`）播放视频。MVP 已验证视频预览播放；非视频原文件下载和音频不属于当前已验证能力。

Aliyun 视频回退列表现在优先使用完整 `url`，仅在 `url` 为空时使用 `preview_url`；Aliyun 2-minute preview bug: **FIXED**。

Quark 播放通过同源 `/p/` 请求级代理向 CDN 发送当前浏览器 Cookie、UA 和 Referer。

首页 README 请求失败时，访客注入层只静默移除 README 的错误块；README success 继续正常显示，README failure 不再显示 Axios 500。

## Final Acceptance

```text
GitHub Actions GREEN: PASS
Local Docker clean/empty/persistent volume: PASS
Aliyun real QR/video playback/expiration-rescan: PASS
Quark real QR/video playback/expiration-rescan: PASS
Previous browser A/B acceptance: PASS (did not cover exact same-path cache reuse)
if-newer: PASS
Local HTTPS/fallback/Secure Cookie/management hiding: PASS
```

CI evidence from the implementation baseline:

```text
Test BYOA Production Image: 34301653708 PASS
Test Build: 34301653784 PASS
```

## Known Security Findings: Accepted Risk

```text
Known issue:
BYOA same-path requests may reuse request-derived Link state through
OpenList global Link Cache / singleflight.

Impact:
Potential cross-user playback capability / Quark credential reuse
when different visitors access the exact same file path.

Current product decision:
Accepted Risk for the low-traffic Reality camouflage-site use case.

Status:
Deferred. Planned for future hardening.
```

The previous browser A/B test covered browser-cookie isolation only; it did not test exact same-path cache reuse. This accepted risk does not block the current release.

## Production Boundary

```text
RELEASE APPROVED
PRODUCTION IMAGE CHANNEL: latest
Production Reality E2E: NOT RUN; deferred to pre-production deployment acceptance
```

The release is approved for the stated low-traffic camouflage-site use case. Real public TLS, production browser cookies, endpoint hiding and Reality/fallback failure isolation remain deployment acceptance checks. External CVE scanning, DAST, penetration testing, two distinct provider-account identity testing and Aliyun non-video/audio validation were not run.
