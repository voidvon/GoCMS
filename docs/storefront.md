# 独立购物前台

`storefront/` 是 Vite + React + TypeScript 应用，使用与后台一致的 shadcn Base UI Nova。它只负责购物车、结算、订单和付款结果，以及独立的会员登录、注册。商品列表、详情和规格选择复用主题生成的 HTML，React 不提供商品展示列表。

## 架构与目录

| 层 | 职责 |
| --- | --- |
| `storefront/src/pages/` | 购物车、结算、订单、登录注册及加入购物车的交接入口 |
| `storefront/src/components/app/` | 会员表单、反馈和购物业务组件 |
| `storefront/src/i18n/` | 语言目录、语言解析与持久化、日期和数字格式化 |
| `storefront/src/lib/` | API、路由、会员状态、购物车与异步数据加载 |
| `frontend/src/components/ui/` | 两个应用复用的官方 shadcn Base UI 源码，保持原样 |
| Go `internal/commerce` | 内容规格、金额计算、订单明细快照、支付与交付状态 |
| Go `internal/payment` | 支付商接口及 PingPong 适配器 |

购物前台通过 Vite、TypeScript 别名读取官方 UI，业务代码与后台独立，Vite 对 React 等共享依赖去重，避免加载多份运行时。全局颜色、字体、间距入口是 `storefront/src/index.css`。所有页面滚动使用共享 `ScrollArea`，操作反馈通过根节点的 Sonner Toast 在视口顶部居中显示。

界面使用浅灰背景与深青色主操作，导航、表单、订单状态和反馈共享同一套视觉规则。`components/app/commerce-ui.tsx` 封装页面标题、结算步骤、分区标题和商品图片占位；样式继续由 `src/index.css` 管理，不修改复用的官方 UI 源码。购物车和结算页在宽屏使用商品／表单与摘要双栏，小屏改为单栏；登录注册在小屏优先呈现表单，支持密码可见性切换。订单列表分别展示支付与交付状态，详情展示订单进度和金额、联系人、地址信息。所有新增界面文案已同步到 11 种语言目录，布局继续使用逻辑方向属性。

购物车按站点保存到浏览器 `localStorage`，只保存规格 ID 与数量，不把浏览器中的价格当作交易依据。会员继续使用 GoCMS 的会员 Cookie，会话与后台管理员登录独立。商品名称、封面、规格、价格及启停均来自现有分类内容配置；站点名称来自后台站点设置，不需要另建商品管理列表。

## 路由与开发

| 路由 | 页面 |
| --- | --- |
| `/shop/` | 转到购物车，不提供商品列表 |
| `/shop/cart/add` | 接收主题已选好的规格与数量，校验后加入购物车 |
| `/shop/login` | 独立登录页，支持同源 `next` 返回路径 |
| `/shop/register` | 独立注册页，注册后登录并返回原操作 |
| `/shop/cart` | 购物车、数量修改和按币种汇总 |
| `/shop/checkout?currency=USD` | 当前币种的购物车结算，登录后下单 |
| `/shop/orders` | 当前会员的全部订单 |
| `/shop/orders/<订单编号>` | 全部商品明细、支付与交付状态、继续付款和主动同步 |

首次分别在 `frontend/` 和 `storefront/` 执行 `npm ci`，之后运行 `make dev`。也可分别运行 `make backend`、`make frontend`、`make storefront`。

- Go：`127.0.0.1:18080`。
- 后台 Vite：`127.0.0.1:1244/admin/`。
- 购物 Vite：`127.0.0.1:1245/shop/`。
- 统一调试入口：`127.0.0.1:1244/shop/`，后台 Vite 代理至 Go，再由 Go 代理购物 Vite。热更新直接连接 1245。

本地先在后台选择站点再打开购物页。开发代理沿用后台选择站点的 Cookie，启动后的购物应用将 API 固定到自身当前站点。已匹配的真实域名优先决定站点，开发 Cookie 不用于生产。切换后台站点后重新打开购物前台即可切换。

Go 在开发目录默认代理购物 Vite，参数 `-storefront-dev` 可覆盖地址；设为空则关闭代理。`-storefront` 可指定构建目录。开发 Vite 未启动时尝试读取构建目录；资源缺失会显示启动提示。

## 后台准备与主题入口

