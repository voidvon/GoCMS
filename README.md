# GoCMS 静态 CMS

Go + SQLite 内容服务、React 管理后台、Go 模板静态发布。

## 目录

- `backend/`：Go 模块，`cmd/site` 服务、`cmd/generate` 发布、`cmd/migrate` 导入、`cmd/reset-password` 密码重置。
- `backend/templates/`：无主题包时使用的默认 Go 模板；页面布局在主题模板中维护。发布版会把默认模板编入可执行文件，并在首次运行时初始化到外部目录，之后保留外部修改。
- `frontend/`：Vite + React 管理后台，shadcn Base UI Nova；官方组件保持原样，业务封装放 `src/components/app/`。
- `assets/theme/<id>/`：已安装主题的 CSS、JS、skin、模板和固定图片，属于可更改的外部资源；升级程序不会覆盖它。
- `assets/images/`：内容封面、正文和详情介绍图，属于运行时业务资源，不纳入 Git；服务直接读取，不参与发布复制。
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
后台页面内“网站发布”支持全站重新生成；内容编辑支持“仅保存”和“保存并发布”。隐藏内容会自动提交发布任务。已有任务运行时，新的保存发布请求会排队，在当前任务结束后再读取最新数据生成。

```sh
make generate  # 命令行全站生成
make test      # Go 测试/vet + 前端构建
make release-dry-run  # 查看下一次 Release 版本，不修改仓库
make release          # 更新版本、创建 tag 并发布 GitHub Release
```

后台“主题模板”支持切换、导入和导出主题。主题包为 ZIP，根目录包含 `theme.json`、`templates/` 和 `assets/`；模板由主题自行定义，资源按需放在 `assets/css/`、`assets/js/`、`assets/skin/`、`assets/images/`。切换前会检查全局页面绑定的模板，以及每个分类当前类型对应的 `list_template`、`cover_template` 或 `detail_template`，主题包需提供这些路径对应的模板。导入后主题保存在 `assets/theme/<id>/`，切换会记录到 `data/theme.json` 并自动重新生成网站。主题包不包含数据库、业务图片或 `web/`。

版本号保存在 `frontend/package.json`，从 `0.1.0` 开始。`make release` 首次发布 `v0.1.0`，之后每次递增 patch 版本；`0.1.99` 之后进入 `0.2.0`。发布前需要保持 Git 工作区干净，并确保已通过 `gh auth login` 登录 GitHub，且已安装 `zip` 命令。Release 每个平台上传一个 `gocms-<tag>-<os>-<arch>.zip` 压缩包，包内包含对应平台的单文件可执行文件；可以使用 `RELEASE_REMOTE=upstream make release` 指定其他 Git remote。

后台“关于 GoCMS”中的“检查更新”会读取 GitHub 最新 Release。服务检测到对应平台的新版本后会下载 ZIP 压缩包、解压可执行文件、替换当前程序并自动重启；主题、模板、业务图片、数据库和已发布网站目录都不会被更新流程覆盖。只支持裸二进制下载的旧版本需要先手动下载压缩包并解压替换程序一次，之后即可自动更新。

## 发布规则

| 数据 | 模板 | 文件 |
| --- | --- | --- |
| 站点配置、SEO、自定义片段和推荐内容 | `index.html` | `/index.html` |
| 公开内容 (`visible=1`) | 分类绑定的详情模板 | 由内容所属分类的详情目录和文件名规则决定 |
| 栏目内容（包含后代内容） | 分类绑定的列表模板 | 由分类的列表目录和文件名规则决定 |
| 公司介绍 (`root=32`) | corporation | `/about/About-{id}.html` |
| 有效招聘 (`state=1`) | job_sort / job_detail | `/job/index.html`、`/job/detail/{id}.html` |
| 栏目封面页 | 栏目配置的 `cover_template` | 由栏目 URL 目录和文件名规则决定 |
| 留言表单 | message | `/msg.html` |
| 已生成页面 | 内置 | `/Sitemap.xml`、`/sitemap.html` |

