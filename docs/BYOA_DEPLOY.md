# Xiaoya BYOA 部署说明

> 当前文档对应 `feature/byoa-mvp`，与最终 MVP 验收实现同步。
>
> MVP 状态：**READY**。生产状态：**NOT READY**，Production Reality E2E 延期至预生产部署验收。

## 部署边界

Xiaoya BYOA 是 Reality fallback 伪装站：公开目录匿名浏览，真正播放时由当前浏览器扫码。服务器不保存访客私人 Aliyun/Quark 账号，不使用用户 Session DB、Redis 或账号池。

支持 Provider：Aliyun、Quark。不要恢复 `mytoken.txt`、`myopentoken.txt`、`quark_cookie.txt` 或其他全局私人账号方案。

## 启动

```bash
git clone https://github.com/pixingzoudaiyuexing/xiaoya-byoa.git
cd xiaoya-byoa
git checkout feature/byoa-mvp
docker compose -f docker-compose.byoa.yml up -d --build
curl -fsS http://127.0.0.1:5244/ping
```

默认结果：`pong`。

Compose 只发布：

```text
127.0.0.1:5244 -> container:5244
```

5244 不应直接暴露到公网；由 HTTPS reverse proxy / Reality fallback 接入。

## 首次启动与持久化

空 volume 首启自动取得 Xiaoya 官方公开数据，归一化为 `AliyunShare`、`QuarkShare` 和 `Alias`。不需要预置任何私人 Token/Cookie。

持久化目录 `/opt/alist/data` 包含：

```text
data.db
byoa_cookie.key       # 0600，实例级 AES-256-GCM 密钥
xiaoya_data.version   # 与 byoa_state.xiaoya_data_version 一致
```

删除 container、保留 volume 时，BYOA key、admin、目录和版本应保持。`BYOA_XIAOYA_UPDATE=if-newer` 同版本不更新，远端版本检查失败继续使用本地内容。

## HTTPS Reverse Proxy

生产必须使用真实公有 TLS。反代必须传递：

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
normal HTTPS
    -> fallback / reverse proxy
    -> Xiaoya BYOA 127.0.0.1:5244
```

HTTPS 下 BYOA Cookie 必须为：

```text
HttpOnly; Secure; SameSite=Lax; Path=/
```

## Reality Fallback

目标结构：

```text
公网 :443
  ├─ Reality handshake -> Reality core service
  └─ normal HTTPS      -> fallback -> Xiaoya BYOA :5244
```

Reality core service 与 Xiaoya fallback 必须故障解耦：fallback 停止不能改变 Reality 客户端流量；Xiaoya 恢复后普通 HTTPS 页面和播放恢复。

## 管理入口隐藏

伪装站公网反代应隐藏：

```nginx
location ^~ /@manage {
    return 404;
}

location ^~ /api/admin {
    return 404;
}
```

不要阻断：`/api/fs/*`、`/api/public/byoa/*`、`/d/*`、`/p/*`。

## 公共扫码接口

扫码 status 只接受 POST JSON；Aliyun `ck/t` 和 Quark `token` 不放 query。接口有：

- start：按 IP 约 1 req/s，burst 5；
- status：按 IP 约 5 req/s，burst 20；
- 上游请求硬超时 15 秒；
- 错误只返回固定分类，不返回 Token/Cookie/响应正文。

生产反代仍建议增加基础 WAF/限流。

## Pre-production Checklist

部署前必须逐项取得证据：

```text
[ ] Production Reality E2E
[ ] Real public TLS certificate and browser trust
[ ] Secure Cookie in a real browser
[ ] /@manage and /api/admin* hidden from public fallback
[ ] Public 5244 remains loopback-only
[ ] Reality traffic unaffected by fallback failure
[ ] Normal HTTPS reaches Xiaoya BYOA fallback
[ ] Aliyun/Quark credentials absent from DB, Storage, volume, logs and cache
[ ] Independent Gemini Security & Architecture Review complete
```

以上清单未完成前：

```text
MVP READY
PRODUCTION READY: NO
DO NOT DEPLOY PRODUCTION
```

## License

本项目基于 AGPL-3.0 组件。通过网络提供修改后的程序时，应履行对应源代码提供义务。
