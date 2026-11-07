# 平台共享 Microsoft 会员登录

入口：**平台管理 → 第三方登录 → Microsoft**（`/admin/member-login?provider=microsoft`）。仅超级管理员后台会话可配置。所有站点共享一套应用凭据、总开关和身份绑定，会员组、订单与权限仍按站点区分。

## Microsoft Entra 配置

1. 在 [Microsoft Entra 管理中心](https://entra.microsoft.com/) 的应用注册中创建应用，受支持账号类型选择 **任何组织目录中的账号和个人 Microsoft 账号**。本实现使用公共云 `common` v2.0 端点，支持个人账号和工作／学校账号，不适用于中国世纪互联等独立云或自定义 B2C 租户。
2. 添加 **Web** 平台，把 GoCMS 后台列出的全部 `/api/auth/microsoft/callback` 地址加入重定向 URI，生产使用 HTTPS。不要选 SPA 平台。本实现采用服务端授权码流程与 PKCE，不需要启用隐式授权。
3. 在“证书和密码”创建客户端密码，将其 **值（Value）** 填入 GoCMS 的 Client Secret，而非 Secret ID。将应用程序（客户端）ID 填入 Client ID。密码只在创建时可查看，请在到期前更新；留空保存会保留原密钥。
4. 本实现只请求 `openid profile`，读取登录身份和姓名，不需要 `User.Read`、邮箱、文件、通讯录或 `offline_access`。不持久保存 Microsoft access/refresh token，不调用 Microsoft Graph 用户资料接口。
5. 登录加密主密钥沿用平台已有密钥，无需重新生成。Microsoft 密文独立于 Google、Facebook 及邮件配置作用域；备份、导入和启动校验支持此凭据。
6. 保存后通过站点公开域名中的后台点击“测试当前域名”。测试也可在总开关关闭时执行，不创建会员或会员会话。确认后开启总开关；未开启时 storefront 登录、注册、账号绑定与密码恢复入口均隐藏。

组织账号可能受其管理员的用户同意、应用准入和条件访问策略限制。发布者验证与组织管理员授权是不同事项，网站端无法绕过组织限制。填写应用品牌、支持邮箱、隐私政策和服务条款时应真实反映运营方。

## 账号与安全边界

- 校验 RS256 签名、精确租户 issuer、客户端 audience、有效期、v2.0 版本、nonce 和存在时的 azp。租户 ID 仅接受 UUID，不根据 token 中任意 URL 获取密钥，也不关闭 issuer 校验。
- `common` 返回租户专属 issuer。身份使用应用 ID 命名空间以及 `tid:sub`，不同租户中的相同 subject 不会被认作同一会员；更换 Client ID 不自动迁移已有身份。
- Microsoft 邮箱和用户名不是已验证邮箱证明。新会员不导入邮箱，也不与同邮箱的已有会员自动合并。已有会员应登录后在账号安全页主动绑定。
- state 绑定提供商、发起浏览器、实际站点、精确来源、配置版本和十分钟有效期，单次消费。修改 Microsoft 配置会使它的未完成授权失效，不影响其他提供商。
- 绑定需要本地密码或无密码账号的近期会话及新的 Microsoft 授权；解绑要求确认本地密码。共享绑定的增删对所有站点生效，各域名仍需分别建立会话。
- 找回密码可使用此前已绑定的 Microsoft 身份，不依赖邮件功能。未绑定身份不能认领账号、创建账号或加入其他站点。关闭提供商、配置变更或解绑会使相应的待用重置凭证失效。

## 接口

- `GET /api/auth/providers`：包含 `microsoft` 可用标志。
- `POST /api/auth/microsoft/start`：`next`、`bind`、`password`、`recover` 与其他提供商一致。
- `GET /api/auth/microsoft/callback`：验证授权并完成登录、绑定、恢复或管理员测试。
- `GET /api/v1/me/identities/microsoft`：绑定及本地密码状态。
- `DELETE /api/v1/me/identities/microsoft`：确认本地密码后解绑。
- 平台配置读取、保存、测试沿用 `/api/admin/member-login`、`/api/admin/member-login/test`，附加 `?provider=microsoft`。

## 验证

自动测试使用模拟令牌接口与签名密钥覆盖令牌校验、跨租户 issuer 拒绝、跨站共享、密钥隔离、总开关、回调重放、跨来源及跨提供商拒绝，以及绑定身份的密码恢复和撤销。生产联调仍需真实 Entra 应用凭据，分别验证个人与组织账号及各个公开域名。

官方参考：[OIDC](https://learn.microsoft.com/en-us/entra/identity-platform/v2-protocols-oidc)、[ID token 声明](https://learn.microsoft.com/en-us/entra/identity-platform/id-token-claims-reference)、[授权码流程](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow)。
