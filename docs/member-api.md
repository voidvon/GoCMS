# 前台会员 API v1

本次范围：账号、个人资料、会员状态、密码及会话。没有增加页面、支付、内容访问控制。

## 架构

HTTP 适配位于 internal/site/member_api.go；账号、密码、资料、会员有效期、
会话操作位于 internal/member。服务独立于模板、HTTP 和后台管理员身份。
当前 SQL 封装在该业务包内部，使用现有数据库连接；不引入额外 ORM 或微服务。
旧 /api/auth/* 保留原 user/ok 成功结构，并复用账号服务。
v1 成功响应为 {"data": ...}，失败为 {"error":{"code":"...","message":"..."}}。
调用方应按 code 处理错误，不匹配 message。

## 认证与部署

浏览器使用服务端会话，Cookie gocms_user，HttpOnly、SameSite=Lax，有效期30天；
Go 直接处理 HTTPS 时设置 Secure。不支持把后台 API Key 当用户身份。
推荐把前端与 API 通过反向代理部署在同一 Origin；HTTPS 终止在代理时，
由代理为 Cookie 强制添加 Secure。程序不盲信 X-Forwarded-*。
目前不开放跨源 CORS，也不支持 Bearer/JWT。不同源的项目需配置同源代理；
不应把这种部署要求误解为已支持任意跨域 Cookie 登录。

写请求需要 application/json（DELETE 无请求体除外），校验 Origin/
Sec-Fetch-Site，拒绝跨站。无 Origin 的非浏览器客户端允许访问，但仍需会话。
请求体上限16KiB，敏感响应 Cache-Control: no-store。
注册/登录/改密按 IP 和账号各自限制15分钟内10/20/10次，包括成功尝试。
反向代理下 IP 当前为连接来源，需考虑共享额度；没有信任任意代理头。
密码8–1024字节。用户名3–64字节，不含 @ 或空白。
注册邮箱仅作为登录标识，尚未验证，不能用于找回密码或作为已验证身份。

## 接口

| 方法 | 路径 | 请求/行为 |
| --- | --- | --- |
| POST | /api/v1/auth/register | username,password,email（可选）；201 data.user，不自动登录 |
| POST | /api/v1/auth/login | identifier 或 username,password；200 data.user，设置 Cookie |
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
达到上限返回 HTTP 409：
`{"error":{"code":"session_limit_reached","message":"登录会话已达上限，请退出其他会话或联系管理员"}}`。
失败不删除原会话、不覆盖 Cookie。旧 /api/auth/login 同样执行此规则。

退出、到期或撤销释放名额。关闭浏览器不释放；清除 Cookie 后旧会话仍占名额，
可从另一有效会话使用撤销 API，或联系管理员。新客户端不会获得管理旧会话的临时凭据。
降低上限不会踢掉既有会话；如果既有数量超限，重复登录也会被拒绝，直到撤销到上限以内。
同时提交登录会按事务串行检查名额；同一个旧 Token 的并发登录不能重复抵扣名额。

后台 PATCH /api/admin/site-users 支持：
- {"id":123,"max_sessions":2}：调整上限，不修改账号状态。
- {"id":123,"revoke_sessions":true}：撤销全部会话，不禁用账号。
上述操作仅限超级管理员登录会话，不允许前台账号或 API Key 操作。
