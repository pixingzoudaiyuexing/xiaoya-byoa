# Xiaoya BYOA MVP 状态

## 目标

把 Xiaoya / PowerList 改造成轻量 BYOA Reality fallback 伪装站：

- Xiaoya 丰富目录可以匿名浏览；
- 点击阿里云盘或夸克真实媒体时，才要求当前访客扫码；
- 浏览器 A 使用 A 自己的网盘凭据，浏览器 B 使用 B 自己的凭据；
- 服务器不保存访客私人网盘账号，不建设本站用户系统。

当前 MVP 只支持：

- 阿里云盘
- 夸克网盘

明确不做：

- 用户注册/登录体系
- Redis
- 服务端 Session 数据库
- 收藏、历史、会员、账号中心
- 115、UC、PikPak、迅雷、百度等其他 Provider
- 多账号负载均衡
- TVBox / Emby 扩展功能

## 开发位置

```text
Branch: feature/byoa-mvp
Draft PR: #1 feat: bootstrap BYOA MVP
```

`main` 暂不合并。

---

## 已完成

### 1. 浏览器级加密凭据

当前 Cookie：

```text
xy_byoa_aliyun
xy_byoa_quark
```

特点：

- AES-256-GCM 加密；
- `HttpOnly`；
- `SameSite=Lax`；
- HTTPS 下自动 `Secure`；
- 持久 BYOA 加密密钥位于 `/opt/alist/data/byoa_cookie.key`；
- 容器重建后密钥保持，已有浏览器无需因为容器重建重新扫码；
- 加密 Cookie 超过安全尺寸阈值时明确报错，避免浏览器静默丢弃。

缺少/损坏/密钥不匹配的凭据统一转换为：

```text
BYOA_AUTH_REQUIRED:<provider>
```

API 返回结构化数据供访客脚本识别。

### 2. 阿里 BYOA

扫码流程：

```text
阿里二维码
→ 浏览器持有 ck/t 并轮询
→ 用户确认
→ 服务端短暂取得 Refresh Token
→ 立即换短期 Access Token
→ Refresh Token 丢弃
→ Access Token 加密写入当前浏览器 HttpOnly Cookie
```

播放：

```text
当前浏览器 Access Token
+ Xiaoya 公开 ShareToken / drive_id
→ get_share_link_download_url
→ 播放直链
```

不需要：

- AliyundriveOpen 用户账号池；
- 服务端 Open API Refresh Token；
- 临时目录；
- 转存到个人盘；
- `aliyundriveShareLinkCache`。

Access Token 失效/过期会重新返回 `NEED_AUTH(aliyun)`，而不是普通 500。

### 3. 夸克 BYOA

扫码流程无服务端 Session：

```text
申请 QR token
→ 浏览器持有 token 并轮询
→ service_ticket
→ 获取 Quark Cookie / __puus
→ 加密写入当前浏览器 HttpOnly Cookie
```

播放：

```text
当前浏览器 Quark Cookie
→ 分享 /file/download
→ 播放直链
```

该路径绕过：

- 服务器 Quark 账号池；
- `quarkUCShareLinkCache`；
- 临时转存目录；
- 多账号分片；
- 123 秒传。

BYOA 请求不会把访问者 Cookie 写回包级全局状态。

明确的登录/鉴权失效会重新返回 `NEED_AUTH(quark)`；分享失效、限流等普通错误不会全部误判成重新扫码。

### 4. 最小访客扫码 UI

没有 fork 整套 OpenList Frontend。

后端只在访客 `index.html` 注入小型脚本：

```text
点击媒体
→ /api/fs/get 返回 NEED_AUTH
→ 自动识别 aliyun/quark
→ 弹对应二维码
→ 扫码成功
→ HttpOnly Cookie 写入
→ 页面刷新
→ 再次点击即可播放
```

现在同时兼容：

- `XMLHttpRequest`
- `fetch`

管理后台不依赖这套访客脚本。

### 5. 公共扫码接口保护

阿里/夸克扫码上游请求均有 15 秒硬超时。

公开 start/status 接口加入无状态内存限流：

- start：持续约 1 次/秒，burst 5；
- status：持续约 5 次/秒，burst 20；
- 按 IP 隔离；
- 过期 IP 自动清理；
- 不引入 Redis/Session。

### 6. Tokenless Xiaoya bootstrap

新增：

```text
scripts/byoa-xiaoya-bootstrap.sh
```

`entrypoint.sh` 在正常首次启动时自动执行。

无需：

```text
mytoken.txt
myopentoken.txt
temp_transfer_folder_id.txt
quark_cookie.txt
```

bootstrap 会：

1. 使用 Xiaoya 官方公开数据运行 `/updateall`；
2. 将历史 `AliyundriveShare*` 归一化成 BYOA `AliyunShare`；
3. 清除 Ali storage 中历史私人账号字段；
4. 清除 QuarkShare 全局 cookie 字段；
5. 删除 115/UC/PikPak 等非 MVP 账号型 Storage；
6. 保留阿里、夸克与 Alias 内容；
7. 开启 guest 只读浏览所需权限。

