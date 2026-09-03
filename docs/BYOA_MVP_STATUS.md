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
8. MVP 优先采用免转存直链；只有真实环境证明不可用时才考虑恢复转存链。

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

### 2. 结构化 NEED_AUTH API

新增：

- `server/common/byoa.go`
- `server/common/byoa_test.go`

修改：

- `server/common/common.go`

所有通过 `common.ErrorResp` 返回的 `AuthRequiredError` 会自动转换成：

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

阿里同理返回 `provider: aliyun`。

前端后续不需要解析错误字符串，只判断 `data.byoa_auth_required` 并按 provider 弹对应二维码。

### 3. 夸克 BYOA 最小播放路径

新增：

- `drivers/quark_uc_share/byoa.go`
- `drivers/quark_uc_share/byoa_test.go`

修改：

- `drivers/quark_uc_share/driver.go`

当前行为：

```text
QuarkShare Link
  -> 从当前请求读取 xy_byoa_quark
  -> 没有：NEED_AUTH(quark)
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

### 4. 阿里 BYOA 最小播放路径

新增：

- `drivers/aliyundrive_share2_open/byoa.go`
- `drivers/aliyundrive_share2_open/byoa_test.go`

修改：

- `drivers/aliyundrive_share2_open/driver.go`

审计发现仓库仍保留旧 `AliyundriveShare` 的直接分享取链接口：

`https://api.alipan.com/v2/file/get_share_link_download_url`

因此 MVP 不再走原 `AliyundriveShare2Open -> AliyundriveOpen -> 转存临时目录 -> Open API 取链` 的双 Token 重型路径。

当前 BYOA 行为：

```text
AliyunShare Link
  -> 从当前请求读取 xy_byoa_aliyun
  -> 没有：NEED_AUTH(aliyun)
  -> 有：作为当前浏览器的普通 Aliyun Access Token
  -> 使用公开 ShareToken + 分享 drive_id
  -> get_share_link_download_url
  -> 返回分享直链
```

该路径：

- 不使用服务器 AliyundriveOpen 账号池
- 不把访客 Token 写入 Storage
- 不转存到访客个人网盘
- 不创建临时目录
- 不使用 `aliyundriveShareLinkCache`
- 不需要 Open API Refresh Token

MVP 中浏览器保存的是短期普通 Aliyun Access Token。Access Token 失效时直接重新返回 NEED_AUTH(aliyun)，不做服务端 Refresh Token 生命周期系统。

## 待真实环境验证

### 夸克

1. Xiaoya 的 QuarkShare 在没有服务器 `quark_cookie.txt` 时是否仍能完成公开目录 List。
2. 若实时 List 仍强依赖账号，则公共浏览改用 Xiaoya `index.zip` 的夸克索引，播放时再走 BYOA。
3. 实际扫码得到的夸克 Cookie 长度是否稳定小于浏览器单 Cookie 限制；若超过，再做最小拆分方案。
4. 当前 `/file/download` 免转存接口在真实 Quark 分享上的稳定性。

### 阿里

1. `get_share_link_download_url` 在当前阿里 API 环境中是否仍能正常取得分享直链。
2. `byoaShareDriveID()` 从公开分享根目录取得 drive_id 是否覆盖 Xiaoya 当前分享数据。
3. 扫码层如何以最少代码取得普通 Aliyun Access Token。
4. Access Token 实际有效期是否足够用于伪装站场景。

如果旧分享直链接口在真实环境失效，再回退研究现有 `AliyundriveShare2Open` 的转存/Open API 路线；MVP 阶段不提前增加该复杂度。

## 扫码层尚未实现

后端播放链目前已经具备“当前浏览器有凭据就播放、没凭据就 NEED_AUTH”的接口。

下一步扫码层只需要完成：

```text
NEED_AUTH(provider)
  -> 前端弹对应二维码
  -> 扫码完成
  -> 将最终播放凭据写入当前浏览器 Cookie
  -> 自动重试刚才的 /api/fs/get
```

不新增用户表、Redis 或 Session 数据库。

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

目前已添加的纯逻辑测试覆盖：

- 浏览器凭据解析
- 阿里/夸克 Provider 区分
- A/B 浏览器凭据隔离
- 阿里缺凭据立即 NEED_AUTH
- 夸克缺凭据立即 NEED_AUTH
- API 层 AuthRequiredError -> 结构化 401 响应

## 下一开发顺序

1. GitHub Actions 验证当前代码完整编译。
2. 做最小前端 BYOA 弹窗/扫码入口，不 fork 整套 OpenList 前端。
3. 接夸克现成扫码流程并写入当前浏览器凭据。
4. 接阿里最小扫码流程并写入短期 Access Token。
5. 真实部署验证 Aliyun / Quark 播放直链。
6. 验证完全无服务器私人 Token 时的 Xiaoya 内容浏览。
7. 做 Xiaoya bootstrap/updater 最小兼容层。
8. A/B 两浏览器隔离验收。
9. Reality fallback 集成测试。
