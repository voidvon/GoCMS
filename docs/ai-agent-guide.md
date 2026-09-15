# GoCMS AI Agent 操作指南 (AI Integration Guide)

本指南专为大语言模型（LLM / AI Agent）接管并操作 GoCMS 提供接口规范与核心工作流说明。

---

## 1. 快速接入与鉴权

- **服务基础地址**：`http://<SERVER_IP_OR_DOMAIN>:<PORT>`（默认端口通常为 `18080`，例如 `http://127.0.0.1:18080`）
- **鉴权方式**：
  在每个 HTTP 请求头中携带生成的 API Key，支持以下两种形式之一：
  ```http
  X-API-Key: gocms_live_xxxxxxxxxxxxxxxx
  ```
  或
  ```http
  Authorization: Bearer gocms_live_xxxxxxxxxxxxxxxx
  ```
- **请求/响应格式**：所有非上传请求均使用 `Content-Type: application/json`，响应统一返回 JSON。

---

## 2. 核心架构约束与工作流（AI 必须遵守）

> [!IMPORTANT]
> **发布闭环规则**：
> GoCMS 是一套静态生成的 CMS。在数据库中保存或修改内容后，**前台静态页面不会自动更新**。
> - 可以在保存接口 URL 后追加 `?publish=1`（如 `POST /api/admin/content?publish=1`），系统会在保存后自动异步触发重新生成；
> - 或者在批量操作完成后，单独调用一次 `POST /api/admin/publish` 接口。

### 推荐标准工作流：创建并发布文章
1. **查询栏目**：调用 `GET /api/admin/categories`，找到目标栏目的 `id`（例如新闻栏目的 `id`）。
2. **提交内容**：调用 `POST /api/admin/content?publish=1`，提交文章标题、分类 ID、正文 HTML。
3. **完成发布**：前台静态网页即刻更新。

---

## 3. 核心 API 接口定义

### 3.1 栏目管理 (Categories)

#### 获取所有栏目树
- **路径**：`GET /api/admin/categories`
- **说明**：获取站点所有栏目，用于定位 `category_id`。
- **响应示例**：
  ```json
  [
    {
      "id": 1,
      "name": "新闻动态",
      "parent_id": 0,
      "page_type": "list",
      "model_id": 1
    },
    {
      "id": 2,
      "name": "产品中心",
      "parent_id": 0,
      "page_type": "list",
      "model_id": 2
    }
  ]
  ```

---

### 3.2 内容管理 (Content)

#### 1. 分页查询内容列表
- **路径**：`GET /api/admin/content`
- **Query 参数**：
  - `page`: 页码（从 1 开始，默认 1）
  - `page_size`: 每页条数（默认 20，最大 100）
  - `q`: 搜索关键词（可选）
  - `category_id`: 按栏目筛选（可选，传 0 查询全部）
- **响应示例**：
  ```json
  {
    "query": "",
    "page": 1,
    "page_size": 20,
    "total": 42,
    "items": [
      {
        "id": 10,
        "title": "公司最新动态发布",
        "category_id": 1,
        "summary": "文章摘要...",
        "visible": 1,
        "published_at": "2026-09-14 12:00:00"
      }
    ]
  }
  ```

#### 2. 获取单篇内容详情
- **路径**：`GET /api/admin/content/{id}`
- **响应**：完整的文章/产品详情对象。

#### 3. 新建内容
- **路径**：`POST /api/admin/content`（建议追加 `?publish=1` 自动触发全站静态生成）
- **请求体 (JSON)**：
  | 字段名 | 类型 | 必填 | 说明 |
  | :--- | :--- | :--- | :--- |
  | `title` | string | 是 | 内容标题 |
  | `category_id` | integer | 是 | 所属分类 ID |
  | `content` | string | 否 | 正文富文本（HTML 格式） |
  | `summary` | string | 否 | 摘要（若为空，前台主题通常会截取正文） |
  | `cover_image` | string | 否 | 封面图相对路径，如 `/images/demo.jpg` |
  | `keywords` | string | 否 | SEO 关键词，以逗号分隔 |
  | `description` | string | 否 | SEO 页面描述 |
  | `visible` | integer | 否 | `1` 为公开展示，`0` 为隐藏草稿（默认 `1`） |
  | `order_id` | integer | 否 | 排序权重（数值越大越靠前，默认 `0`） |
  | `extra_data` | object | 否 | 自定义模型字段，如 `{"price": "99.00"}` |
