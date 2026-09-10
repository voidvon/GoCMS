# Repository Guidelines

## 项目目标与架构约束

GoCMS 是主题驱动的通用 CMS。当前站点仅是一个使用案例，新增功能必须适用于其他行业、栏目和主题。

- 核心负责内容、分类、权限、路由和发布；主题负责前台布局、样式、文案及展示规则。
- 禁止根据站点域名、分类名称、固定 ID、旧表名或路径推断业务含义。例如不得写入 `case "service": return "阀门知识"`，也不得根据路径强制选择模板。
- 分类名称、排序、分页、路由及模板绑定来自配置或数据库；列表和详情分别遵循 `list_template`、`detail_template`。
- 生成器向主题提供结构化数据，例如 `listItems`、`listChildren`、`listPagination`。列表 HTML、列数、摘要展示长度和缩略图规则由主题决定。
- 旧站映射应隔离在一次性导入逻辑中。已有兼容代码不代表推荐架构，不得继续扩展站点专属分支。

## 项目结构

- `backend/cmd/`：服务、生成、迁移等 Go 命令；`backend/internal/`：业务模块，测试与源码同目录。
- `backend/templates/`：无主题包时使用的默认 Go 模板；`assets/theme/<id>/`：已安装主题及其资源。
- `frontend/src/`：React + TypeScript 管理后台；业务组件放 `components/app/`。
- `scripts/`：资源同步和 Release 脚本；`data/`、`assets/images/`、`web/` 分别保存运行数据、业务图片和生成页面。

## 开发与验证命令

- 首次开发执行 `cd frontend && npm ci`。
- `make backend`、`make frontend`：分别启动 Go 服务和 Vite 后台。
- `make build`：构建后台并生成 `bin/site`。
- `make generate`：按当前数据和主题重新生成网站，会替换 `web/`。
- `make test`：前端构建、资源同步、Go 测试及 `go vet`。
- `cd frontend && npm run lint`：执行 Oxlint。
- `make release-dry-run`：预览版本；`make release`：通过已登录的 `gh` 发布，要求工作区干净。

## 编码与测试规范

Go 使用 `gofmt`；包名小写，导出标识符使用 PascalCase。TypeScript 遵循现有双空格缩进、双引号和省略分号风格；React 组件使用 PascalCase，业务函数使用 camelCase。保持官方 UI 组件原样，通过业务组件封装。

Go 测试使用标准 `testing`，文件命名为 `*_test.go`，测试函数为 `TestXxx`。修改生成器时覆盖自定义模板绑定、任意分类名称及路径、分页和发布失败场景。未规定覆盖率门槛；优先验证行为。页面变更还需检查生成结果及视觉效果。

## 提交与 PR

沿用历史提交格式，如 `feat(backend): ...`、`fix(frontend): ...`、`refactor(backend): ...`。PR 说明问题、修改后的行为、验证命令及结果；有关联问题时附链接，界面变更附截图，数据迁移说明兼容影响。

## 资源与配置边界

程序支持单文件可执行文件，用户可编辑的主题、模板及数据保持外置，更新不得覆盖。禁止直接公开 `assets/theme/` 原始目录、模板源码或 `data/`；主题静态资源通过配置的公开资源路由提供。勿提交数据库、业务图片、生成页面或凭据。

主题包是 ZIP 文件，根目录必须包含 `theme.json`，并包含 `templates/` 下的 HTML 模板；公开资源放在 `assets/` 下的 `css/`、`js/`、`skin/`、`images/` 等目录。`theme.json` 至少提供唯一的字母数字、点、短横线或下划线组成的 `id`；可选提供 `name`、`version`、`description`、`author`。后台导入会校验归档大小、文件数量、路径和符号链接，导出使用同一格式。切换前会检查数据库中已配置的全局模板、`list_template` 和 `detail_template` 是否存在，主题作者应提供这些配置所引用的模板。活动主题记录在 `data/theme.json`，服务启动和 `make generate` 必须读取同一配置。
