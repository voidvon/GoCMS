package legacyimport

import (
	"fmt"
	"strings"
)

type columnType int

const (
	columnText columnType = iota
	columnInteger
	columnDateTime
)

type column struct {
	name       string
	typeOf     columnType
	primaryKey bool
}

type table struct {
	Name    string
	Columns []column
}

// Tables describes the source database accepted by the one-time importer.
// These names intentionally stay inside this package; the running CMS never
// creates or queries any of them.
var Tables = []table{
	{
		Name: "benming_ch_Cocat",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true},
			{name: "coname", typeOf: columnText}, {name: "root", typeOf: columnInteger},
			{name: "orderid", typeOf: columnInteger}, {name: "sitepath", typeOf: columnInteger},
			{name: "siteurl", typeOf: columnText}, {name: "Centern", typeOf: columnText},
		},
	},
	{
		Name: "benming_ch_config",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true},
			{name: "WebName", typeOf: columnText}, {name: "WebUrl", typeOf: columnText},
			{name: "WebIcp", typeOf: columnText}, {name: "WebQQ", typeOf: columnText},
			{name: "WebMsn", typeOf: columnText}, {name: "Webauthor", typeOf: columnText},
			{name: "WebCopyright", typeOf: columnText}, {name: "CoName", typeOf: columnText},
			{name: "CoAdd", typeOf: columnText}, {name: "CoPost", typeOf: columnText},
			{name: "CoPhone", typeOf: columnText}, {name: "CoFax", typeOf: columnText},
			{name: "CoRen", typeOf: columnText}, {name: "CoEmail", typeOf: columnText},
			{name: "benming", typeOf: columnText},
		},
	},
	{
		Name: "benming_ch_Contact",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true},
			{name: "offname", typeOf: columnText}, {name: "address", typeOf: columnText},
			{name: "phone", typeOf: columnText}, {name: "fax", typeOf: columnText},
			{name: "linkren", typeOf: columnText}, {name: "Email", typeOf: columnText},
			{name: "Post", typeOf: columnText},
		},
	},
	{
		Name:    "benming_ch_cuskind",
		Columns: []column{{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "kindname", typeOf: columnText}},
	},
	{
		Name: "benming_ch_cuslabel",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "lname", typeOf: columnText},
			{name: "ldes", typeOf: columnText}, {name: "lcontent", typeOf: columnText},
			{name: "lkind", typeOf: columnInteger}, {name: "lidate", typeOf: columnDateTime},
		},
	},
	{
		Name: "benming_ch_job",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "jobName", typeOf: columnText},
			{name: "address", typeOf: columnText}, {name: "jobnob", typeOf: columnInteger},
			{name: "jobneed", typeOf: columnText}, {name: "linkren", typeOf: columnText},
			{name: "phone", typeOf: columnText}, {name: "state", typeOf: columnInteger},
			{name: "date", typeOf: columnDateTime},
		},
	},
	{
		Name: "benming_ch_MetaType",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "typename", typeOf: columnText},
			{name: "meta_keywords", typeOf: columnText}, {name: "meta_descriptions", typeOf: columnText},
			{name: "title", typeOf: columnText},
		},
	},
	{
		Name: "benming_ch_Msg",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "Title", typeOf: columnText},
			{name: "linkren", typeOf: columnText}, {name: "phone", typeOf: columnText},
			{name: "mobile", typeOf: columnText}, {name: "fax", typeOf: columnText},
			{name: "email", typeOf: columnText}, {name: "content", typeOf: columnText},
			{name: "date", typeOf: columnDateTime}, {name: "address", typeOf: columnText},
			{name: "state", typeOf: columnInteger}, {name: "statedate", typeOf: columnDateTime},
			{name: "prodid", typeOf: columnInteger},
		},
	},
	{
		Name: "benming_ch_news",
		Columns: []column{
			{name: "newsid", typeOf: columnInteger, primaryKey: true}, {name: "Title", typeOf: columnText},
			{name: "Content", typeOf: columnText}, {name: "Typeid", typeOf: columnInteger},
			{name: "Tjnews", typeOf: columnInteger}, {name: "Nfrom", typeOf: columnText},
			{name: "Picture", typeOf: columnText}, {name: "Dateandtime", typeOf: columnDateTime},
			{name: "hits", typeOf: columnInteger}, {name: "tjhome", typeOf: columnInteger},
			{name: "homepic", typeOf: columnInteger}, {name: "html_pass", typeOf: columnInteger},
			{name: "homehot", typeOf: columnInteger}, {name: "upsize_ts", typeOf: columnText},
			{name: "pictext", typeOf: columnInteger}, {name: "key", typeOf: columnText},
			{name: "desc", typeOf: columnText},
		},
	},
	{
		Name: "benming_ch_NewsCat",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "CatName", typeOf: columnText},
			{name: "Root", typeOf: columnInteger}, {name: "ORderID", typeOf: columnInteger},
		},
	},
	{
		Name: "benming_ch_prod",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "prodName", typeOf: columnText},
			{name: "prodCode", typeOf: columnText}, {name: "CatId", typeOf: columnInteger},
			{name: "remark", typeOf: columnText}, {name: "itemize", typeOf: columnText},
			{name: "smallpic", typeOf: columnText}, {name: "bigpic", typeOf: columnText},
			{name: "key", typeOf: columnText}, {name: "orderid", typeOf: columnInteger},
			{name: "tjhome", typeOf: columnInteger}, {name: "show", typeOf: columnInteger},
		},
	},
	{
		Name: "benming_ch_ProdCat",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "CatName", typeOf: columnText},
			{name: "Root", typeOf: columnInteger}, {name: "Orderid", typeOf: columnInteger},
			{name: "key", typeOf: columnText}, {name: "desc", typeOf: columnText},
			{name: "pic", typeOf: columnText}, {name: "over_pic", typeOf: columnText},
		},
	},
	{
		Name: "benming_ch_prodphoto",
		Columns: []column{
			{name: "id", typeOf: columnInteger, primaryKey: true}, {name: "photoName", typeOf: columnText},
			{name: "photopic", typeOf: columnText}, {name: "date", typeOf: columnDateTime},
		},
	},
	{
		Name: "benming_ch_worldec_Temp",
		Columns: []column{
			{name: "Id", typeOf: columnInteger, primaryKey: true}, {name: "home_index", typeOf: columnText},
			{name: "Co_index", typeOf: columnText}, {name: "produts_index", typeOf: columnText},
			{name: "produts_sort1", typeOf: columnText}, {name: "produts_sort2", typeOf: columnText},
			{name: "produts_sort3", typeOf: columnText}, {name: "produts_detail", typeOf: columnText},
			{name: "job_index", typeOf: columnText}, {name: "Job_sort", typeOf: columnText},
			{name: "Job_detail", typeOf: columnText}, {name: "news_index", typeOf: columnText},
			{name: "News_sort1", typeOf: columnText}, {name: "news_detail", typeOf: columnText},
			{name: "service_index", typeOf: columnText}, {name: "service_sort1", typeOf: columnText},
			{name: "service_detail", typeOf: columnText}, {name: "msg_index", typeOf: columnText},
			{name: "Contact", typeOf: columnText}, {name: "Shou_index", typeOf: columnText},
			{name: "tempname", typeOf: columnText}, {name: "selected", typeOf: columnInteger},
		},
	},
	{
		Name: "benming_master",
		Columns: []column{
			{name: "Id", typeOf: columnInteger, primaryKey: true}, {name: "UserName", typeOf: columnText},
			{name: "PassWord", typeOf: columnText}, {name: "Flag", typeOf: columnText},
			{name: "LastLogin", typeOf: columnDateTime}, {name: "LastLoginIp", typeOf: columnText},
		},
	},
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func columnSQLType(item column) string {
	if item.typeOf == columnInteger {
		return "INTEGER"
	}
	return "TEXT"
}

func (table table) createSQL() string {
	definitions := make([]string, 0, len(table.Columns))
	for _, item := range table.Columns {
		definition := fmt.Sprintf("%s %s", quoteIdentifier(item.name), columnSQLType(item))
		if item.primaryKey {
			definition += " PRIMARY KEY"
		}
		definitions = append(definitions, definition)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", quoteIdentifier(table.Name), strings.Join(definitions, ", "))
}