### 7. Xiaoya 内容更新

默认：

```text
BYOA_XIAOYA_UPDATE=if-newer
```

目标行为：

- 每次容器启动检查 Xiaoya 官方版本；
- 相同版本不重复更新；
- 新版本才执行更新；
- 远端版本检查失败时继续使用本地内容；
- 已有实例更新时保护 admin 行；
- 夸克分享清单下载失败时已有实例保留旧数据库；
- 成功后记录 `/opt/alist/data/xiaoya_data.version`。

这部分已加入 runtime smoke，最新实现正在由 GitHub Actions 持续验证。

### 8. 正式部署入口

新增：

```text
Dockerfile.byoa
docker-compose.byoa.yml
docs/BYOA_DEPLOY.md
```

Compose 默认只绑定：

```text
127.0.0.1:5244
```

用于 Reality/Nginx fallback，不直接把管理端口暴露到公网。

---

## 已被 CI 实际证明的运行时结果

在无服务器阿里/夸克私人 Token、空数据卷环境中，runtime smoke 已实际生成：

```text
AliyunShare: 101
QuarkShare: 6
Alias: 16
Legacy/account-bound storage: 0
```

guest 根目录实测有 18 个真实分类/入口，例如：

```text
元数据
体育
动漫
教育
每日更新
电影
电视剧
纪录片
综艺
音乐
📺画质演示测试（4K，8K，HDR，Dolby）
...
```

### 阿里匿名目录实测

示例：

```text
/曲艺/戏曲（京，豫，吕，黄梅戏）剧
```

匿名 List 成功，曾实测一次返回 708 个条目。

找到真实媒体：

```text
1000京剧 马连良 - 湄邬县在马上心神不定.wma
```

无凭据 `/api/fs/get` 正确触发：

```text
NEED_AUTH(aliyun)
```

### 夸克匿名目录实测

示例：

```text
/每日更新/电视剧/日剧（夸克）
```

匿名 List 成功。

找到真实媒体：

```text
/每日更新/电视剧/日剧（夸克）/最好的选择TAXI/S01E09.mp4
```

无凭据 `/api/fs/get` 正确触发：

```text
NEED_AUTH(quark)
```

### 容器重建实测

已通过：

```text
空数据卷
→ 正常 docker run
→ entrypoint 自动 bootstrap
→ 服务上线
→ 删除容器
→ 同一持久卷重建
→ 内容保持
→ BYOA key hash 保持
```

双架构 musl 静态编译也已多轮通过：

- linux/amd64
- linux/arm64

---

## 当前 CI 收口项

最新测试套件除上述已证明内容外，还覆盖：

- `server/static` XHR + fetch 注入测试；
- QR IP 限流测试；
- shell 语法；
- Compose 配置解析；
- 阿里/夸克 QR start + 未扫码状态协议 smoke；
- `xiaoya_data.version` 持久化；
- 同卷重建后 version/key/admin 不变；
- 人为把版本改成 `0.0.0` 后自动更新；
- 更新后 BYOA key/admin 继续保持。

在最新 HEAD 全绿之前，PR 继续保持 Draft。

---

## 还不能由 CI 替代的真实验收

### 阿里

1. 手机 App 实际扫码确认；
2. 扫码所得 Access Token 对当前 Xiaoya 分享真实取链；
3. 浏览器实际播放；
4. Token 真实过期后的重扫体验。

### 夸克

1. 手机 App 实际扫码确认；
2. 扫码所得完整 Quark Cookie 加密后是否稳定低于 3500 字节；
3. `/file/download` 对当前真实分享稳定取链；
4. 浏览器实际播放；
5. Cookie 真实失效后的重扫体验。

### A/B 隔离

必须用两个浏览器 Profile/两个真实会话验证：

```text
Browser A → Account A
Browser B → Account B
```

确保无串号、无数据库 Session、无 Storage 凭据污染、无跨用户 Link Cache。

### Reality fallback

最后接到真实 Reality 测试节点确认：

```text
Reality 流量 → Reality 服务
普通 HTTPS → fallback → Xiaoya BYOA
```

并检查日志、数据库、data volume 不存在访客私人 Token/Cookie 泄漏。

完整交接步骤见：

```text
docs/CODEX_HANDOFF.md
```

建议 Codex：**GPT-5.6 Sol / High**。

---

## 合并标准

只有以下全部通过后，才把 PR 从 Draft 转 Ready：

1. GitHub Actions 全绿；
2. `Dockerfile.byoa` 实机正常构建/启动；
3. 阿里真实扫码播放通过；
4. 夸克真实扫码播放通过；
5. A/B 浏览器隔离通过；
6. 凭据失效重扫通过；
7. `if-newer` 更新保护通过；
8. Reality fallback 实机通过；
9. 安全终审无私人凭据泄漏。

即使全部通过，也不自动合并 `main`，最终由用户决定。