每个分类可以设置栏目类型。列表式栏目使用 `list_template`，按每页数量生成分页；第一页同时生成 `{id}.html` 和 `{id}-1.html`，后续为 `{id}-{page}.html`。封面式栏目使用 `cover_template`，只生成一页，文件名可以是固定值（如 `contact.html`），目录留空时直接生成在站点根目录。首页读取统一的推荐内容标签；网站地图从实际生成的地址产生。

所有内容统一使用 `gocms_content`，所有栏目统一使用 `gocms_category`，留言统一使用 `gocms_message`。每个分类保存自己的栏目类型、列表/封面模板、详情模板、静态目录和文件名规则；内容只保存所属分类 ID，发布器按分类解析路径。现有数据库中的 `/valve`、`/Products`、`/Product`、`/news`、`/service` 等线上目录作为分类配置保留，新建分类使用 `category`、`content` 等通用默认值。旧 Access 表只在 `cmd/migrate` 的一次性导入阶段读取，服务启动和发布过程不会读取旧内容表或旧留言表。

生成使用一致的 SQLite 读事务快照。先完成所有模板渲染和 UTF-8 检查，再向临时目录写入新 HTML 和站点地图，最后切换 `web/`。新目录只包含本次生成结果，因此隐藏/删除内容及过期分页会清理；独立资源目录不被修改。未知模板标签或模板错误会终止发布，保持旧站点。发布锁用于防止 CLI/API 并发写入。

目录切换使用同文件系统的两次 rename；切换间可能存在极短的无目录窗口，并非整个站点的单次原子交换。安装失败会尝试恢复 `web.previous`。若进程在切换时中断并留下 `web.previous`，先检查 `web/` 是否完整，再恢复或归档备份后重新发布。发布结果保存在 `data/publish.json`，后台也会展示生成数量、状态和失败原因。

模板与自定义 HTML 片段属于受信任的管理员内容。普通字段会 HTML 转义，富文本正文按 HTML 输出。图片统一使用 `/images/文件名`，服务先查找 `assets/images/`，再查找当前主题的 `images/` 目录；数据库导入和发布时会把旧图片地址规范化为这个路径。

## 生产运行

```sh
make build
cd backend
../bin/site -addr 127.0.0.1:18080
```

Go 同时提供 `/admin/`（发布版内置前端）、`/api/` 和公开静态站点。`/images/` 依次映射到 `assets/images/` 和当前主题的 `images/`，`/css/`、`/js/`、`/skin/` 映射到当前主题对应目录，网站页面读取 `web/`。发布不复制图片，也不创建符号链接。旧的 `/uploadfile/` 和 `/UploadFile/` 地址不再提供服务。Nginx 可直接反向代理 Go；如果由 Nginx 提供静态文件，应按相同顺序配置这些资源目录，页面映射到 `web/`。外网部署由反向代理提供 HTTPS。

命令行发布和 Go 服务支持 `-assets` 指定资源根目录，并支持 `-theme` 指定主题目录覆盖活动主题配置。没有覆盖参数时，程序从 `assets/theme/` 和 `data/theme.json` 解析活动主题；没有活动记录时选择已安装主题中的第一个。发布版可执行文件会把管理后台和默认 Go 模板编入自身，模板首次运行时写入外部 `templates/`；Release 单文件不包含主题目录，部署时需要在外部 `assets/theme/<id>/` 提供主题资源，修改后直接生效。备份应包括 `data/site.db`、运行时的 `assets/images/`、`assets/theme/` 和 `templates/`。

登录仅接受 Argon2id。新导入 Access 后必须重新设置管理员密码：

```sh
cd backend
go run ./cmd/reset-password -username gocms -password '新的密码'
```

后台的分类页面统一维护栏目树，内容编辑从统一分类树中选择所属栏目，发布器读取统一内容、分类和 SEO 字段生成页面。新增业务图片应保存到 `assets/images/`，主题图片应保存到当前主题的 `images/` 目录，数据库字段统一使用 `/images/文件名`。
