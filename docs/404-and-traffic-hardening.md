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

### 4. 多域名错误归并

访问日志新增 `host` 字段，保存实际请求域名（保留 `www.`，独立语言域名也分别保留）。错误列表和访问明细按 `host + path + status` 归并/展示，仪表盘新增“域名”列。服务启动时 `EnsureAccessLog` 会自动补列并建索引，不需要手工迁移。

- 新记录：显示真实请求域名。
- 升级前的历史记录：`host=''`，后台回退显示当前站点主域名，保留期结束后自然淘汰。
- 因此同一个 `/missing-page` 在 `www.example.com` 和 `cn.example.com` 上会显示为两条独立记录，不再混在一起。

域名归属查询与缓存也必须保留 `www.`，与前台路由使用相同的完整主机名。若在查询前去掉 `www.`，数据库中配置为 `www.example.com` 的站点会匹配失败，其访问与错误便回落到默认站点；独立语言域名还可能使用错误的输出目录校验 Referer。根域名与 `www` 域名需要共享站点时，应通过站点别名配置，不能在统计层隐式合并。

修复只影响升级后的新记录，不修改历史日志的站点归属。已记错的记录会按配置的保留期自然淘汰。

### 5. 错误响应不再继承长缓存

Go 侧统一为 4xx/5xx 设置 `Cache-Control: no-store`；Nginx 侧移除 `uploads/images`、`css/js/assets` 和 HTML location 上的 `add_header ... always`，只对成功响应设置长缓存/协商缓存。

### 6. 仪表盘滚动加载

- `GET /api/admin/access-logs/errors` 支持 `limit`、`offset`、`sort=hits|recent`，返回真实分组总数；仪表盘使用 `sort=recent`。
- 新增 `GET /api/admin/access-logs/pages`，支持 `limit`、`offset`，用于热门页面。
- “错误与死链”和“热门页面”卡片内部固定最大高度，滚动到底部自动加载下一页。
- “错误类型”统计从错误列表下方移到错误卡片右侧的窄栏，列表不再被状态徽章挤压。

### 7. 错误来源 IP

- 错误列表显示每组最近一次请求的来源 IP。有安全管理权限的用户可以将该 IP 加入当前站点黑名单。启用 Nginx 同步后，规则会自动同步。
- 升级后已有路径忽略规则失效，保留期内的相关错误会重新计入统计。

## 部署顺序

1. 替换 GoCMS 可执行文件并重启服务（使用本仓库构建产物 `bin/site`）。
2. 更新 Nginx 配置并 `nginx -t && nginx -s reload`。
3. 清理 Cloudflare 上已有的 404 缓存（旧缓存可能仍带 30 天 TTL）。
4. 更新主题 `header.html`，使用 `is_404` / `seo_robots` / `seo_title` 分支。
5. 重新发布站点，确认 404 页面、sitemap 和访问统计。

## 本地测试数据

```bash
python3 scripts/seed-access-logs.py --reset
# 或指定站点/域名
python3 scripts/seed-access-logs.py --site 2 --hosts test.local,www.test.local --reset
```

默认写入站点 1 的 287 条记录（182 条页面访问、105 条错误、46 个错误路径），便于测试滚动加载、请求域名、来源 IP 和错误类型。

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
