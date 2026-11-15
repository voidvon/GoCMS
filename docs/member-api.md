# 前台会员 API v1

本次范围：账号、个人资料、会员状态、密码及会话。没有增加页面、支付、内容访问控制。

## 架构

认证分为三个职责：

- `internal/member`：账号验证、会员有效期和会话业务。密码及第三方登录接受具名输入，
  返回 `LoginResult`；`sessions.go` 统一负责会话生成、摘要存储、有效期、数量限制、轮换和撤销。
- `internal/site/member_credentials.go`：解析 Cookie / Authorization，统一认证为会员身份及公开会话 ID，
  并负责凭据传输。受保护接口只认证一次，后续操作使用同一个身份和凭据来源。
- `internal/site/member_auth.go`、`member_api.go`：请求校验、调用业务服务和编码响应。
  v1 直接调用共用的业务操作；旧 `/api/auth/*` 只保留 `user/ok` 成功结构的兼容编码，
  不通过捕获、解析旧 HTTP 响应实现新接口。

服务独立于模板、HTTP 和后台管理员身份，SQL 封装在业务包内部，使用现有数据库连接。
会话签发返回 `SessionGrant`，包含凭据及实际保存的到期时间；Cookie 到期时间和 Bearer
`expires_in` 均由它计算。业务结果禁止自动 JSON 输出凭据，由传输层显式决定交付方式。
资料、有效会员组、站点加入和会话轮换在登录事务内一起准备、提交；失败会回滚，
不会占用新会话名额或销毁被替换的有效会话。

账号是平台共享身份；会员组仍按请求所属站点隔离。配置了匹配域名时，域名决定整个请求
使用的站点，查询参数不能切换到其他站点；本地工具没有匹配域名时继续兼容显式站点选择。
登录及旧会话接口的 groups 只包含当前有效会员组，与会员明细接口使用相同的有效期判断。

延续不透明服务端会话，以便现有退出、管理员撤销及会话数量限制立即生效；
不增加独立 JWT、刷新凭据、会话表或另一套过期策略。
v1 成功响应为 {"data": ...}，失败为 {"error":{"code":"...","message":"..."}}。
调用方应按 code 处理错误，不匹配 message。

## 认证与部署

浏览器使用服务端会话，Cookie gocms_user，HttpOnly、SameSite=Lax，有效期30天；
Go 直接处理 HTTPS 时设置 Secure。桌面端等非浏览器客户端可使用
`POST /api/v1/auth/token` 获取会员 Token，后续通过
`Authorization: Bearer <access_token>` 访问会员接口，无需接收或发送 Cookie。
Token 是随机、不透明的服务端会话凭据，不是 JWT，也不提供 refresh_token；
签发时固定有效期30天，`expires_in` 为响应时的剩余整秒数，过期后重新登录。Cookie 和 Bearer 共用会话、账号状态、
登录数量限制和撤销机制。不支持把后台 API Key 当用户身份，会员 Token 也无后台管理权限。
推荐把前端与 API 通过反向代理部署在同一 Origin；HTTPS 终止在代理时，
由代理为 Cookie 强制添加 Secure。程序不盲信 X-Forwarded-*。
目前不开放跨源 CORS；Bearer 支持不改变浏览器的跨域限制。不同源的浏览器项目需配置同源代理；
不应把这种部署要求误解为已支持任意跨域 Cookie 登录。

写请求需要 application/json（DELETE 无请求体除外），校验 Origin/
Sec-Fetch-Site，拒绝跨站。无 Origin 的非浏览器客户端允许访问，但仍需会话。
请求体上限16KiB，敏感响应 Cache-Control: no-store。
注册/登录/改密按 IP 和账号各自限制15分钟内10/20/10次，包括成功尝试。
两种登录接口共享登录限流额度。
反向代理下 IP 当前为连接来源，需考虑共享额度；没有信任任意代理头。
密码8–1024字节。用户名3–64字节，不含 @ 或空白。
注册邮箱仅作为登录标识，尚未验证，不能用于找回密码或作为已验证身份。

## 接口

| 方法 | 路径 | 请求/行为 |
| --- | --- | --- |
| POST | /api/v1/auth/register | username,password,email（可选）；201 data.user，不自动登录 |
| POST | /api/v1/auth/login | identifier 或 username,password；200 data.user，设置 Cookie |
| POST | /api/v1/auth/token | 同登录请求；200 data.user、access_token、token_type、expires_in，不设置 Cookie |
| POST | /api/v1/auth/logout | {}；撤销当前会话，data.ok |
| GET | /api/v1/me | data 为用户资料，未登录401 |
| PATCH | /api/v1/me | display_name、avatar_url，可单独修改；返回资料 |
| PUT | /api/v1/me/password | old_password,new_password；撤销所有会话，data.reauthenticate=true |
| GET | /api/v1/me/memberships | data 为会员明细数组，包括失效记录 |
| GET | /api/v1/me/sessions | data 为未过期会话数组 |
| DELETE | /api/v1/me/sessions/{id} | 只允许撤销自己会话，不存在/不属于自己返回404 |

/me 不允许修改 id、username、email、status、groups。头像允许完整 http/https URL，
此接口不抓取远程内容。邮箱变更需未来单独的验证流程。

