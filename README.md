# Xiaoya BYOA

Xiaoya BYOA 是基于 OpenList 和小雅公开目录开发的独立网盘浏览与播放服务。访客可以匿名浏览公开目录，播放 Aliyun 或 Quark 媒体时由当前浏览器扫码授权。

正式镜像：

```text
ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
```

支持 `linux/amd64` 和 `linux/arm64`。

## 一键部署 / 更新

服务器已经安装 Docker 时，复制下面这一整条命令执行即可。首次运行会创建容器和数据卷；以后再次执行同一条命令就是更新。

```bash
docker pull ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest && (docker rm -f xiaoya-byoa >/dev/null 2>&1 || true) && docker run -d --name xiaoya-byoa --restart unless-stopped -e TZ=Asia/Shanghai -e BYOA_XIAOYA_BOOTSTRAP=true -e BYOA_XIAOYA_UPDATE=if-newer -e BYOA_XIAOYA_STRICT=false -p 127.0.0.1:5244:5244 -v xiaoya_byoa_data:/opt/alist/data ghcr.io/pixingzoudaiyuexing/xiaoya-byoa:latest
```

检查运行状态：

```bash
docker ps --filter name=xiaoya-byoa
curl -fsS http://127.0.0.1:5244/ping
```

看到 `pong` 表示启动成功。

查看日志：

```bash
docker logs --tail 100 -f xiaoya-byoa
```

## 数据不会因更新丢失

一键命令使用独立 Docker volume：

```text
xiaoya_byoa_data -> /opt/alist/data
```

其中保存：

- Xiaoya/OpenList 数据库
- BYOA Cookie 加密密钥
- Xiaoya 数据版本

更新只会替换容器，不会删除该 volume。不要执行 `docker volume rm xiaoya_byoa_data`。

## HTTPS 反向代理

容器默认只监听：

```text
127.0.0.1:5244
```

这是有意的安全设置，5244 不应直接暴露到公网。生产环境请通过 Nginx、Caddy 等 HTTPS 反向代理访问，并传递：

```text
X-Forwarded-Proto: https
```

Nginx 示例：

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

公网部署还应隐藏管理接口：

```nginx
location ^~ /@manage {
    return 404;
}

location ^~ /api/admin {
    return 404;
}
```

不要阻断 `/api/fs/*`、`/api/public/byoa/*`、`/d/*` 和 `/p/*`。

## 当前能力

- Aliyun：浏览器扫码后，将分享视频临时转存到当前访客自己的专用目录，取得完整播放地址后立即回收本次创建的精确临时文件。已验证完整影片时长及 05:00、30:00 跳转播放。
- Quark：使用当前浏览器扫码凭据播放。
- 服务器不使用共享 Aliyun 账号池，不持久化访客 Refresh Token。

## 已知限制

- Aliyun 当前验证范围是视频播放；非视频原文件下载和音频未验证。
- BYOA 同路径请求可能复用 OpenList 全局 Link Cache / singleflight 中的请求派生状态。这是当前版本的已接受风险，计划后续加固。
- 不要在日志、Issue、PR 或配置中提交 Token、Cookie、Authorization、完整签名 URL 或管理员密码。

## 更多文档

- [部署说明](docs/BYOA_DEPLOY.md)
- [MVP / Release 状态](docs/BYOA_MVP_STATUS.md)
- [最终交接](docs/CODEX_HANDOFF.md)
- [许可证](LICENSE)

本项目基于 AGPL-3.0 组件。通过网络提供修改后的程序时，应履行对应源代码提供义务。
