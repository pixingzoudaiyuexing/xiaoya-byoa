# Codex 真实环境验收交接

## 建议模型

- **模型：GPT-5.6 Sol**
- **推理强度：High**

这份任务只处理 ChatGPT/GitHub CI 无法替代的真实浏览器、真实网盘账号和真实 Reality 节点验收。不要重新设计架构，不要重做已经通过的 BYOA 核心。

---

## 仓库与边界

```text
Repo:   pixingzoudaiyuexing/xiaoya-byoa
Branch: feature/byoa-mvp
PR:     #1 feat: bootstrap BYOA MVP
```

必须遵守：

1. **不要合并 main**。
2. 所有修复继续提交到 `feature/byoa-mvp`。
3. 不把真实阿里/夸克 Refresh Token、Access Token、Cookie 提交到 Git、Issue、PR、日志或测试夹具。
4. 不增加本站用户系统、Redis、Session 数据库、账号池。
5. 不恢复服务端全局 `mytoken/myopentoken/quark_cookie` 方案。
6. 不启用 115/UC/PikPak 等非 MVP Provider。
7. 不把 BYOA 用户直链重新放进只按 fileID 建 key 的全局缓存。
8. 不为了测试去修改用户现有 Reality Panel 生产节点；先使用测试节点/测试域名。

---

## 当前架构不要推翻

```text
Xiaoya 官方公开数据
        ↓
公开目录匿名浏览
        ↓
点击真实媒体
        ↓
NEED_AUTH(provider)
        ↓
当前浏览器扫码
        ↓
HttpOnly 加密 Cookie
        ↓
当前请求 BYOA Link
        ↓
阿里 / 夸克真实播放直链
```

服务器只持久化：

```text
/opt/alist/data/data.db
/opt/alist/data/byoa_cookie.key
/opt/alist/data/xiaoya_data.version
```

访客私人网盘凭据不应写入 Storage/账号池/用户数据库。

---

## 开始前先做

```bash
git fetch origin
git checkout feature/byoa-mvp
git pull --ff-only origin feature/byoa-mvp
git status
```

确认工作区干净后：

```bash
go test ./internal/byoa -count=1
go test ./server/common -run BYOA -count=1
go test ./server/static -run '^TestBYOA' -count=1
go test ./server/handles -run '^TestBYOAIPRateLimiterBurstAndRefill$' -count=1
go test ./drivers/quark_uc_share -run '^TestQuarkBYOARequiresBrowserCredential$' -count=1
go test ./drivers/aliyundrive_share2_open -run '^TestAliyunBYOARequiresBrowserCredential$' -count=1
sh -n entrypoint.sh
sh -n scripts/byoa-xiaoya-bootstrap.sh
docker compose -f docker-compose.byoa.yml config
```

如果 GitHub Actions 最新一轮不是全绿，先读取失败 job 日志并修复；不要跳过失败测试直接做真实环境验收。

---

# Phase A：正式 BYOA 镜像实机启动

使用仓库提供的：

```text
Dockerfile.byoa
docker-compose.byoa.yml
```

执行：

```bash
docker compose -f docker-compose.byoa.yml up -d --build
curl -fsS http://127.0.0.1:5244/ping
docker logs --tail 300 xiaoya-byoa
```

验收：

- `/ping` 返回 `pong`；
- 不创建 `mytoken.txt`；
- 不创建 `myopentoken.txt`；
- 不创建服务器 `quark_cookie.txt`；
- 根目录能看到 Xiaoya 丰富分类；
- 阿里/夸克目录在未扫码前可以浏览；
- 点击真实媒体时弹出正确 Provider 二维码。

记录当前数据库状态，但不要输出 addition 的完整敏感内容：

```bash
docker exec xiaoya-byoa sqlite3 /opt/alist/data/data.db \
  "select driver,count(*) from x_storages group by driver order by driver;"
```

预期至少有：

```text
AliyunShare
QuarkShare
Alias
```

并确认没有 MVP 已删除的全局账号型 Storage。

---

# Phase B：真实阿里云盘扫码 → 播放

必须使用浏览器实际操作，不用伪造 Token。

流程：

1. 新建一个干净浏览器 Profile/无痕环境 A；
2. 打开 BYOA 页面；
3. 找到 CI 已证明可匿名浏览的阿里真实媒体；
4. 点击播放；
5. 确认出现阿里二维码；
6. 使用阿里云盘 App 扫码并确认；
7. 页面提示授权成功并刷新；
8. 再次点击同一媒体；
9. 确认获得实际直链并能够播放/开始读取媒体数据。

重点检查：

- 响应 `Set-Cookie` 中存在 `xy_byoa_aliyun`；
- Cookie 为 `HttpOnly`；
- HTTPS 下必须有 `Secure`；
- 前端 JS 不能读取明文 Access Token；
- `x_storages` 中没有写入该浏览器的 Access/Refresh Token；
- 服务日志没有打印 Access/Refresh Token；
- 阿里 Refresh Token 只在扫码确认请求内短暂使用，不落盘。

如果 `get_share_link_download_url` 当前上游已经改变：

- 保存 HTTP 状态、错误 code/message、请求路径；
- **不要记录 Authorization 值**；
- 优先做最小兼容修复，不恢复“服务器账号 + 转存”架构，除非已用真实证据证明免转存路径完全不可行。

---

# Phase C：真实夸克扫码 → 播放

使用干净浏览器 Profile A：

