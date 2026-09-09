# Xiaoya BYOA MVP 状态

> **STATUS: COMPLETED / SSOT**
>
> 本文件是当前实现和最终本地验收事实来源。最终验收报告见
> `outputs/XIAOYA_BYOA_MVP_FINAL_ACCEPTANCE.md`（本地交付目录）。

## Repository

```text
Branch: feature/byoa-mvp
PR: #1 feat: bootstrap BYOA MVP（OPEN / Draft）
Final HEAD: d8187c01c99fd670f0d7d03c9b0bcd6245d3c79a
```

`main` 未合并，生产未部署。

## MVP 范围

当前 MVP 只支持：

- Aliyun
- Quark

访客匿名浏览 Xiaoya 公开目录；点击真实媒体后，由当前浏览器扫码授权。服务器不保存访客私人网盘账号，不建设用户系统、Redis、Session 数据库或账号池。

明确不做：115、UC、PikPak、迅雷、百度、TVBox/Emby 扩展、多账号负载均衡和服务器全局私人 Token/Cookie。

## 凭据边界

浏览器 Cookie：

```text
xy_byoa_aliyun
xy_byoa_quark
```

属性：AES-256-GCM 密文、HttpOnly、SameSite=Lax、Path=/；HTTPS 请求下 Secure。持久密钥位于 `/opt/alist/data/byoa_cookie.key`，权限 0600。访客凭据不写入 `x_storages`、数据库 Session、包级全局状态或日志。

缺失、损坏、过期或明确鉴权失败统一转换为：

```text
BYOA_AUTH_REQUIRED:aliyun
BYOA_AUTH_REQUIRED:quark
```

## Aliyun 实现与验收

扫码：浏览器持有 `ck/t` 并轮询；Refresh Token 仅在确认请求内换取短期 Access Token，随后丢弃；Access Token 只写当前浏览器加密 HttpOnly Cookie。

播放链路：

```text
优先尝试 get_share_link_download_url
        ↓ HTTP 410 Gone（上游已退役）
回退 get_share_link_video_preview_play_info
        ↓
使用 preview_url（兼容 url）播放视频
```

已实际验证：匿名目录浏览、NEED_AUTH、真实 QR、真实视频播放、Access Token 失效后 NEED_AUTH、重扫恢复播放。当前已验证能力是视频预览播放；非视频原文件下载/音频不属于本 MVP 已验证能力。

## Quark 实现与验收

扫码成功得到的 Quark Cookie 只写当前浏览器加密 Cookie。本地真实密文长度：781 bytes（目标 <=3500）。

播放使用当前浏览器凭据调用分享 `/file/download`，随后通过同源 `/p/` 请求级代理向 CDN 发送 Cookie、UA 和 Referer。BYOA 路径绕过 `quarkUCShareLinkCache`、服务器账号池和转存。

已实际验证：匿名目录浏览、NEED_AUTH、真实 QR、真实视频播放、清除 Cookie 后重新 NEED_AUTH、重扫恢复播放。

## Browser A/B

使用 Codex In-app Browser 与独立 Chrome profile：

- A/B 均完成 Aliyun 与 Quark 授权并播放真实视频；
- 扫码成功后自动返回原媒体页面，不停留在 OpenList 登录框；
- JS 无法读取任一 BYOA Cookie；
- A/B 不共享 Cookie、Session、Storage 私人字段或 BYOA 全局 Link Cache。

是否为两个不同网盘账号未验证；浏览器级隔离已 PASS。

## Xiaoya Bootstrap / 持久化

空卷首启无需 `mytoken.txt`、`myopentoken.txt`、`quark_cookie.txt`。已实测：

```text
AliyunShare: 101
QuarkShare: 6
Alias: 16
Legacy/account-bound: 0
Guest root entries: 18
```

`/opt/alist/data/xiaoya_data.version` 与 SQLite `byoa_state.xiaoya_data_version` 保持一致。相同版本不重复更新；强制设为 `0.0.0` 后会更新并恢复版本。容器重建后 BYOA key、admin 指纹、目录和版本保持。

## CI

当前 HEAD CI 全绿：

```text
HEAD: d8187c01c99fd670f0d7d03c9b0bcd6245d3c79a
Test BYOA Production Image: 34301653708 PASS
Test Build: 34301653784 PASS
  Test BYOA MVP: PASS
  Build linux-arm64-musl: PASS
  Build linux-amd64-musl: PASS
  Smoke persistent Xiaoya BYOA runtime: PASS
```

## Local HTTPS / Fallback

```text
Local HTTPS/fallback integration: PASS
X-Forwarded-Proto=https: PASS
Secure Cookie behavior: PASS
5244: 127.0.0.1 only
/@manage: hidden
/api/admin*: hidden
Normal HTTPS → fallback → Xiaoya BYOA: PASS
Fallback backend failure does not stop proxy process: PASS
```

本地使用 Caddy internal CA；未修改系统信任设置。生产必须使用真实公有 TLS 证书。

## Security Audit

最终只读审计结果：

- database integrity：`ok`；Session 表：0；Aliyun/Quark 私人字段：0；
- data volume 中 `mytoken.txt`、`myopentoken.txt`、`quark_cookie.txt`：0；
- 日志中 refresh/access token、`__puus`、Authorization、Bearer、完整 Cookie：0 命中；
- BYOA 不进入 `aliyundriveShareLinkCache` 或 `quarkUCShareLinkCache`；
- 公共 QR 接口使用 POST JSON、长度校验、15 秒上游超时、按 IP 限流和错误清洗。

## Final Status

```text
MVP READY
```

这不等于生产就绪：

```text
PRODUCTION READY: NO
Production Reality E2E: NOT RUN
Deferred to: Pre-production deployment acceptance
```

明确未完成：生产 Reality 公网 E2E、两个不同 provider-account identity 的验证、Aliyun 非视频原文件/音频支持、外部 CVE scanner、外部 DAST、生产渗透测试。

在独立 Gemini Security & Architecture Review 完成前，PR 保持 Draft；不要合并 `main`，不要部署生产。