1. 后台分类开启在线销售，原内容设置商品类型、币种、规格、价格和封面，并公开内容、启用销售及规格。
2. 超级管理员在支付管理生成或导入加密密钥，然后配置与商品同币种的 PingPong 渠道并启用。
3. 新渠道的付款返回路径使用 `/shop/orders`，公开地址使用本站实际 HTTPS 源站。浏览器收到 `payment_order` 后进入对应订单详情，只读取服务端确认的状态。
4. 主题的商品列表、详情、规格选项及购买按钮沿用原 HTML，选好规格后提交到 `/shop/cart/add`；公共导航可链接到 `/shop/cart`、`/shop/login`、`/shop/register`。接入方式见下方。按钮位置、样式、文案和具体主题的修改由主题管理维护；系统不会自动改写已有主题。

已有渠道的公开地址、凭据与返回路径保持不可变，需要调整时在后台“复制为新渠道”，补齐 Salt 后保存。旧渠道保留历史交易查询和通知。没有当前币种的可用渠道时，结算页明确提示并禁用提交。

## 生成的 HTML 如何接入

Blue 主题和默认模板已提供 `commerce-purchase.html` 公共片段，产品详情及通用内容详情已引用它，因此同一配置也适用于服务。片段从当前内容的公开 API 读取最新规格和单价，提供数量、加入购物车、加入并结算、购物车、订单及登录注册入口；未开启销售或没有可售规格时不展示。网络失败保留重试入口，浏览器后退回到商品页时重新读取价格。

```gotemplate
{{include "commerce-purchase.html" .}}
```

购买区域的 HTML、样式、文案和脚本均位于主题公共片段，可从后台主题管理编辑或随主题 ZIP 导入。自定义主题可把该文件加入自己的 `templates/`，再在需要的详情模板中加入上述引用；不需要更改分类名称、固定 ID、生成器或已有模板绑定。默认外置主题不会随程序升级自动覆盖，需要通过主题管理显式更新。

修改模板后需在后台更新 HTML；价格、规格和启停变化由页面动态读取，不必为每次改价重新发布。当前示例站点的产品与服务模板已接入片段，付款结果仍以 Go 服务确认的状态为准。


商品详情模板使用当前内容的 `{{.id}}` 调用 `GET /api/commerce/catalog?content_id=<内容ID>`，只读取该内容的可售规格；由主题自行呈现参数、价格和选项。这是 HTML 页内的规格数据接口，不是 React 商品列表。主题可通过后台主题管理编辑或上传。

后台“规格维度”独立页面按站点维护选项、文本、数字维度及其单位和候选值；内容的“在线销售与规格价格”只选择已有维度、配置每个组合的独立售价、SKU、固定币种价格和启停。共享名称及候选值更新会同步引用内容，已有 SKU 和价格保留，历史订单不改写。默认和 Blue 主题的 `commerce-purchase.html` 读取目录中的 `dimensions`，按配置顺序和单位显示多个规格选择器；后续维度会随前面维度联动，当前前置选择下不可售的选项禁用，最终提交实际匹配的 `variant_id`。文本和数字维度在购买页同样只能选择后台已配置的值，访客不能自行输入参数影响价格。没有维度时使用标准规格，参数与选用维度不一致时停止购买并提示检查配置。

单个内容最多 500 个组合，公共目录仍每页 50 行；内置购买片段会读取后续页，自定义主题也需按 `page=N` 继续读取，不能只展示首批组合。下方简化示例仅演示第一页和原有组合下拉。规格值的可选 `unit` 会随订单快照保存，购物车和订单页一并展示；旧订单不补写单位。模板变化后通过后台重新生成产品 HTML，此后维度、候选值、各组合价格和启停均动态读取。

选好规格后使用普通同源 GET 表单即可，不需要把商品正文或列表搬进 React：