1. 进入真实 QuarkShare 媒体；
2. 点击播放；
3. 确认弹出夸克二维码；
4. 使用夸克 App 扫码并确认；
5. 确认写入 `xy_byoa_quark`；
6. 再次点击媒体；
7. 验证 `/file/download` 返回真实直链并能播放。

必须记录**加密后 Cookie 的字节长度**，但不要记录 Cookie 内容。

目标：

```text
<= 3500 bytes
```

如果超过：

- 当前代码应明确报 `credential cookie too large`；
- 不允许把夸克 Cookie 改成明文；
- 再设计安全的多 Cookie 分片方案，并补单元测试后再继续。

检查：

- 访客 Quark Cookie 不写入 `x_storages`；
- 不写入包级全局 Cookie；
- 不使用 `quarkUCShareLinkCache`；
- 服务日志不打印 Cookie。

---

# Phase D：A/B 浏览器隔离

这是 BYOA 最关键的真实验收之一。

准备：

```text
Browser A → 网盘账号 A
Browser B → 网盘账号 B
```

分别完成阿里/夸克授权（能使用不同测试账号时优先使用不同账号）。

验收：

1. A 的浏览器只有 A 自己的加密 Cookie；
2. B 的浏览器只有 B 自己的加密 Cookie；
3. A 播放后，B 的 Cookie/header 不变化；
4. B 播放后，A 不变化；
5. 服务器数据库没有 A/B 用户 Session；
6. 服务器 Storage 没有 A/B 私人 Token/Cookie；
7. A 的直链不会因为缓存被 B 直接复用；
8. 清除 A 的 Provider Cookie，只影响 A；B 仍可播放。

同时检查应用日志，禁止出现完整：

```text
Authorization
refresh_token
access_token
__puus
完整 Cookie header
```

---

# Phase E：凭据过期 / 失效重扫

阿里：

- 用测试方式使当前浏览器 `xy_byoa_aliyun` 对应 Access Token 无效/过期；
- 再点击媒体；
- 必须重新得到 `NEED_AUTH(aliyun)` 并弹二维码，而不是普通 500。

夸克：

- 使用失效的测试 Cookie 或退出测试账号；
- 再点击媒体；
- 明确登录/鉴权失效必须返回 `NEED_AUTH(quark)`；
- 分享失效、限流、普通上游错误不能全部误判成“重新扫码”。

---

# Phase F：Xiaoya if-newer 更新

记录：

```bash
docker exec xiaoya-byoa cat /opt/alist/data/xiaoya_data.version
docker exec xiaoya-byoa sha256sum /opt/alist/data/byoa_cookie.key
```

不要输出 admin 密码，只做 hash/行级对比。

然后：

1. 正常重启，版本没变时不重复 updateall；
2. 测试环境把 `xiaoya_data.version` 改成 `0.0.0`；
3. 重启；
4. 必须触发内容更新；
5. 更新后真实版本恢复；
6. BYOA key hash 不变；
7. admin 行不变；
8. 阿里/夸克真实目录仍可浏览；
9. 如果远端版本检查临时失败，已有站点仍应正常启动使用旧库。

---

# Phase G：Reality fallback 集成

只有 A-F 都通过后再做。

目标：

```text
公网 443
   ├── Reality 握手命中 → 原 Reality 服务
   └── 普通 HTTPS       → fallback → Xiaoya BYOA 127.0.0.1:5244
```

要求：

- 不改变 Reality 正常客户端流量；
- 普通浏览器访问 fallback 域名得到 Xiaoya 页面；
- 公网不直接暴露 5244；
- 反代传递 `X-Forwarded-Proto: https`；
- HTTPS 下 BYOA Cookie 必须有 `Secure`；
- 伪装站专用实例公网隐藏 `/@manage` 和 `/api/admin`；
- Reality 节点/中转节点故障行为不能因为 Xiaoya 管理端依赖而改变。

参考 `docs/BYOA_DEPLOY.md`。

---

# Phase H：安全终审

完成真实播放后执行一次只读检查。

数据库：

```bash
docker exec xiaoya-byoa sqlite3 /opt/alist/data/data.db \
  "select id,mount_path,driver,addition from x_storages;" \
  > /tmp/xiaoya-storage-audit.txt
```

**本地检查，不要把完整文件贴进聊天或 PR。**

确认不存在真实访客：

```text
Aliyun Access Token
Aliyun Refresh Token
Quark Cookie
```

再检查容器数据目录文件名与日志；如需要搜索，只输出“命中/未命中 + 文件名/行号”，不要输出凭据值。

---

## 修复规则

遇到问题时按以下优先级：

1. 修当前 BYOA 路径；
2. 补可复现测试；
3. 保持浏览器级凭据隔离；
4. 保持无 Redis/Session/用户系统；
5. 保持无服务器全局私人网盘账号；
6. 保持 BYOA Link 不进跨用户缓存。

不要因为一个 Provider 上游变化就整体退回原 Xiaoya 全局 Token 架构。

---

## 完成标准

只有全部满足才可以建议把 PR 从 Draft 转 Ready：

- GitHub Actions 全绿；
- 正式 `Dockerfile.byoa` 实机可构建/启动；
- 阿里真实扫码播放通过；
- 夸克真实扫码播放通过；
- A/B 浏览器隔离通过；
- 凭据失效重新扫码通过；
- `if-newer` 更新保护通过；
- Reality fallback 实机通过；
- 日志/数据库/数据目录无访客私人凭据泄漏。

即使全部通过，也**不要自动合并 main**。最后只提交验收报告与必要修复，等待用户决定是否合并。
