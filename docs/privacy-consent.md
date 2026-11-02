# 隐私与 Cookie 同意

GoCMS 提供站点级同意策略、语言级启用覆盖、多语言文案、用途选择、策略版本和前台资源加载控制。不使用语言、域名或路径推断访客所在地。功能默认关闭，不会修改已有主题及用户数据。

## 后台配置

在“多站点管理”中点击站点的盾牌按钮，或在“多语言”中打开“隐私与 Cookie”。语言行的盾牌按钮直接打开该语言的覆盖配置。

配置范围分为“站点默认”和每个语言版本。站点默认提供总开关、策略版本、选择有效期（1–365 天，默认 180 天）及实际使用的可选用途；语言版本支持继承、开启或关闭，并覆盖文案和政策链接。语言定义是全局共享的，但同意配置和语言覆盖按站点隔离。

文案优先级：语言配置 → 站点配置 → 内置翻译。内置简体中文和英语，其他语言需要管理员提供翻译。链接支持站内绝对路径或 HTTP(S) 地址，不自动根据语言前缀改写。

保存配置后必须执行**全站发布**。单条内容发布只更新相关页面，不能替代策略变更后的全站发布。发布过程在输出目录切换前校验配置和用途标记，失败时保留当前已发布页面。

## 主题接入

开启策略后，生成器自动为主题页面、分页、详情页、404 和 HTML 站点地图添加默认界面及运行时，无需复制弹窗到每个模板。搜索页等由模板生成的页面同样适用。核心提供状态与加载机制，主题可配置文案、定制样式和增加入口。

必须先盘点主题和第三方服务。**未标记的脚本、嵌入或网络请求保持原有行为，无法由一个弹窗自动识别或阻止。** 必要的登录、购物车、安全等资源不应误标为可选用途；普通统计、营销及其他可选资源按实际用途分类。

对可选资源使用以下标记：

```html
<script data-consent="analytics" src="https://analytics.example/script.js"></script>
<script data-consent="analytics">
  // 依赖上方统计脚本的初始化代码，也需要同一用途标记。
</script>
<script data-consent="preferences" type="module" src="/js/optional.js"></script>
<iframe data-consent="marketing" src="https://video.example/embed/123" title="Video"></iframe>
<img data-consent="marketing" src="https://ads.example/pixel.gif" alt="">
```

支持 `preferences`、`analytics`、`marketing` 三种用途，且只有后台启用并获得同意的用途才会加载。用途未启用时，即使曾同意也不会执行。`data-consent` 仅支持 `script`、`iframe` 和 `img`；错误用途、不支持的元素或重复资源属性会导致发布失败。

生成器在输出 HTML 时把这些资源的 `src`、`srcset`、`srcdoc` 转为惰性属性，并把脚本类型变为 `text/plain`，因此浏览器预加载扫描器也不能提前加载。原始脚本类型（包括 `module`）保留，同意后按文档顺序加载脚本；显式 `async` 脚本保留异步行为。需要必要脚本初始化的资源，应由主题安排正确依赖和就绪时机。不要使用 `document.write` 型旧追踪代码。

不要在其他地方同时预加载、预连接或加载相同追踪服务，也不要在未受控脚本、CSS、事件属性、`noscript` 内容或网络代理中绕过用途控制。静态标记不支持把用途放在父容器上来控制整个子树。

动态插入的资源必须在插入 DOM **之前**使用惰性属性；运行时不能撤回已经发出的请求：

```javascript
const script = document.createElement("script")
script.type = "text/plain"
script.dataset.consent = "analytics"
script.dataset.consentSrc = "https://analytics.example/script.js"
document.body.append(script)
```

## 状态与撤回

界面同时显示“全部接受”“拒绝非必要项”和“自定义设置”。可选项默认关闭，拒绝后不会影响基本页面浏览。底部常驻“隐私设置”入口允许重新选择；主题还可以添加：

```html
<button type="button" data-consent-open>隐私设置</button>
```

关闭某语言的同意界面不会把未选择视为接受。该语言下已标记资源仍遵守同一站点的已有选择，没有有效选择时保持停用，避免切换语言绕过拒绝。

状态保存在当前浏览器的 `localStorage`，键为 `gocms:consent:<site_id>`，包括策略版本、修订标识、选择时间和各用途布尔值。相同源、相同站点的语言版本共享选择，不同站点隔离；不同域名（包括语言独立域名）不跨域共享。无有效选择、记录过期、版本变化或用途/有效期变化时重新征求同意。文案翻译变化不会单独使选择失效；实际处理内容变化时应主动更新策略版本。

存储不可用时，选择只在当前页面有效，再次访问会重新询问，不会默认为同意。运行时监听跨标签页的选择变化。撤回已启用用途时，先发布状态事件，再刷新当前页以终止已加载脚本和嵌入；后续页面不再加载被撤回用途。有效期在页面长时间打开时也会检查。

需要清理服务已写入的 Cookie、存储或订阅时，主题必须接入对应服务的退出/清理 API。GoCMS 不会尝试跨域删除第三方 Cookie，也不会误删登录 Cookie。

```javascript
window.addEventListener("gocms:consent-change", event => {
  const choices = event.detail
  // 按对应服务文档更新其同意状态，撤回时清理该用途的第一方 Cookie/存储。
})
window.addEventListener("gocms:consent-ready", event => {
  // 首次加载或返回页面时获取状态。
})
// 运行时就绪后亦可直接查询：
window.GoCMSConsent.get() // necessary/preferences/analytics/marketing/decided/version/revision/time
window.GoCMSConsent.has("analytics")
window.GoCMSConsent.open()
window.GoCMSConsent.reject()
window.GoCMSConsent.save({ analytics: true, marketing: false, preferences: false })
```

`accept()`、`reject()` 和 `save()` 仅应用于访客主动选择，不应由主题启动代码自动调用。

## 主题展示接口

模板函数 `{{consent}}` 返回当前语言已解析的结构化策略，字段为 `Enabled`、`Version`、`Revision`、`MaxAgeDays`、`Purposes`、`Text`、`SiteID`、`Language`。当前模板引擎采用 `text/template`，主题自行输出文本时仍需按已有规则转义。

默认界面使用独立 Shadow DOM，避免主题的按钮和正文样式影响可读性。主题可通过 CSS 变量调整外观：

```css
gocms-consent {
  --consent-bg: #fff;
  --consent-text: #18222c;
  --consent-border: #cbd5df;
  --consent-accent: #243f53;
  --consent-accent-text: #fff;
  --consent-radius: 12px;
}
gocms-consent::part(panel) { /* 面板样式 */ }
gocms-consent::part(button) { /* 维持接受与拒绝同等可见 */ }
gocms-consent::part(launcher) { /* 常驻入口位置 */ }
```

运行时和配置随生成的页面输出，无需额外公开主题源目录或依赖后台接口。若部署使用限制内联脚本的 CSP，需要为生成的运行时配置允许的哈希/nonce；不能仅允许弹窗而阻止运行时执行。

## 范围

此功能提供 Cookie 和可选追踪的同意管理能力。是否需要征求同意、用途描述、具体 Cookie 清单、第三方服务处理和政策内容仍需根据实际业务配置。只使用严格必要 Cookie 的网站不必为了这些 Cookie 征求可选用途同意。

本地选择记录用于执行浏览器偏好，不是服务器端的不可篡改同意证据库。会员数据、反馈表单、服务器访问日志和 IP 保存期限应分别确定处理依据及披露方式，不由这个弹窗统一授权。