- **请求示例**：
  ```json
  {
    "title": "2026 年秋季新产品发布公告",
    "category_id": 2,
    "summary": "本次发布涵盖三款全新智能设备...",
    "content": "<p>今天我们非常荣幸地宣布推出全新一代智能设备...</p>",
    "keywords": "新产品,智能设备,发布会",
    "description": "2026 年秋季新产品发布详情与参数说明。",
    "visible": 1
  }
  ```
- **响应示例**：
  ```json
  { "ok": true }
  ```

#### 4. 更新内容
- **路径**：`PUT /api/admin/content/{id}`（可加 `?publish=1`）
- **请求体**：与新建相同，传入需要修改后的完整字段。
- **响应**：`{ "ok": true }`

#### 5. 删除内容
- **路径**：`DELETE /api/admin/content/{id}`（可加 `?publish=1`）
- **响应**：`{ "ok": true }`

---

### 3.3 网站发布管理 (Publish)

#### 1. 触发全站静态页面重新生成
- **路径**：`POST /api/admin/publish`
- **说明**：触发全站 HTML 和 Sitemap 生成，支持异步排队。
- **响应示例**：
  ```json
  {
    "started": true,
    "report": {
      "state": "running"
    }
  }
  ```

#### 2. 查看当前发布状态
- **路径**：`GET /api/admin/publish`
- **响应字段说明**：
  - `state`: `"idle"`（空闲）、`"running"`（生成中）、`"success"`（成功）、`"failed"`（失败）。
  - `pages`: 生成的页面总数。
  - `error`: 错误信息（若失败）。

---

### 3.4 媒体资源上传 (Media)

- **路径**：`POST /api/admin/media`
- **请求格式**：`multipart/form-data`
  - 字段：`file`（文件二进制数据）
- **响应示例**：
  ```json
  {
    "ok": true,
    "data": {
      "id": 5,
      "url": "/images/media-20260914-123456.jpg",
      "original_name": "photo.jpg"
    }
  }
  ```
- **说明**：上传返回的 `url` 可直接填入内容的 `cover_image` 或正文 `<img>` 标签中。

---

### 3.5 客户留言处理 (Messages)

- **获取留言列表**：`GET /api/admin/messages?page=1&page_size=20`
- **修改留言状态**：`PUT /api/admin/messages/{id}`，参数 `{"state": 1}`（`0`=未审核/待处理，`1`=已处理/公开）。

---

### 3.6 模板组与主题文件管理 (Themes & Templates)

> [!IMPORTANT]
> 修改 HTML 模板或 CSS/JS 资源后，**前台静态页面不会自动更新**。修改完成后必须调用一次 `POST /api/admin/publish` 触发全站静态页面重新生成！

#### 1. 查询当前活动主题及全部模板文件列表
- **路径**：`GET /api/admin/theme`
- **说明**：获取当前主题 ID、模板文件分类（首页、封面、列表、内容等）、以及自定义 CSS/JS/图片文件列表。
- **响应结构简要**：
  ```json
  {
    "active_theme": "blue",
    "themes": [
      { "id": "blue", "name": "默认蓝色主题", "active": true }
    ],
    "template_groups": [
      {
        "key": "home",
        "label": "首页模板",
        "files": [{ "path": "index.html", "size": 10240 }]
      },
      {
        "key": "list",
        "label": "列表模板",
        "files": [{ "path": "article_list.html", "size": 8192 }]
      }
    ],
    "custom_files": {
      "css": [{ "path": "css/style.css", "size": 4096 }],
      "js": [{ "path": "js/main.js", "size": 2048 }]
    }
  }
  ```

#### 2. 读取指定模板或自定义文件源码
- **路径**：`GET /api/admin/theme?kind={kind}&path={path}`
- **Query 参数**：
  - `kind`: 文件类别（`template` 为 HTML 模板，`css` 为样式，`js` 为脚本）
  - `path`: 相对文件名，如 `index.html` 或 `css/style.css`
