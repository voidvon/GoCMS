# 模板数据调用与帝国灵动标签对照

参考帝国 CMS `e/class/functions.php` 的 `DoRepEcmsLoopBq`：`[e:loop]` 被替换为 PHP 查询循环，提供 `$bqr`、`$bqsr` 和 `$bqno`。GoCMS 不执行 PHP，也不提供 SQL 字符串调用。

| 能力 | GoCMS |
| --- | --- |
| 当前栏目分页列表 | 已有 `listItems`、`listPagination` |
| 导航和子栏目 | 已有 `navigation`、`listChildren` |
| 推荐、相关内容 | 已有 `featuredItems`、`relatedItems` |
| 标签模板复用 | 已有 `label` |
| 任意栏目内容调用 | 新增 `contentItems` |
| 只显示有图片的内容 | 新增 `contentItemsWithImage`，兼容 `[e:loop]` 图片标志 |
| 循环序号、首末标记 | ListItem 新增 `Index`（从 1 起）、`First`、`Last` |
| 自定义字段 | ListItem 新增 `Fields`，值按文本转义；复杂数组/对象暂无类型化输出 |
| 条件、空结果和嵌套 | Go 模板 `if`、`range`、`else` |
| 帝国 PHP、SQL 和原始标签语法 | 仅 `[e:loop]` 的数据参数有受限转换，其余需要改写模板 |

## 受限 `[e:loop]` 迁移语法

生成器会先识别帝国 CMS 的 `[e:loop={...}]...[/e:loop]`，只转换数据调用部分。栏目模板可使用以下形式，循环体必须是 Go 模板：

```html
[e:loop={71,10,2,0,,newstime desc}]<a href="{{.URL}}">{{.Title}}</a>[/e:loop]
```

它等价于 `contentItemsWithImage 71 10 true false true "newest"`。支持操作类型 `2`（栏目）、空条件、图片筛选，以及 `sort`、`newstime/id asc`、`newstime/id desc` 排序。栏目 ID、条数和排序都会校验。

帝国循环体中的 `$bqr`、`$bqsr`、PHP 代码、任意 SQL 条件、标题分类/数据表操作会让生成失败，并保留旧网站。这是迁移辅助语法，不是 PHP 兼容层；应将循环体改写为 `.Title`、`.URL`、`.Fields` 等 Go 模板字段。

## 全站最新内容

```gotemplate
{{range contentItems 0 10 true false "newest"}}
  <article>
    <span>{{.Index}}</span>
    <a href="{{.URL}}">{{.Title}}</a>
    {{with index .Fields "material"}}<p>{{.}}</p>{{end}}
  </article>
{{else}}
  <p>暂无内容</p>
{{end}}
```

参数依次为栏目 ID（0 为全站）、条数（1–500）、是否包含子栏目、是否只取推荐内容、排序。排序仅接受 `sort`（已有栏目排序）、`newest`、`oldest`。时间相同时按内容 ID 确定顺序。栏目 ID 应由配置或当前模板数据传入，而不是由核心推断业务含义。

函数只返回公开内容，沿用当前语言的发布快照和栏目路由，不修改分页、不输出列表 HTML。参数错误使模板执行失败，并沿用现有发布失败保护。`Fields` 是已转义文本，不应作为富文本或直接当作 URL 输出。

本批不支持任意字段筛选、多栏目联合查询、SQL 排序表达式、PHP 代码，以及帝国模板原样导入。`RowStart`/`RowEnd` 为已有兼容字段，新模板应使用序号和 CSS 自行决定列数。