```html
<form action="/shop/cart/add" method="get" id="purchase-form" data-content-id="{{.id}}">
  <input type="hidden" name="content_id" value="{{.id}}">
  <input type="hidden" name="return_to" value="/">
  <label>规格 <select name="variant_id" required disabled></select></label>
  <label>数量 <input name="quantity" type="number" min="1" max="1000" value="1" required></label>
  <p data-price role="status"></p>
  <button type="submit" disabled>加入购物车</button>
  <button type="submit" name="destination" value="checkout" disabled>加入并结算</button>
</form>
<script type="module">
  const form = document.getElementById("purchase-form")
  const select = form.elements.namedItem("variant_id")
  const message = form.querySelector("[data-price]")
  form.elements.namedItem("return_to").value = location.pathname + location.search
  try {
    const response = await fetch("/api/commerce/catalog?content_id=" + encodeURIComponent(form.dataset.contentId), { credentials: "same-origin", cache: "no-store" })
    const result = await response.json()
    if (!response.ok) throw new Error(typeof result.error === "string" ? result.error : result.error?.message || "规格加载失败")
    const variants = result.offers.filter(item => item.content_id === Number(form.dataset.contentId))
    if (!variants.length) throw new Error("当前内容暂无可购买的规格")
    select.replaceChildren(...variants.map(item => new Option(item.specifications.map(spec => spec.name + "：" + spec.value).join(" / ") || "标准规格", String(item.id))))
    const showPrice = () => {
      const item = variants.find(item => String(item.id) === select.value)
      message.textContent = new Intl.NumberFormat("zh-CN", { style: "currency", currency: item.currency }).format(item.amount / (item.currency === "JPY" ? 1 : 100))
    }
    select.addEventListener("change", showPrice)
    showPrice()
    select.disabled = false
    form.querySelectorAll("button").forEach(button => { button.disabled = false })
  } catch (error) { message.textContent = error instanceof Error ? error.message : "规格加载失败，请刷新重试" }
</script>
```

`variant_id` 必须是所选规格的真实 ID，不能把内容 ID 当成规格 ID。`content_id` 用于额外核对规格归属，`quantity` 默认 1；接口会重新读取本站可售状态。成功加入后立即替换交接地址，刷新购物车不会再次添加。购物车写入失败时停留并提示重试。

`return_to` 只接受同源公开路径，购物车的“继续浏览网站”会回到该 HTML 页面，未提供时回到 `/`。`destination=checkout` 会加入商品后进入对应币种的结算，包含购物车内其他同币种商品；不会直接创建订单。访客可以先加购物车，结算时再登录。

登录链接可使用 `/shop/login?next=<经过 URL 编码的同源路径>`，注册链接同理。结算和订单页会保留原路径、订单编号及查询参数，登录成功后返回。注册包含可选邮箱、密码确认；跨站路径和登录循环路径不会作为回跳地址。

## 订单与 API 约定

除原有目录、订单和会员接口外，新增：

- `GET /api/commerce/channels`：只返回公开的已启用渠道元信息，React 结算、订单页使用此接口，不拉取商品目录。
- `GET /api/commerce/storefront`：返回当前 `site_id`、`site_name`、`path`、`default_language`（后台语言管理中启用的主语言）。
- `GET /api/commerce/variants?ids=1,2`：重新读取最多 50 个可购买规格，返回 `items`。已下架、隐藏或停用的规格不返回，购物车提示先移除。

`POST /api/commerce/orders` 继续要求登录及 `Idempotency-Key`，支持以下请求：

```json
{
  "items": [
    { "variant_id": 1, "quantity": 2 },
    { "variant_id": 2, "quantity": 1 }
  ],
  "currency": "USD",
  "expected_amount": 35000,
  "customer_name": "Buyer Name",
  "email": "buyer@example.com",
  "address": {
    "street": "123 Example Road",
    "city": "Example City",
    "state": "CA",
    "postcode": "90001",
    "country": "US"
  }
}
```

金额使用最小货币单位整数；例中的 `35000` 需要与实际规格价格计算结果一致。一个订单最多 50 个不同规格，每行数量 1–1000，所有行必须属于当前站点且币种相同。实物商品要求收货地址，数字商品和服务可传空地址。运费、税费、库存、优惠券、自动交付和部分退款尚未实现。

Go 在同一事务重新读取可售状态和单价，计算总额并保存各行名称、规格、单价、数量、类型及封面快照。`expected_amount` 或币种不符返回 HTTP 409、`code: "price_changed"`；前台刷新价格，要求用户重新确认。历史订单继续按创建时快照付款。

