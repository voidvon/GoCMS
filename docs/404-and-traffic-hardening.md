# 404、站内死链与错误缓存：根因修复说明

## 背景

线上仪表盘出现大量 404 与“站内死链”，排查后确认不是单一页面问题，而是通用 CMS 在四个层面的契约缺失：

1. **404 页面没有独立的 SEO 上下文**。主题的通用 head 把 404 当成普通页面，回退到首页 `<title>`、canonical `/` 和 `index, follow`。
2. **sitemap 把 404 当内容**。生成器按 `.html` 文件遍历，`404.html` 和各语言 `404.html` 被写进 `sitemap.xml`、`sitemap.html` 和 `llms.txt`。
3. **访问统计只看 Referer 域名**。扫描器把 Referer 伪造成同域路径（例如 `/.aws/credentials`），就被算作“站内死链”。
4. **错误响应继承了静态资源的长缓存**。Nginx 对 `uploads/images` 使用 `add_header ... always`，导致 404 也被缓存 30 天；文件补传后仍然 404。

这些问题都不应该靠改某个站点的页面来解决，而应该在 CMS 核心统一定义。

## 核心改动

### 1. 404 的通用 SEO 契约

`generator.notFoundPage` 现在向主题传入：

- `is_404=true`
- `page_type=404`
- `seo_title=404`
- `seo_robots=noindex, follow`
- `seo_canonical=""`

主题应在 head 中使用这些字段，而不是让通用 header 回退到首页元数据：

```go
{{if .is_404}}
  <title>{{.seo_title}} | {{setting "site_name"}}</title>
  <meta name="robots" content="{{.seo_robots}}">
{{else}}
  <!-- 正常页面 head -->
{{end}}
```

同时，服务端对所有 4xx/5xx 响应统一设置：

```http
Cache-Control: no-store
```

HTML 错误额外设置：

```http
X-Robots-Tag: noindex, follow
```

直接访问 `/404.html` 也会带上这两个响应头。这样即使某个主题暂时没有使用新字段，搜索引擎也不会索引错误页。

### 2. sitemap / llms 排除 404

新增共享约定 `routing.NotFoundFileName` 与 `routing.IsNotFoundPage`：

- 识别 `404.html`、`404.htm`；
- 识别语言目录，如 `/es/404.html`；
- 同时兼容反斜杠路径。

生成器与独立 sitemap API 都使用同一判断，`404` 不再进入：

- `sitemap.xml`
- `sitemap.html`
- `llms.txt`

### 3. 站内 Referer 真实性校验

错误请求的 `referer_kind` 现在按以下顺序判定：

1. 没有 Referer：`none`；
2. `Sec-Fetch-Site: cross-site`：`external`；
3. Referer 主机不属于本站：`external`；
4. `Sec-Fetch-Site: none`：`none`；
5. Referer 指向本站真实存在的 HTML 页面（目录需有 `index.html`）：`internal`；
6. 同域但页面不存在：`invalid`。

`invalid` 是新增来源类型，不计入“站内死链”，但仍保留在访问明细中用于排查。仪表盘新增：

- `internal_404_errors`：站内死链命中次数；
- `internal_404_paths`：按 URL 去重的站内死链地址数。

后台卡片与徽标改用去重地址数，避免把命中次数误读成地址数。

### 4. 错误响应不再继承长缓存

Go 侧统一为 4xx/5xx 设置 `Cache-Control: no-store`；Nginx 侧移除 `uploads/images`、`css/js/assets` 和 HTML location 上的 `add_header ... always`，只对成功响应设置长缓存/协商缓存。

## 部署顺序

1. 替换 GoCMS 可执行文件并重启服务（使用本仓库构建产物 `bin/site`）。
2. 更新 Nginx 配置并 `nginx -t && nginx -s reload`。
3. 清理 Cloudflare 上已有的 404 缓存（旧缓存可能仍带 30 天 TTL）。
4. 更新主题 `header.html`，使用 `is_404` / `seo_robots` / `seo_title` 分支。
5. 重新发布站点，确认 404 页面、sitemap 和访问统计。

## 验证清单

```bash
cd backend
go test ./...
go vet ./...

cd ../frontend
npm run build
npm run lint
```

线上验证：

```bash
# 404 页面：HTTP 404 + X-Robots-Tag + no-store
curl -sSI https://example.com/definitely-missing | grep -iE 'HTTP|x-robots|cache-control'

# 直接访问 404.html：HTTP 200，但仍带 noindex/no-store
curl -sSI https://example.com/404.html | grep -iE 'HTTP|x-robots|cache-control'

# sitemap 不再包含 404.html
curl -sS https://example.com/sitemap.xml | grep -o '404\.html' || true
```

访问统计验证：

- 正常站内页面跳转到不存在路径：`referer_kind=internal`，进入站内死链。
- 伪造 Referer 的同域扫描路径：`referer_kind=invalid`，不进入站内死链。
- 仪表盘“站内死链”显示去重地址数，详情显示命中次数。
