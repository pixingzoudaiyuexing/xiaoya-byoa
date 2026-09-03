# Xiaoya BYOA 部署说明

> 当前文档对应 `feature/byoa-mvp`。在真实阿里/夸克扫码播放与 Reality fallback 最终验收完成前，不合并 `main`。

## 1. 这套部署解决什么

Xiaoya BYOA 用于轻量 Reality fallback / 伪装站：

- Xiaoya 丰富目录可匿名浏览；
- 服务器不需要保存 `mytoken.txt`、`myopentoken.txt`、`quark_cookie.txt`；
- 访客点到需要账号的阿里/夸克媒体时，当前浏览器才弹出对应二维码；
- 扫码所得网盘凭据只写入当前浏览器的加密 `HttpOnly` Cookie；
- 不建立本站用户系统、Redis、Session 数据库、账号池；
- 不把访客网盘凭据写进 `x_storages`。

当前 MVP 只支持：

- 阿里云盘
- 夸克网盘

## 2. 推荐部署方式

仓库提供独立的 BYOA 镜像定义，不修改上游通用 `Dockerfile`：

```text
Dockerfile.byoa
docker-compose.byoa.yml
```

启动：

```bash

git clone https://github.com/pixingzoudaiyuexing/xiaoya-byoa.git
cd xiaoya-byoa
git checkout feature/byoa-mvp

docker compose -f docker-compose.byoa.yml up -d --build
```

查看状态：

```bash
curl http://127.0.0.1:5244/ping
docker logs --tail 200 xiaoya-byoa
```

正常应返回：

```text
pong
```

默认只监听：

```text
127.0.0.1:5244
```

这是故意的。不要把 OpenList/PowerList 管理端口直接暴露到公网；应由 Reality fallback 或本机 Nginx/Caddy 反代。

## 3. 首次启动会发生什么

空数据卷第一次启动时：

```text
生成持久 BYOA Cookie 加密密钥
        ↓
拉取 Xiaoya 官方公开数据
        ↓
执行 Xiaoya /updateall
        ↓
AliyundriveShare → BYOA AliyunShare
        ↓
移除服务器私人账号字段
        ↓
保留阿里 + 夸克 + Alias 公共目录
        ↓
启动 5244
```

不需要事先创建：

```text
mytoken.txt
myopentoken.txt
temp_transfer_folder_id.txt
quark_cookie.txt
```

当前 CI 已实测 tokenless 初始化可生成真实 Xiaoya 内容，包括阿里分享、夸克分享和根目录分类。

## 4. 内容自动更新

默认：

```text
BYOA_XIAOYA_UPDATE=if-newer
```

每次容器启动时：

1. 检查 Xiaoya 官方 `version.txt`；
2. 版本相同则直接启动；
3. 版本变化才执行内容更新；
4. 远端版本检查失败时继续使用本地内容；
5. 已有实例更新时保护本地 admin 行；
6. 夸克公开分享清单下载失败时，已有实例跳过本次更新，避免破坏现有目录。

可用模式：

```text
if-newer   推荐，仅有新版本时更新
if-missing 仅 data.db 不存在时初始化
always     每次启动都更新
never      完全禁止自动更新
```

## 5. 持久化数据

Compose 默认使用：

```text
xiaoya_byoa_data:/opt/alist/data
```

其中包括：

```text
data.db
byoa_cookie.key
xiaoya_data.version
...
```

`byoa_cookie.key` 是服务实例级 AES-256 密钥，用来解密浏览器自己携带的 BYOA Cookie。它不是任何阿里/夸克账号 Token，但必须像应用密钥一样保护：

- 不放进 Git；
- 不通过网页公开；
- 不打印到日志；
- 不随意删除，否则已有浏览器 Cookie 无法解密，需要重新扫码；
- 文件权限保持 `0600`。

## 6. HTTPS 与反向代理

生产环境必须使用 HTTPS。后端根据 TLS 或 `X-Forwarded-Proto: https` 给 BYOA Cookie 加 `Secure`。

Nginx 最小示例：

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

Reality 节点应保持原来的流量分流：Reality 命中正常走 Reality；普通 HTTPS 才进入上述 fallback。

概念结构：