结算先把原请求及随机幂等键写入当前站点、会员专用的 `sessionStorage`，再发送下单请求。网络结果不明时显示恢复入口，使用原键和原请求重试；成功取得订单后移除对应购物车数量，再发起付款。支付发起失败可在订单详情继续处理，订单列表也可找回已创建的订单。浏览器返回不是付款成功依据，详情读取数据库状态并支持主动同步；服务端原有通知及后台补查继续生效。

旧的单规格请求 `variant_id`／`offer_id`、`quantity` 仍兼容，不应与 `items` 混传。返回新增 `items` 全明细数组；旧 `item` 保留第一行，`quantity` 是所有行数量合计。启动时把已有单行订单明细迁移为 `(order_id, line_number)` 主键，第 1 行保留全部旧快照，订单和支付记录不变。

## 多语言

购物前台使用 i18next + react-i18next，11 个语言目录随应用打包，无需翻译服务账号。语言选择器位于购物前台顶部，语言偏好存入浏览器 `localStorage`。界面、表单说明、状态、加载和空状态、错误提示与无障碍标签均使用翻译键。浏览器原生表单校验弹窗使用浏览器自身的语言。

| 代码 | 名称 |
| --- | --- |
| `en` | English |
| `zh-CN` | 简体中文 |
| `es` | Español |
| `fr` | Français |
| `ru` | Русский |
| `tr` | Türkçe |
| `pt` | Português |
| `id` | Bahasa Indonesia |
| `ar` | العربية |
| `th` | ไทย |
| `vi` | Tiếng Việt |

语言优先级为：**storefront 手动选择 → 前台当前语言 → 后台“语言管理”中启用的站点主语言 → 浏览器语言 → 英文**。购物入口使用普通地址，例如 `/shop/cart`，不需要语言参数；storefront 不再读取或生成 URL `lang` 参数。支持 `en-US`、`pt-BR` 等语言区域变体及简体中文标记；未提供的繁体中文目录不会被强制认作简体。不支持的语言会继续向下回退，购物 UI 的可选语言与内容语言是否启用独立。

两个状态按 API 确认的站点 ID 隔离，不能根据栏目、路径、域名字符串或后台选择状态猜测：

| localStorage 键 | 内容 | 写入方 |
| --- | --- | --- |
| `gocms-site-language:<siteId>` | 前台页面的语言代码 | 主题公共语言片段 |
| `gocms-storefront-language:<siteId>` | 用户在 storefront 手动选择的语言代码 | storefront 语言选择器 |

前台自动同步和购物内部导航不写入手动偏好。选择器默认显示“自动”（跟随网站语言），选中某个语言后写入手动偏好；重新选择“自动”会移除该项并立即按当前前台语言解析。即使用户手动选择的语言恰好等于当前网站语言，也会保存为明确的手动覆盖。旧版全局键 `gocms-storefront-language` 同时保存过 URL 语言和用户选择，无法可靠区分来源，因此不迁移为新的手动偏好；升级后默认跟随，用户可重新选择。

例如：英文产品页进入购物车会显示英文；购物车手动选择法语后，再从西班牙语前台进入仍为法语；恢复跟随后立即采用已记录的西班牙语。状态通过 `storage` 事件在同源标签页之间同步，前台语言变化不会覆盖手动选择。浏览器从页面缓存恢复 storefront 时会重新读取偏好；语言切换更新标题及 HTML `lang` / `dir`，不清空购物车、不重建结算表单、不触发重复加购，也不改变现有查询参数、订单编号或付款恢复信息。

默认模板和 Blue 已加入 `site-language.html` 公共片段。它读取模板函数 `currentSiteID` 与 `currentLang`，在页面加载、恢复、重新激活、普通链接点击和表单提交时更新前台语言记录；监听 HTML `lang` 属性变化，兼容主题在当前页面内切换语言。所有操作保留链接和表单原本行为，不需要逐个修改购物入口。状态仅在内容页面更新，storefront 手动选择不会反向改写静态前台语言。

自定义主题从后台主题管理导入或编辑公共语言片段，在公共头部中接入一次：

```gotemplate
{{include "site-language.html" .}}
```

片段根元素使用 `data-site-id="{{currentSiteID}}"` 与 `data-language="{{currentLang}}"` 获取发布上下文；不依赖正文或 URL 中的语言参数。默认模板在各页面的 head 中引用，Blue 在公共头部中引用，首页、列表、详情、留言、搜索等页面均覆盖。更新主题后需要在后台重新生成 HTML，已经发布的旧 HTML 不会自动被改写。