会员明细含 group_id、name、slug、started_at、expires_at、status。
时间输出 UTC RFC3339，永久 expires_at=null。status 是实时计算的
active、scheduled、expired、disabled；不依赖定时任务，也不把 vip 名称写死。
历史数据库时间按 UTC 解析；后台旧接口仍接受既有 SQL 时间格式，
新调用方应传带时区的 RFC3339。无效时间会拒绝。

会话返回 id、created_at、expires_at、ip、user_agent、current，
id 不是认证凭据，不暴露 Token 或 Token 摘要。
改密需重新登录；成功改密写入 gocms_user_event，不记录密码或请求体。

会员认证优先使用显式 Authorization；无效、空白、重复或非 Bearer 的该请求头
返回401，不回退使用 Cookie。无 Authorization 时兼容原 Cookie 登录。
不接受 URL 查询参数或表单中的 Token。未认证响应带 WWW-Authenticate: Bearer；
无效 Bearer 凭据附 error="invalid_token"。
Bearer 退出或撤销当前会话不修改浏览器 Cookie；改密仍会撤销该账号全部会话。
Bearer 密码重置也不修改浏览器 Cookie。互动式第三方登录和绑定保留浏览器 Cookie / OAuth
状态校验；绑定入口拒绝使用 Bearer 身份发起浏览器绑定事务。第三方绑定状态查询及解除绑定支持 Bearer。
后端故障返回5xx，不伪装为401，客户端不应因后端临时故障清除有效凭据。

常用错误码：invalid_input、authentication_required、invalid_credentials、
account_conflict、not_found、rate_limited、origin_not_allowed、
unsupported_media_type、method_not_allowed、internal_error。

## 浏览器调用

```js
await fetch("/api/v1/auth/login", {
  method: "POST",
  credentials: "same-origin",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ username: "alice", password: "your-password" })
})
const response = await fetch("/api/v1/me", { credentials: "same-origin" })
const payload = await response.json()
if (!response.ok) throw new Error(payload.error.code)
console.log(payload.data.display_name)
```

## 桌面客户端调用

通过 HTTPS 提交账号密码：

```http
POST /api/v1/auth/token
Content-Type: application/json

{"username":"alice","password":"your-password"}
```

成功响应示例（省略部分用户字段）：

```json
{"data":{"user":{"id":123,"username":"alice","display_name":"alice"},"access_token":"<随机会话凭据>","token_type":"Bearer","expires_in":2591999}}
```

后续请求使用响应中的 access_token：

```http
GET /api/v1/me
Authorization: Bearer <access_token>
```

退出调用 `POST /api/v1/auth/logout`，携带相同 Authorization、JSON 请求体 `{}`。
客户端应将 Token 作为密码级凭据保存，不写入 URL 或日志；收到401后清除失效凭据并重新登录。
服务端数据库仅保存 Token 摘要，Token 接口仅在登录成功时返回新的明文凭据。

OpenAPI: member-api.openapi.json。

## 后续边界

后台会员组接口仍保留历史混合请求协议；尚未迁移为独立资源式路由。
不宣称已经完成后台会员操作审计、邮箱验证、找回密码、跨域配置管理。
日志保留/清理策略也需另行配置，当前限流记录持久化保存。

## 登录会话数量限制

每个前台账号的 max_sessions 默认 2，管理员可以在“会员管理 → 登录会话”
设置为 1–100，或撤销该账号的全部会话。旧账号升级自动补齐默认值。
此限制不是设备数，也不是实时在线人数，不绑定机器码。

登录请求携带同账号有效 gocms_user Cookie 时，事务内替换会话并轮换 Token。
旧 Token 立即失效；客户端必须接收并保存新的 Set-Cookie。
没有有效 Cookie（包括已过期、其他账号 Cookie）视为新登录。
Token 登录接口只通过 Authorization 判断是否替换当前会话，忽略 Cookie；
携带同账号有效 Bearer Token 时可在上限内轮换会话，必须保存响应的新 access_token。
未携带有效的同账号 Token 则视为新登录，不会挤掉浏览器或其他客户端的会话。
达到上限返回 HTTP 409：
`{"error":{"code":"session_limit_reached","message":"登录会话已达上限，请退出其他会话或联系管理员"}}`。
失败不删除原会话、不覆盖 Cookie。旧 /api/auth/login 同样执行此规则。

退出、到期或撤销释放名额。关闭程序不释放；清除 Cookie 或丢失 Token 后旧会话仍占名额，
可从另一有效会话使用撤销 API，或联系管理员。新客户端不会获得管理旧会话的临时凭据。
降低上限不会踢掉既有会话；如果既有数量超限，重复登录也会被拒绝，直到撤销到上限以内。
同时提交登录会按事务串行检查名额；同一个旧 Token 的并发登录不能重复抵扣名额。

后台 PATCH /api/admin/site-users 支持：
- {"id":123,"max_sessions":2}：调整上限，不修改账号状态。
- {"id":123,"revoke_sessions":true}：撤销全部会话，不禁用账号。
上述操作要求目标站点的会员管理权限，支持后台会话或 API Key；站点归属等账号管理约束保持生效，前台账号无权操作。

## Google 登录与绑定

Google 授权、后台配置与开关、会员绑定规则见 [Google 会员登录](google-login.md)。会员会话沿用本页现有接口，Google 凭据不返回 storefront。

密码修改、邮件找回及绑定第三方账号验证流程见 [member-password.md](member-password.md)。`GET /api/v1/me` 另返回 `has_password`。
