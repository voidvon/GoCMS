# Go + SQLite 迁移

项目已拆分为 `backend/`、`frontend/`、`web/`、`assets/`、`data/` 和原系统归档 `legacy/`。模板组资源位于 `assets/theme/blue/`，业务图片位于不纳入 Git 的 `assets/images/`，公开地址统一为 `/images/文件名`。

完整启动方式、静态生成规则、发布行为及当前功能边界见 [README.md](README.md)。

重新导入 Access（不要覆盖正在使用且已编辑的数据库）：

```sh
brew install mdbtools
cd backend
go run ./cmd/migrate -access '../legacy/database/kerfm!!@@##.asa' -sqlite '../data/site.db'
```

默认拒绝覆盖已有 SQLite。导入命令把旧表一次性转换为 `gocms_category`、`gocms_content` 和 `gocms_message`；Go 服务之后只读取统一表。管理员密码使用 Argon2id，Access 导入的旧 MD5 密码需要重置后才能登录。

运行时不会读取旧表、旧模板或旧资源路径。旧系统的 `benming_ch_cuslabel` 和 `benming_ch_cuskind` 会在一次性迁移阶段分别转换到 `gocms_template_label` 和 `gocms_template_label_category`，旧的 `#BM_xxx#` 调用名会转换为 `{{label "bm-xxx" .}}`，发布器只读取统一标签表。模板管理中的标签模板与首页、封面、列表、内容和其他模板属于同一套模板入口。需要从原系统重新迁移时，请重新执行上面的导入命令；导入器会在一次性转换阶段写入统一表和 `/images/` 资源路径，生成的 SQLite 不保留源表。

如果数据库已经由早期版本导入且仍保留旧表，可执行以下命令补迁站点设置和模板标签：

```sh
cd backend
go run ./cmd/migrate-settings -db '../data/site.db'
```
