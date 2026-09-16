# GoCMS 模板语法参考

模板使用 Go `html/template` 语法。模板文件位于主题包的 `templates/`，输出会自动生成静态 HTML。

## 基础控制语法

```gotemplate
{{if .Title}}<h1>{{.Title}}</h1>{{else}}无标题{{end}}
{{range listItems .}}<a href="{{.URL}}">{{.Title}}</a>{{else}}暂无内容{{end}}
{{with listPagination .}}共 {{.Total}} 条{{end}}
{{template "card.html" .}}
{{include "header.html" .}}
```

支持 `if`、`else`、`range`、`with`、`define`、`template`、变量赋值（`{{$x := .Title}}`）、`index`、`len`、`and`、`or`、`not`、比较运算及管道。

## 内置函数

### 全局与复用

| 函数 | 用法 |
|---|---|
| `setting` | `{{setting "site_name"}}`，读取并转义站点设置 |
| `settingHTML` | `{{settingHTML "footer_html"}}`，输出受信任的原始 HTML |
| `include` | `{{include "header.html" .}}`，渲染公共模板 |
| `label` | `{{label "card" .}}`，渲染后台标签模板 |
| `currentLang` | `{{currentLang}}`，当前语言代码 |
| `languages` | `{{range languages}}...{{end}}`，语言导航，字段 `.Code`、`.Name`、`.URL`、`.Current` |

### 导航与栏目

`navigation` 默认位置为 `main`，可传 `main`、`top`、`footer` 等位置。第二个参数控制深度：`none` 仅顶级、`children` 一级子级、`all` 全部递归；省略时为 `all`。

```gotemplate
{{range navigation "main" "children"}}<a href="{{.URL}}">{{.Name}}</a>{{end}}
```

项目：`.URL`、`.Name`、`.Position`、`.Children`。

| 函数 | 上下文 | 说明 |
|---|---|---|
| `listCategories` | 列表 | 当前根栏目下的同级栏目；`.Current`、`.Last` |
| `listChildren` | 栏目 | 当前栏目的直属子栏目 |
| `catalogCategories` | 所有 | 当前站点根栏目下的一级栏目 |

栏目项字段：`.URL`、`.Name`、`.Current`、`.Last`、`.RowStart`、`.RowEnd`。

### 内容列表

```gotemplate
{{range listItems .}}{{.Title}}{{end}}
{{range contentItems 0 10 true false "newest"}}{{.Title}}{{end}}
{{range contentItemsWithImage 0 10 true false true "newest"}}{{.Image}}{{end}}
{{range featuredItems 6}}{{.Title}}{{end}}
{{range featuredItemsIn "products" 6}}{{.Title}}{{end}}
{{range relatedItems . 6}}{{.Title}}{{end}}
```

`contentItems` 参数依次为栏目 ID、条数、包含子栏目、仅推荐、排序（`sort`、`newest`、`oldest`）。`contentItemsWithImage` 额外接受只显示有图参数。内容字段包括 `.ID`、`.CategoryID`、`.Index`、`.First`、`.Last`、`.URL`、`.Title`、`.Summary`、`.Excerpt`、`.PublishedAt`、`.Date`、`.Image`、`.Category`、`.Fields`、`.RowStart`、`.RowEnd`。

详情页可用字段：`.title`、`.body`/`.content`（HTML）、`.summary`、`.keywords`、`.description`、`.published_at`、`.date`、`.source`、`.code`、`.cover_image`、`.extra_data` 等。

### 分页

```gotemplate
{{with listPagination .}}
  {{if .HasPrevious}}<a href="{{.PreviousURL}}">上一页</a>{{end}}
  {{range .PageLinks}}<a class="{{if .Current}}active{{end}}" href="{{.URL}}">{{.Number}}</a>{{end}}
  {{if .HasNext}}<a href="{{.NextURL}}">下一页</a>{{end}}
{{end}}
```

分页字段：`.Total`、`.Page`、`.Pages`、`.PageSize`、`.FirstURL`、`.PreviousURL`、`.NextURL`、`.LastURL`、`.HasPrevious`、`.HasNext`、`.PageLinks`（每项 `.Number`、`.URL`、`.Current`）。

### 字段解析

`{{range morepic .morepic}}` 解析多图字段，每项有 `.URL`、`.Alt`；`{{range multiValue .tags}}` 将多值字段拆成字符串数组。

所有文本字段默认已 HTML 转义；正文和 `settingHTML` 保留 HTML。模板错误会使本次发布失败并保留原有网站。
