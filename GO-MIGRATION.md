# Go + SQLite 迁移

项目已拆分为 `backend/`、`frontend/`、`web/`、`assets/`、`data/` 和原系统归档 `legacy/`。主题资源位于 `assets/theme/blue/`，业务图片位于不纳入 Git 的 `assets/images/`，公开地址统一为 `/images/文件名`。

完整启动方式、静态生成规则、发布行为及当前功能边界见 [README.md](README.md)。

重新导入 Access（不要覆盖正在使用且已编辑的数据库）：

```sh
brew install mdbtools
cd backend
go run ./cmd/migrate -access '../legacy/database/kerfm!!@@##.asa' -sqlite '../data/site.db'
```

默认拒绝覆盖已有 SQLite。当前数据库保留 15 张业务表和迁移内容；管理员密码已改为 Argon2id，Access 导入的旧 MD5 密码需要重置后才能登录。

已有数据库可运行 `go run ./cmd/normalize-images`，将旧的 `/uploadfile/`、`/UploadFile/` 和 `produppic` 图片路径迁移到 `/images/`。