- **响应示例**：
  ```json
  {
    "kind": "template",
    "path": "index.html",
    "content": "<!DOCTYPE html>\n<html>\n<head>...",
    "size": 10240
  }
  ```

#### 3. 编辑保存模板或自定义 CSS/JS 代码
- **路径**：`PUT /api/admin/theme/files`
- **请求体 (JSON)**：
  ```json
  {
    "kind": "template",
    "path": "index.html",
    "content": "<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n  <meta charset=\"UTF-8\">\n  <title>{{.Site.Title}}</title>\n</head>\n<body>\n  <h1>{{.Site.Title}}</h1>\n</body>\n</html>"
  }
  ```
- **响应**：`{ "ok": true, "file": { "path": "index.html", "size": 1056 } }`
- **注意**：保存后务必调用 `POST /api/admin/publish` 才能将修改编译到前台静态页。

#### 4. 切换当前活动主题
- **路径**：`POST /api/admin/theme/activate`
- **请求体 (JSON)**：
  ```json
  {
    "id": "blue"
  }
  ```
- **说明**：系统会自动校验该主题中已绑定的模板完整性，若校验通过会自动保存并在后台启动全站重新发布任务。

#### 5. 修改全局模板绑定 (Global Template Assignments)
- **路径**：`PUT /api/admin/theme/assignments/{key}`
- **说明**：设置全局页面绑定的模板文件。常用 key：`home_index`（首页）、`message`（留言板）、`search`（搜索页）。
- **请求体 (JSON)**：
  ```json
  {
    "template_path": "index.html"
  }
  ```
- **响应**：`{ "ok": true, "key": "home_index", "template_path": "index.html" }`

---

### 3.7 标签模板管理 (Template Labels)

GoCMS 支持类似帝国 CMS `[e:loop]` 的标签模板与动态数据调用模块：

- **获取标签列表**：`GET /api/admin/theme/label-templates?page=1&page_size=20`
- **新建标签模板**：`POST /api/admin/theme/label-templates`
  - 请求体：
    ```json
    {
      "key": "news-card",
      "name": "新闻列表卡片",
      "category_id": 1,
      "context": "list",
      "description": "首页调用的新闻卡片组件",
      "temptext": "<ul>[!--list.temp--]<!--list.var1-->[!--list.temp--]</ul>",
      "listvar": "<li><a href=\"[!--url--]\">[!--title--]</a></li>"
    }
    ```
- **修改标签模板**：`PUT /api/admin/theme/label-templates/{id}`
- **删除标签模板**：`DELETE /api/admin/theme/label-templates/{id}`

---

## 4. 可直接喂给 AI 的 System Prompt 模板

你可以直接将以下文本复制给任何外部大模型（如 ChatGPT / Claude / Dify Agent），并在首行填入真实参数：

```text
你现在是当前 GoCMS 网站的内容运营与技术维护 Agent。

【接入配置】
- CMS 根地址: http://127.0.0.1:18080 （请根据实际情况替换）
- 认证头: X-API-Key: gocms_live_xxxxxxxxxxxxxxxx （请填入你的实际 Key）

【基本指令与工作流规范】
1. 涉及文章或产品新增/修改时：
   - 先请求 GET /api/admin/categories 获取栏目列表及 category_id。
   - 使用 POST /api/admin/content?publish=1 提交内容，标题为 title，正文为 content（支持 HTML 格式）。
2. 涉及前台模板或样式调整时：
   - 读取模板：GET /api/admin/theme?kind=template&path=模板文件名（如 index.html）。
   - 更新模板：PUT /api/admin/theme/files 传入 kind, path, content 保存更新。
   - 模板或样式保存完成后，必须发送 POST /api/admin/publish 触发全站静态生成！
3. 核心发布规则：
   - GoCMS 是主题驱动的静态发布 CMS。除带 ?publish=1 保存内容会自动触发外，任何模板调整或批量修改后，必须发送 POST /api/admin/publish 才能在前台静态网页中生效。
4. 错误处理：
   - 返回 401 说明 API Key 失效或无权限；返回 403 说明该 Key 无权操作对应栏目或模块。
```
