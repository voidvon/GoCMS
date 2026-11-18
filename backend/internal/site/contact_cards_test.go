package site

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"gocms/internal/sitecontacts"
)

func TestContactCardsAPIKeepsOrderedSiteLanguageReferences(t *testing.T) {
	server, _, token := newLanguageTestServer(t)
	call := func(method, path, data string, status int) []byte {
		t.Helper()
		res := adminRequest(t, server, token, method, path, data)
		if res.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, res.Code, status, res.Body.String())
		}
		return res.Body.Bytes()
	}
	const cardsPath = "/api/admin/languages/contact-cards"
	const langsPath = "/api/admin/languages"
	var library contactCardsPayload
	if err := json.Unmarshal(call(http.MethodGet, cardsPath, "", 200), &library); err != nil || len(library.Cards) != 0 || library.Revision == "" {
		t.Fatalf("empty library: %+v, %v", library, err)
	}
	initial := library.Revision
	library.Cards = []sitecontacts.Card{
		{ID: "first", Name: "First", Email: "sales@example.test"},
		{ID: "second", Name: "Second", Phone: "+44 7707 709941"},
	}
	putLibrary := func(status int) {
		t.Helper()
		body, err := json.Marshal(library)
		if err != nil {
			t.Fatal(err)
		}
		result := call(http.MethodPut, cardsPath, string(body), status)
		if status == http.StatusOK {
			if err := json.Unmarshal(result, &library); err != nil {
				t.Fatal(err)
			}
		}
	}
	putLibrary(http.StatusOK)
	var langs []LanguageItem
	if err := json.Unmarshal(call(http.MethodGet, langsPath, "", 200), &langs); err != nil || len(langs) != 1 {
		t.Fatalf("languages: %+v, %v", langs, err)
	}
	url := fmt.Sprintf("%s/%d", langsPath, langs[0].ID)
	const language = `"code":"zh-CN","name":"简体中文","is_enabled":1,"is_default":1,"is_fallback":1`
	call(http.MethodPut, url, "{"+language+`,"contact_card_ids":["second","first"]}`, 200)
	call(http.MethodPut, url, "{"+language+"}", 200)
	call(http.MethodPut, url, "{"+language+`,"contact_card_ids":["missing"]}`, 400)
	checkOrder := func(want []string) {
		t.Helper()
		if err := json.Unmarshal(call(http.MethodGet, langsPath, "", 200), &langs); err != nil || !reflect.DeepEqual(langs[0].ContactCardIDs, want) {
			t.Fatalf("saved choices: %+v, %v; want %v", langs, err, want)
		}
	}
	checkOrder([]string{"second", "first"})
	cards := library.Cards
	library.Cards = cards[1:]
	putLibrary(http.StatusConflict)
	library.Revision = initial
	putLibrary(http.StatusConflict)
	call(http.MethodPut, "/api/admin/site-settings", `{"contact_cards":"[]"}`, 400)
	call(http.MethodPut, url, "{"+language+`,"contact_card_ids":[]}`, 200)
	checkOrder([]string{})
}
