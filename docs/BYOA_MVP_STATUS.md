# Xiaoya BYOA MVP 状态

## 项目目标

本项目只服务于一个轻量目标：把 Xiaoya/PowerList 变成一个可真实浏览、真实扫码、真实播放的影视伪装站。

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

1. Xiaoya 内容数据继续跟随 Xiaoya 上游更新，不维护静态内容副本。
2. 访客浏览内容时不要求注册本站账号。
3. 点击播放时才检查对应网盘凭据。
4. 浏览器 A 必须只使用 A 自己扫码得到的凭据；浏览器 B 同理。
5. 不把访客网盘凭据写入 PowerList Storage、账号池或全局 Token 表。
6. BYOA 播放路径不得复用只按 fileID 建 Key 的账号相关播放直链缓存。
7. 优先复用现有 Share Driver，不重新实现网盘协议。

## 当前开发分支

`feature/byoa-mvp`

Draft PR：#1

## 已完成

### 1. 浏览器级凭据解析底座

新增：

- `internal/byoa/credential.go`
- `internal/byoa/credential_test.go`

当前 Provider：

- `aliyun`
- `quark`

浏览器 Cookie 名称：

- `xy_byoa_aliyun`
- `xy_byoa_quark`

提供能力：

- 从当前 HTTP Header 读取对应浏览器凭据
- 不读取服务器 Session / Redis
- 缺少凭据时返回 `BYOA_AUTH_REQUIRED:<provider>`
- 对夸克原始 Cookie 做 URL 编码/解码，避免内部 `;` 被浏览器 Cookie 语法截断
- 单元测试覆盖 A/B 浏览器凭据隔离

### 2. 夸克 BYOA 最小播放路径

新增：

- `drivers/quark_uc_share/byoa.go`
- `drivers/quark_uc_share/byoa_test.go`

修改：

- `drivers/quark_uc_share/driver.go`

当前行为：

```text
QuarkShare Link
  -> 从当前请求读取 xy_byoa_quark
  -> 没有：BYOA_AUTH_REQUIRED:quark
  -> 有：使用该 Cookie 走分享免转存 /file/download
  -> 返回播放直链
```

该 BYOA 分支位于以下逻辑之前：

- 服务器 Quark 账号池
- `quarkUCShareLinkCache`
- 转存临时目录
- 多账号分片
- 123 秒传

因此 MVP 下夸克不会把 A 浏览器的账号相关直链缓存后给 B 浏览器复用。

BYOA 专用请求函数不会把响应中的 `__puus` 写回原有包级 `shareCookie`，避免访问者账号状态污染服务器全局状态。

## 夸克后续待验证

需要真实环境验证：

1. Xiaoya 的 QuarkShare 在没有服务器 `quark_cookie.txt` 时是否仍能完成公开目录 List。
2. 若实时 List 仍强依赖账号，则公共浏览改用 Xiaoya `index.zip` 的夸克索引，播放时再走 BYOA。
3. 实际扫码得到的夸克 Cookie 长度是否稳定小于浏览器单 Cookie 限制；若超过，再做最小拆分方案。
4. `BYOA_AUTH_REQUIRED:quark` 在现有 API 错误响应中的前端表现。

## 阿里当前审计结果

当前 PowerList 的 `AliyundriveShare2Open` 播放会寻找注册的 `AliyundriveOpen` 账号，并通过个人账号转存分享文件后获取 Open API 直链。

当前 `AliyundriveOpen.Init()` 同时执行：

1. `RefreshAliToken(false)`：普通阿里 Refresh Token
2. `refreshToken(false)`：Open API Refresh Token
3. 校验两类 Token 属于同一账号
4. 获取 DriveId
5. 创建临时目录

因此当前实现不是“只给一个 Open Token 就能完整初始化”。

Xiaoya 当前公开的 `glue_python/aliyuntvtoken/alitoken2.py` 扫码流程只明确写入：

- `myopentoken.txt`

普通 `mytoken.txt` 的获取实现并未在该目录公开，`glue_python/aliyuntoken/README.md` 明确说明相关脚本加密、源代码未公开。

### 阿里下一步必须先解决的问题

在修改阿里 Driver 前，先确定以下最小方案之一：

A. 一次扫码能够安全获得当前 PowerList 所需的两类 Refresh Token；或

B. 改造阿里分享播放链，使 BYOA 只依赖 Open API Token；或

C. 找到不需要完整 `AliyundriveOpen.Init()` 的现有分享直链路径。

在这个问题确认前，不要为了赶进度把访客 Token 临时写进全局 `x_storages`，也不要覆盖服务器全局账号。

## Xiaoya 内容更新方向

最终结构保持：

```text
Xiaoya 上游 data / update.zip / index.zip
        -> 内容更新
        -> BYOA postprocess
        -> 本项目 PowerList BYOA Core
```

不要 fork 一份 Xiaoya Data 长期手工维护。

原版 Xiaoya 更新逻辑中存在“没有服务器 Quark Cookie 就删除 QuarkShare”等行为，后续 bootstrap/updater 必须绕过这些账号绑定动作，但保留内容更新。

## 测试状态

仓库已建立 Draft PR #1 以触发现有 `Test Build` workflow。

当前 ChatGPT 执行环境无法解析 `github.com`，因此不能在本地容器直接 clone 后运行 `go test`。优先依赖 GitHub Actions；若 fork 上 Actions 未启用，需要在 GitHub 开启后再验证完整构建。

## 下一开发顺序

1. 让 GitHub Actions 验证当前基础代码编译。
2. 真实部署一份 Quark BYOA，验证扫码 Cookie -> 播放直链闭环。
3. 验证无服务器夸克账号时的目录浏览；必要时接 Xiaoya index。
4. 解决阿里单次扫码凭据模型。
5. 接阿里 BYOA Link。
6. 做 Xiaoya bootstrap/updater 的最小兼容层。
7. A/B 两浏览器隔离验收。
8. Reality fallback 集成测试。