```text
公网 443
   ├── Reality 流量 → Reality 服务
   └── 普通 HTTPS → fallback/Nginx → 127.0.0.1:5244 → Xiaoya BYOA
```

## 7. 伪装站专用时建议隐藏管理入口

如果这个实例只作为公开 fallback，不需要用户从公网管理 OpenList，建议在公网反代层额外隐藏：

```nginx
location ^~ /@manage {
    return 404;
}

location ^~ /api/admin {
    return 404;
}
```

如确定完全不需要公网登录，也可以继续阻断 `/api/auth/login*`。管理操作通过服务器本机、SSH 隧道或独立受保护管理域名完成。

不要阻断：

```text
/api/fs/*
/api/public/byoa/*
/d/*
/p/*
```

这些路径可能参与浏览、扫码或播放。

## 8. 公共扫码接口保护

代码层已加入无状态、按 IP 的轻量限流：

- QR start：持续约 1 请求/秒，短时 burst 5；
- QR status：持续约 5 请求/秒，短时 burst 20；
- 过期 IP 记录会自动清理；
- 不使用 Redis，也不产生用户 Session。

生产反代仍建议再做一层基础限流/WAF，尤其是 fallback 域名完全公开时。

扫码第三方上游请求另有 15 秒硬超时，防止阿里/夸克接口异常时长期占用连接。

## 9. 凭据安全边界

### 服务端不会持久化的内容

BYOA 设计不把访客的：

```text
Aliyun Refresh Token
Aliyun Access Token
Quark Cookie
```

写入 Storage、账号池或用户数据库。

阿里扫码流程中的 Refresh Token 只在当前请求内用于换短期 Access Token，随后丢弃。

### 浏览器保存什么

当前浏览器保存加密后的：

```text
xy_byoa_aliyun
xy_byoa_quark
```

属性包括：

```text
HttpOnly
SameSite=Lax
Secure（HTTPS 时）
```

浏览器 A 与浏览器 B 的 Cookie 天然隔离。

### 仍然存在的现实风险

BYOA 不是“绝对安全”：浏览器恶意扩展、终端恶意软件、站点 XSS、服务端密钥泄露都可能扩大风险。因此生产环境仍应保持：

- HTTPS；
- 最小化第三方脚本；
- 不暴露 data volume；
- 不在日志中打印 Cookie/Header；
- 定期更新基础镜像与依赖。

## 10. 当前已经自动验证的项目

CI 已覆盖/正在持续覆盖：

- BYOA 凭据加密与浏览器隔离逻辑；
- 阿里/夸克缺授权结构化 `NEED_AUTH`；
- AliyunShare / QuarkShare BYOA 路径；
- 访客页面 XHR + fetch 扫码触发；
- linux/amd64 musl 静态构建；
- linux/arm64 musl 静态构建；
- 空数据卷 tokenless 首启；
- Xiaoya 真实目录匿名浏览；
- 真实媒体文件缺授权触发对应 Provider；
- 容器删除重建后 BYOA 密钥/目录持久化；
- Compose 与 shell 语法；
- QR 公共接口协议 smoke；
- `if-newer` 内容更新与 admin/密钥保护。

最后三项以当前 GitHub Actions 最新成功结果为准。

## 11. 仍必须做的真实环境验收

自动 CI 不能替代真实账号扫码确认。合并 `main` 前仍需：

1. 阿里 App 扫码确认 → Cookie 写入 → 真实 Xiaoya 阿里媒体播放；
2. 夸克 App 扫码确认 → Cookie 写入 → 真实 Xiaoya 夸克媒体播放；
3. 浏览器 A 与浏览器 B 使用不同账号，确认互不串号；
4. 凭据过期后确认自动重新弹出扫码；
5. 接入真实 Reality 节点，确认 Reality 流量与普通 HTTPS fallback 同时正常；
6. 检查日志、`x_storages`、data volume，确认没有访客私人 Token/Cookie 泄漏。

## 12. 许可证

本项目基于 AGPL-3.0 组件。通过网络向用户提供修改后的 AGPL 程序时，需要遵守对应的源代码提供义务。

如果未来把它集成进商业 Reality Panel，建议保持 Xiaoya BYOA 为独立容器/独立组件，并单独处理 AGPL 合规，不要默认认为这一部分可以闭源打包进专有后端。
