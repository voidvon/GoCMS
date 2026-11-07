# 平台共享 Facebook 会员登录

入口：**平台管理 → 第三方登录 → Facebook**（`/admin/member-login?provider=facebook`）。仅超级管理员后台会话可配置。Google 和 Facebook 使用独立 Tabs，各自保存总开关、凭据和自动注册策略；同一家提供商的配置由全部站点共享，不随当前站点选择改变。

## 配置

1. 在 Meta 开发者控制台创建适用于网页登录的应用，并配置 Facebook Login。
2. 配置站点域名、隐私政策、数据删除说明或回调等控制台要求的信息，按应用实际要求完成权限、审核及上线设置。开发模式下先使用具有应用角色的测试账号联调。
3. 在后台 Facebook Tab 填写 **App ID**（数字）和 **App Secret**。登录加密主密钥沿用 Google 的同一份服务级密钥，无需重新生成；Secret 加密作用域按提供商隔离，界面不回传密钥。
4. 将后台列出的全部回调地址加入这一个 Meta 应用的有效 OAuth 重定向 URI：`https://site.example/api/auth/facebook/callback`。新增站点域名后补充白名单，无需再配置一套 App ID / Secret。是否允许这些域名和品牌组合，仍以 Meta 控制台的校验和审核结果为准。
5. 保存后点击“测试当前域名”，完成真实授权。总开关关闭时也能测试，不会创建会员或会员会话。
6. 开启 Facebook 总开关。未开启时，storefront 登录页、注册页和账号安全页均隐藏 Facebook 入口；Google 的状态不受影响。

本站代码固定使用 Graph API `v24.0`，仅请求 `public_profile`，读取用户 `id,name`，不申请邮箱权限。版本升级需更新服务端适配器并验证 Meta 行为。

## 账号与安全

- 后端交换授权码后，通过 `debug_token` 检查有效性、所属 App ID、用户 ID、过期时间及数据访问过期时间，再使用 `appsecret_proof` 调用 `/me`，核对返回 ID。
- 一次性 state 绑定发起浏览器、提供商、精确来源、实际站点、配置版本和十分钟有效期。跨站回调、跨提供商回调和重放被拒绝。修改某一提供商的配置只撤销该提供商的未完成授权。
- Facebook 使用 App ID 与应用内用户 ID 建立身份；同一平台凭据下同一 Facebook 账号跨站复用同一会员，各站默认会员组分别分配，不复制其他站点权限。
- Facebook 不提供 Google 的 `email_verified` 声明。新会员以无邮箱、无本地密码创建，不会用 Facebook 返回邮箱匹配或合并已有会员。已有会员应先使用原有方式登录，在 `/shop/account` 绑定 Facebook；Google 与 Facebook 不自动合并。
- 绑定时需要本地密码，或无密码账号的近期会话及新的提供商授权。解绑必须确认本地密码，无本地密码时保持绑定，避免失去账号访问方式。绑定和解绑在所有站点生效。
- Meta App ID 更换会改变身份命名空间，应先评估已有绑定及账号迁移；不要把更换应用视为普通 Secret 轮换。
- 跨域凭据共享不代表跨域 Cookie SSO。新域名仍需点击登录，之后使用同一会员。

## 接口与验证

配置读取、保存与授权测试使用 `/api/admin/member-login`、`/api/admin/member-login/test` 并附带 `?provider=facebook`；密钥生成、导入及备份接口和 Google 共用。公开接口：

| 接口 | 用途 |
| --- | --- |
| `GET /api/auth/providers` | 返回独立的 `google`、`facebook` 可用标志 |
| `POST /api/auth/facebook/start` | 开始登录或绑定，JSON 字段 `next`、`bind`、`password` |
| `GET /api/auth/facebook/callback` | 消费授权事务、校验身份并创建本站会话 |
| `GET /api/v1/me/identities/facebook` | 平台绑定状态及是否有本地密码 |
| `DELETE /api/v1/me/identities/facebook` | 确认本地密码后解除平台绑定 |

自动测试通过模拟 Meta HTTP 接口验证令牌校验、身份匹配、跨站共享、配置和身份隔离、总开关、重放及跨来源拒绝。上线前仍需填入真实凭据，在 Meta 开发及上线环境完成联调。

官方参考：[手动登录流程](https://developers.facebook.com/docs/facebook-login/guides/advanced/manual-flow/)、[安全 Graph API 请求](https://developers.facebook.com/docs/graph-api/guides/secure-requests/)。当前自动抓取官方页面遇到 HTTP 429，具体控制台审核条件需以实际应用显示为准。
