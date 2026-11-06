# Repository Guidelines

## 项目目标与架构约束

GoCMS 是主题驱动的通用 CMS。当前站点仅是一个使用案例，新增功能必须适用于其他行业、栏目和主题。

- 核心负责内容、分类、权限、路由和发布；主题负责前台布局、样式、文案及展示规则。
- 新增功能所需的业务配置、密钥及文件导入、资源上传和备份应提供有权限控制的后台入口，不应要求管理员手动修改服务器文件或环境变量。支付模块先完整遵循此要求，后续新增功能同样执行。
- 禁止根据站点域名、分类名称、固定 ID、旧表名或路径推断业务含义。例如不得写入 `case "service": return "阀门知识"`，也不得根据路径强制选择模板。
- 分类名称、排序、分页、路由及模板绑定来自配置或数据库；列表和详情分别遵循 `list_template`、`detail_template`。
- 生成器向主题提供结构化数据，例如 `listItems`、`listChildren`、`listPagination`。列表 HTML、列数、摘要展示长度和缩略图规则由主题决定。
- 旧站映射应隔离在一次性导入逻辑中。已有兼容代码不代表推荐架构，不得继续扩展站点专属分支。

## 项目结构

- `backend/cmd/`：服务、生成、迁移等 Go 命令；`backend/internal/`：业务模块，测试与源码同目录。
- `backend/templates/`：无主题包时使用的默认 Go 模板；`assets/<site_id>/themes/<id>/`：已安装主题及其资源。
- `frontend/src/`：React + TypeScript 管理后台；业务组件放 `components/app/`。
- `storefront/src/`：独立 Vite + React 购物前台，入口 `/shop/`；通过别名复用后台官方 UI 源码，业务页面与后台分开，仅负责购物车、结算、订单和登录注册；商品列表、详情和规格选择复用主题生成的 HTML，销售配置沿用分类和内容。
- `scripts/`：资源同步和 Release 脚本；`data/`、`assets/<site_id>/uploads/`、`assets/images/`、`web/` 分别保存运行数据、站点附件、历史图片和生成页面。

## 开发与验证命令

- 首次开发分别执行 `cd frontend && npm ci`、`cd storefront && npm ci`。
- `make dev` 启动三个开发服务；`make backend`、`make frontend`、`make storefront` 分别启动 Go、Vite 后台与 Vite 购物前台。
- `make build`：构建两个 React 应用、嵌入资源并生成 `bin/site`。
- `make generate`：按当前数据和主题重新生成网站，会替换 `web/`。
- `make test`：前端构建、资源同步、Go 测试及 `go vet`。
- `cd frontend && npm run lint`：执行 Oxlint。
- `make release-dry-run`：预览版本；`make release`：通过已登录的 `gh` 发布，要求工作区干净。

## 编码与测试规范

Go 使用 `gofmt`；包名小写，导出标识符使用 PascalCase。TypeScript 遵循现有双空格缩进、双引号和省略分号风格；React 组件使用 PascalCase，业务函数使用 camelCase。保持官方 UI 组件原样，通过业务组件封装。

Go 测试使用标准 `testing`，文件命名为 `*_test.go`，测试函数为 `TestXxx`。修改生成器时覆盖自定义模板绑定、任意分类名称及路径、分页和发布失败场景。未规定覆盖率门槛；优先验证行为。页面变更还需检查生成结果及视觉效果。

## 前端 UI 规范

- `storefront/` 的界面文案、无障碍标签、状态及错误使用 `src/i18n/` 中的 i18next 翻译键；新增文案同步补齐全部语言目录，使用插值及复数规则，不拼接特定语言的句子。商品和规格数据由内容系统提供，不在 UI 字典中翻译。金额、数量与日期通过统一工具按当前语言格式化；布局使用逻辑方向属性，兼容阿拉伯语 RTL。站点默认语言沿用后台语言管理的主语言，不依据域名、路径或分类猜测。前台语言通过公共模板写入按站点隔离的 localStorage，storefront 的手动偏好独立保存且优先，支持恢复跟随；普通购物入口不传递语言参数。

- 后台操作的成功和失败反馈统一复用 shadcn/ui Sonner Toast：通过 `components/app/admin-notifications.ts` 的 `showSuccess` / `showError` 调用，使用应用已挂载的 `AdminToaster` 在视口上方居中显示（`position="top-center"`）。不要为单个页面重复挂载 Toaster，也不要用 `alert()` 或重复的页面／弹窗错误条代替操作反馈；字段校验和持续性的配置状态说明可留在原位置。
- 接口错误需提取可读的 `error` 或 `error.message` 文本，复用 `lib/errors.ts` 的 `getErrorMessage`；禁止直接把错误对象转成字符串，避免显示 `[object Object]`。
- 所有可滚动区域统一使用 `@/components/ui/scroll-area` 的 `ScrollArea`，不要直接给容器添加 `overflow-y-auto`、`overflow-auto`、`overflow-x-auto` 等浏览器原生滚动条。
- 垂直滚动使用默认 `orientation="vertical"`；表格、代码等横向滚动使用 `orientation="horizontal"`；双向滚动使用 `orientation="both"`。
- 高度、最大高度、宽度约束放在 `className`；列表间距、内边距等放在 `contentClassName`。
- 仅第三方组件内部无法替换的滚动容器可以例外，并在 PR 中说明原因。
- 历史页面中的原生滚动容器在相关功能改动时一并迁移，不要求无关改动一次性全量重写。
- 评审新页面或组件时，将“是否使用 `ScrollArea`”作为 UI 检查项；浏览器的原生滚动条样式不统一，不作为产品界面使用。

## 提交与 PR

沿用历史提交格式，如 `feat(backend): ...`、`fix(frontend): ...`、`refactor(backend): ...`。PR 说明问题、修改后的行为、验证命令及结果；有关联问题时附链接，界面变更附截图，数据迁移说明兼容影响。

## 资源与配置边界

程序支持单文件可执行文件，用户可编辑的主题、模板及数据保持外置，更新不得覆盖。禁止直接公开 `assets/<site_id>/themes/` 原始目录、模板源码或 `data/`；主题静态资源通过配置的公开资源路由提供。勿提交数据库、业务图片、生成页面或凭据。

主题包是 ZIP 文件，根目录必须包含 `theme.json`，并包含 `templates/` 下的 HTML 模板；公开资源放在 `assets/` 下的 `css/`、`js/`、`skin/`、`images/` 等目录。`theme.json` 至少提供唯一的字母数字、点、短横线或下划线组成的 `id`；可选提供 `name`、`version`、`description`、`author`。后台导入会校验归档大小、文件数量、路径和符号链接，导出使用同一格式。切换前会检查数据库中已配置的全局模板、`list_template` 和 `detail_template` 是否存在，主题作者应提供这些配置所引用的模板。活动主题记录在 `data/theme.json`，服务启动和 `make generate` 必须读取同一配置。
