# 彪维静态 CMS

Go + SQLite 内容服务、React 管理后台、Go 模板静态发布。

## 目录

- `backend/`：Go 模块，`cmd/site` 服务、`cmd/generate` 发布、`cmd/migrate` 导入、`cmd/reset-password` 密码重置。
- `backend/templates/`：从原 blue 模板转换的 Go 模板；页面布局在这里维护。`tag` 函数解析数据库字段与自定义片段，不执行 ASP。
- `frontend/`：Vite + React 管理后台，shadcn Base UI Nova；官方组件保持原样，业务封装放 `src/components/app/`。
- `assets/theme/blue/`：当前主题的 CSS、JS、skin 和固定图片，属于代码资源并纳入 Git。
- `assets/images/`：产品图、新闻正文图和详情介绍图，属于运行时业务资源，不纳入 Git；服务直接读取，不参与发布复制。
- `web/`：生成的 HTML 和 Sitemap.xml，不纳入 Git；发布只替换此目录。
- `data/`：SQLite 数据库、发布状态及发布锁；不可作为静态网站根目录。
- `legacy/`：原 ASP、Access、原模板和 IIS 配置本地归档；不纳入 Git，不参与 Go 运行。

## 本地运行

在项目根目录分别打开两个终端：

```sh
make backend
make frontend
```

后台：`http://127.0.0.1:5173/admin/`，网站：`http://127.0.0.1:18080/`。
后台页面内“网站发布”支持全站重新生成；产品编辑支持“仅保存”和“保存并发布”。隐藏产品会自动提交发布任务。已有任务运行时，新的保存发布请求会排队，在当前任务结束后再读取最新数据生成。

```sh
make generate  # 命令行全站生成
make test      # Go 测试/vet + 前端构建
```

## 发布规则

| 数据 | 模板 | 文件 |
| --- | --- | --- |
| 站点配置、SEO、自定义片段、推荐产品和新闻 | index | `/index.html` |
| 公开产品 (`show=1`) | produts_detail | `/Product/{id}.html` |
| 产品分类，包含后代产品 | produts_sort / produts_sort2 | 由分类路由配置决定，现有默认分别为 `/valve/{id}.html` 和 `/Products/{id}.html` |
| 新闻分类 (`root=4`) | news_sort / news_news | `/news/{id}.html`、`/news/detail/{newsid}.html` |
| 技术文章分类 (`root=12`) | service_sort / service_service | `/service/{id}.html`、`/service/detail/{newsid}.html` |
| 公司介绍 (`root=32`) | corporation | `/about/About-{id}.html` |
| 有效招聘 (`state=1`) | job_sort / job_detail | `/job/index.html`、`/job/detail/{id}.html` |
| 联系信息、留言表单 | contact / msg | `/contact.html`、`/msg.html` |
| 已生成页面 | 内置 | `/Sitemap.xml`、`/sitemap.html` |

产品每页 14 条，新闻每页 6 条，第一页同时生成 `{id}.html` 和 `{id}-1.html`，后续为 `{id}-{page}.html`。栏目首页沿用排序第一分类的第一页。首页轮播取最新 8 个推荐公开产品，文字推荐取 32 个；网站地图从实际生成的地址产生。

产品分类保存自己的静态路由规则，产品详情目录和文件名由产品所属分类决定。分类编辑中的列表目录、列表文件名规则、详情目录和详情文件名规则默认分别为 `valve`/`Products`、`{id}.html`、`Product`、`{id}.html`，因此现有线上地址不会改变。产品内容只保存 `CatId`，发布器会按该分类解析详情页路径；SQLite 启动时会自动补充并初始化这些路由字段，旧 Access 导入的列结构不受影响。

生成使用一致的 SQLite 读事务快照。先完成所有模板渲染和 UTF-8 检查，再向临时目录写入新 HTML 和站点地图，最后切换 `web/`。新目录只包含本次生成结果，因此隐藏/删除内容及过期分页会清理；独立资源目录不被修改。未知模板标签或模板错误会终止发布，保持旧站点。发布锁用于防止 CLI/API 并发写入。

目录切换使用同文件系统的两次 rename；切换间可能存在极短的无目录窗口，并非整个站点的单次原子交换。安装失败会尝试恢复 `web.previous`。若进程在切换时中断并留下 `web.previous`，先检查 `web/` 是否完整，再恢复或归档备份后重新发布。发布结果保存在 `data/publish.json`，后台也会展示生成数量、状态和失败原因。

模板与自定义 HTML 片段属于受信任的管理员内容。普通字段会 HTML 转义，富文本正文按 HTML 输出。图片统一使用 `/images/文件名`，服务先查找 `assets/images/`，再查找 `assets/theme/blue/images/`；数据库导入和发布时会把旧图片地址规范化为这个路径。

## 生产运行

```sh
make build
cd backend
../bin/site -addr 127.0.0.1:18080
```

Go 同时提供 `/admin/`（`frontend/dist`）、`/api/` 和公开静态站点。`/images/` 依次映射到 `assets/images/` 和 `assets/theme/blue/images/`，`/css/`、`/js/`、`/skin/` 映射到 `assets/theme/blue/` 下对应目录，网站页面读取 `web/`。发布不复制图片，也不创建符号链接。旧的 `/uploadfile/` 和 `/UploadFile/` 地址不再提供服务。Nginx 可直接反向代理 Go；如果由 Nginx 提供静态文件，应按相同顺序配置这些资源目录，页面映射到 `web/`。外网部署由反向代理提供 HTTPS。

命令行发布和 Go 服务支持 `-assets` 指定资源根目录，并支持 `-theme` 指定主题目录；默认主题是 `../assets/theme/blue`（从 `backend/` 启动）。资源更新直接生效，不随 HTML 发布回滚。备份应包括 `data/site.db` 和运行时的 `assets/images/`；主题资源由 Git 管理。

登录仅接受 Argon2id。新导入 Access 后必须重新设置管理员密码：

```sh
cd backend
go run ./cmd/reset-password -username bilvie -password '新的密码'
```

目前新闻后台仍是查看列表；发布器已经读取新闻正文、分类、SEO 字段并生成页面。新闻编辑、图片上传和模板/站点配置的后台编辑页面仍需后续补齐；可先通过数据库维护相应内容后发布。新增业务图片应保存到 `assets/images/`，主题图片应保存到 `assets/theme/blue/images/`，数据库字段统一使用 `/images/文件名`。