浏览器拒绝本地存储时，前台同步静默回退；storefront 使用站点默认语言或浏览器语言。用户手动切换仍在当前页面生效，并通过顶部 Toast 提示未能保存。localStorage 仅在相同协议、域名及端口下共享，不跨域或跨设备同步；开发时请统一从 `127.0.0.1:1244` 打开静态前台和 `/shop/`，直接访问 `1245` 无法读取 `1244` 的偏好。

主题生成页面的文案继续由主题及内容翻译维护，不自动翻译商品名称、规格、正文、渠道名称或历史订单快照。

阿拉伯语同时设置页面 `dir="rtl"` 和 Base UI `DirectionProvider`，下拉弹层及键盘方向与页面一致。邮箱、国家代码使用从左到右输入；布局采用逻辑对齐与间距。金额使用所选语言的 `Intl.NumberFormat` 格式，只改变显示方式，不转换交易币种或金额；日期按所选语言和浏览器时区显示。

维护入口：

- `src/i18n/index.ts`：支持语言登记、按站点保存的手动偏好、前台语言跟随与跨标签同步、默认回退及格式化工具。
- `src/i18n/locales/*.json`：各语言字典，以英文目录为完整键契约，TypeScript 约束每种语言必须包含同一套基础键；额外的复数形式按语言添加。
- `components/app/locale-provider.tsx`、`language-switcher.tsx`：界面语言切换与组件方向。
- `lib/errors.ts`：后端稳定错误码与前台翻译键映射；保留错误对象，在渲染时按当前语言取文案。后台管理仍保留原有可读错误文本。

