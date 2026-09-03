# Xiaoya BYOA MVP 状态

## 目标

把 Xiaoya / PowerList 改造成轻量 BYOA 影视伪装站：内容可以公开浏览，点击阿里云盘或夸克资源时，由当前访客使用自己的网盘账号扫码授权并播放。

MVP 只支持：

- 阿里云盘
- 夸克

明确不做：

- 用户注册/登录系统
- Redis
- 服务端 Session 数据库
- 收藏、历史、会员、账号中心
- 115、UC、PikPak、迅雷、百度等其他 Provider
- 多账号负载均衡
- TVBox / Emby 扩展功能

## 核心原则

1. Xiaoya 内容继续跟随上游更新，不长期维护静态内容副本。
2. 浏览内容不要求注册本站账号。
3. 点击播放时才检查对应网盘凭据。
4. 浏览器 A 只能使用 A 自己扫码得到的凭据；浏览器 B 同理。
5. 访客凭据不写入 Storage、账号池或全局 Token 表。
6. BYOA 播放不得复用只按 fileID 建 Key 的账号相关直链缓存。
7. 不建设 Redis / Session / 用户数据库。
8. MVP 优先免转存直链，只有真实环境证明不可用时才回退到转存方案。

## 开发位置

- 分支：`feature/byoa-mvp`
- Draft PR：`#1 feat: bootstrap BYOA MVP`
- `main` 暂不合并

## 已完成

### 1. 浏览器级凭据

新增 `internal/byoa/credential.go` 及测试。

当前 Cookie：

- `xy_byoa_aliyun`
- `xy_byoa_quark`

缺少凭据统一返回：

```text
BYOA_AUTH_REQUIRED:<provider>
```

API 层会转换为结构化响应：

```json
{
  "code": 401,
  "message": "BYOA_AUTH_REQUIRED:quark",
  "data": {
    "byoa_auth_required": true,
    "provider": "quark"
  }
}
```

### 2. 夸克 BYOA 播放

`QuarkShare.Link()` 已接入当前浏览器凭据：

```text
xy_byoa_quark
  -> 当前访客夸克 Cookie
  -> 分享免转存 /file/download
  -> 播放直链
```

该路径绕过：

- 服务器 Quark 账号池
- `quarkUCShareLinkCache`
- 临时转存目录
- 多账号分片
- 123 秒传

BYOA 请求不会把访问者返回的 `__puus` 写回原有包级全局 Cookie。

### 3. 阿里 BYOA 播放

复用了仓库已有旧分享接口：

```text
https://api.alipan.com/v2/file/get_share_link_download_url
```

当前路径：

```text
xy_byoa_aliyun
  -> 当前访客普通 Aliyun Access Token
  + Xiaoya 公开分享 ShareToken / drive_id
  -> get_share_link_download_url
  -> 播放直链
```

因此 MVP 不需要：

- AliyundriveOpen 用户账号池
- Open API Refresh Token
- 临时目录
- 先转存到个人盘
- `aliyundriveShareLinkCache`

### 4. 夸克扫码 API

已按 Xiaoya 公开扫码协议改写为 Go，扫码过程无 Session：

```text
GET /api/public/byoa/quark/start
GET /api/public/byoa/quark/status?token=...
POST /api/public/byoa/clear?provider=quark
```

流程：

```text
申请 QR token
-> 浏览器持有 token 并轮询
-> service_ticket
-> 获取 Quark Cookie
-> 补 __puus
-> Set-Cookie 到当前浏览器
```

网盘 Cookie 不返回给前端 JavaScript。

### 5. 阿里扫码 API

已实现普通阿里云盘扫码链：

```text
GET /api/public/byoa/aliyun/start
GET /api/public/byoa/aliyun/status?ck=...&t=...
```

扫码确认后服务端读取 `pds_login_result.refreshToken`，立即调用阿里 token 接口换出短期 Access Token；Refresh Token 不持久化，只把 Access Token 写入当前浏览器凭据 Cookie。

### 6. 最小访客页面弹窗

没有 fork 整套 OpenList Frontend。

后端在访客 `index.html` 注入一段小型 BYOA 脚本，监听 `/api/fs/get` 的结构化 `NEED_AUTH` 响应：

```text
点击播放
-> NEED_AUTH(aliyun/quark)
-> 自动弹二维码
-> 扫码成功
-> 当前浏览器写入 HttpOnly Cookie
-> 刷新页面
-> 再次点击即可播放
```

MVP 暂不为了“扫码后自动重放原 Axios 请求”单独 fork 前端。

## CI

Fork 的 GitHub Actions 已于 2026-09-03 启用。

`.github/workflows/build.yml` 在 PR 到 `main` 时执行 `Test Build`，目标包括：

- `linux-amd64-musl`
- `linux-arm64-musl`

本次文档提交用于重新触发该 workflow。CI 结果以 GitHub Actions 实际构建为准。

## 仍需真实环境验证

### 夸克

1. 没有服务器 `quark_cookie.txt` 时，Xiaoya QuarkShare 是否仍可完成目录 List。
2. `/file/download` 免转存直链在当前真实夸克分享上的稳定性。
3. 扫码所得夸克 Cookie 是否稳定低于单 Cookie 大小限制。

### 阿里

1. `get_share_link_download_url` 当前是否仍可对 Xiaoya 分享稳定工作。
2. 分享根目录取得的 `drive_id` 是否覆盖当前 Xiaoya 阿里内容。
3. 普通 Aliyun Access Token 的实际有效期。

### Xiaoya 数据层

当前 PowerList 已是 OpenList v4，默认 Dockerfile 基于 `openlistteam/openlist-base-image`。虽然代码仍保留 Xiaoya 专用启动适配，但在验证数据库 / update.sql / index.zip 兼容前，不直接采用旧 `xiaoyaliu/alist + 覆盖二进制` 方案。

## 下一顺序

1. CI 编译通过并修完全部编译错误。
2. 做最小可部署测试镜像/部署方法。
3. 实机验证阿里扫码 -> 播放。
4. 实机验证夸克扫码 -> 播放。
5. 验证无服务端私人 Token 时的 Xiaoya 内容浏览。
6. 实现 Xiaoya bootstrap / updater 最小兼容层。
7. A/B 两浏览器隔离验收。
8. 接入 Reality fallback 做最终伪装站测试。