新增语言时添加对应 JSON、在语言登记与资源表中注册；新增文案时补齐所有目录，页面通过 `useTranslation()` 使用键和插值。数量使用 i18next 复数规则，不能在业务组件中拼接单数或复数词尾。参考 [react-i18next 文档](https://react.i18next.com/latest/using-with-hooks)及 [Base UI 方向说明](https://base-ui.com/react/utils/direction-provider)。

## 生产构建与部署

`make build` 分别安装和构建后台及购物前台，然后同步资源并生成 `bin/site`。`scripts/sync-embedded.mjs` 包含 `storefront/dist`，Release 的 `make test` 流程也会构建两个应用。单文件程序内嵌两个 SPA；用户主题、模板、数据库、密钥及上传文件继续外置。

生产反向代理将 `/shop/`、`/api/commerce/`、会员接口和支付通知路径交给 Go，不能只托管发布的 `web/`。Go 只在 `/shop/` 内提供 SPA 回退，未知静态资源返回 404，不公开主题模板或数据目录。购物页面设为不索引，交易 API 禁止缓存。

本次按用户要求未运行构建、自动化测试或浏览器验收。真实支付仍需使用商户沙箱完成下单、回跳、通知、重试及退款联调。

### 国家、行政区与城市选择

实体商品结算页接入共享地址目录，支持搜索国家、按国家选择一级行政区、按国家或行政区搜索城市。国家展示名跟随当前语言；城市和行政区使用 GeoNames 名称，城市可通过数据源中的别名搜索。用户仍可手动填写行政区和城市；目录不可用时支持手动填写两位国家代码。街道与邮编继续填写。

首次使用由超级管理员在「平台管理 → 地址数据」下载或上传 GeoNames 数据。默认只初始化国家，不自动下载城市包。目录按版本原子切换、可回退，订单地址继续保存文字快照，不依赖当前目录版本。独立模块、接口和数据授权见 [共享地址目录](geography.md)。


## 多币种报价

站点管理员在「支付管理 → 币种与汇率」选择基准币种、开放币种及手动汇率。基准不固定为 USD，可选择现有支付模块支持的 AUD、CAD、CHF、CNY、EUR、GBP、HKD、JPY、NZD、SGD、USD；界面语言不限制可选币种。汇率含义始终为「1 单位基准币种 = 目标币种的主单位数量」。配置按站点保存，默认关闭，不为现有站点猜测或填入实时汇率。

新商品默认读取站点基准币种；未配置基准时需在内容编辑中明确选择原始计价币种。既有内容的原始币种和价格保持原值。切换基准时，后台会清空其他币种的汇率，管理员需按新基准重新填写；不会把历史人民币价格直接改标为美元。旧商品币种与基准不同的情况下，需同时提供来源币种与目标币种相对基准的汇率；来源币种可保留汇率但关闭前台展示。

内容规格支持可选 `fixed_prices`，以币种为键、最小货币单位整数为值。报价优先使用目标币种固定售价，否则计算交叉汇率 `目标/基准 ÷ 来源/基准`。服务端使用精确有理数计算，每个规格的单位价格按目标币种最小单位四舍五入，再乘数量汇总，不采用浏览器上传的汇率或单价。

手动汇率有有效期（默认 168 小时）；过期、缺少交叉汇率或换算金额越界时，报价回退原币种并显示说明，固定币种售价不受汇率到期影响。修改汇率会更新时间；只修改国家映射或有效期不会把旧汇率伪装成刚更新。后台「确认汇率仍有效」需保存后生效。本版不自动拉取外部汇率。

`GET /api/commerce/pricing` 返回实际 `site_id`、是否启用、基准币种、开放币种及其精度、推荐币种和配置版本，不公开代理配置。可选 `country=CN` 用于用户明确选择的国家提示。自动推荐优先采用已记住的购物国家，其次采用可信代理的国家信息，无法识别时回退基准币种；不会依据界面语言猜测国家。

地区自动识别通过可选的国家请求头接入。管理员必须同时配置请求头名称及代理 CIDR，并确保边缘代理覆盖该请求头；服务只信任直接连接的已配置代理，不任意信任 `X-Forwarded-For`。未接入提供国家代码的代理时，没有自动 IP 地理定位；本地和内网访问正常回退基准币种。

手动币种偏好使用 `gocms-currency:<siteId>`，优先于自动推荐；选择「自动」移除手动偏好。国家提示使用 `gocms-market-country:<siteId>`。收货表单选择国家时记录下次打开购物页面的推荐地区，不在本次填写过程中自动改价。币种和语言偏好完全独立，均只在同源下共享。主题购买片段与 storefront 使用相同存储键，并处理跨标签变化和浏览器缓存恢复；跳转购物前台不需要币种 URL 参数。

公开目录和规格接口支持 `currency` 查询参数。`amount` / `currency` 始终代表实际下单金额和币种：如果目标币种有已启用渠道，则返回可成交的目标币种价格；没有渠道时保持原币种，另返回 `display_amount` / `display_currency` 参考价，界面显示「约」及「实际支付」。缺少有效报价返回 `pricing_unavailable`，不以过期汇率虚构价格。金额显示沿用所选语言的统一格式化工具，不转换历史订单金额。

购物车、结算和默认／Blue 主题的 `commerce-purchase.html` 共用上述后端报价。更新主题后需从后台重新生成 HTML，才能让已发布产品页出现币种选择。其他主题可按相同 API 与存储协议接入，无需改变内容、分类或模板绑定。各币种按是否可报价和收款决定是否合并成同一币种订单，不能换算的商品仍按原币种分别结算。

结算提交 `currency`、`expected_amount` 和 `pricing_revision`。服务端在创建订单的事务中重新读取价格、汇率版本及渠道能力；配置版本或金额变化返回 `price_changed`，前台刷新报价并要求重新确认。币种切换重新加载价格时保留已填写表单，暂时禁止提交。订单保存成交单价、总额、币种及独立 `pricing_json` 快照（来源金额、定价方式、汇率和版本），以后更换基准或更新汇率不改历史成交金额；退款继续使用原交易币种及金额。

后台 `GET/PUT /api/admin/payments/pricing` 需要 `payments` 和 `payments.configure` 权限；修改还需后台登录会话及同源校验。保存包含配置版本检查，防止旧页面覆盖新设置。支持 JSON 导入／导出备份，导入先形成可编辑草稿，保存后才生效。`GET /api/admin/payments/pricing-options` 仅提供内容编辑所需的默认计价币种，使用现有销售管理权限及后台站点作用域。

升级新增站点报价配置表、规格固定币种售价字段和订单报价快照字段，服务启动时兼容迁移。现有商品、历史订单、支付记录及退款不重算。按用户要求，本次未运行测试、构建或浏览器验收。
