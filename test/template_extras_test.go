package test

import (
	"strings"
	"testing"
)

func TestTemplateLanguagesStylesAndPreview(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	k := e.provision("sacas")
	tplID := str(e.admOK(admin, "POST", wsBase+"/templates", map[string]any{"slug": "welcome-fr"}).JSON, "id")
	schema := []map[string]any{{"name": "name", "type": "string", "max_len": 80}}
	content := map[string]any{
		"en":    map[string]any{"subject": "Welcome {{.name}}", "text": "Hello {{.name}}", "html": "<html><head></head><body><p>Hello {{.name}}</p></body></html>"},
		"fr":    map[string]any{"subject": "Bienvenue {{.name}}", "text": "Bonjour {{.name}}", "html": "<p>Bonjour {{.name}}</p>"},
		"pt_BR": map[string]any{"subject": "Bem-vindo", "text": "Ola", "html": "<p>Ola</p>"},
	}
	// A language tag must look like one; underscores are not a tag.
	if r := e.adm(admin, "PUT", wsBase+"/templates/"+tplID+"/draft", map[string]any{"variable_schema": schema, "content": content}); r.Code != 400 {
		t.Fatalf("pt_BR: %d %s", r.Code, r.Raw)
	}
	content["pt-br"] = content["pt_BR"]
	delete(content, "pt_BR")
	if r := e.adm(admin, "PUT", wsBase+"/templates/"+tplID+"/draft", map[string]any{"variable_schema": schema, "content": content, "style": "nope"}); r.Code != 400 {
		t.Fatalf("unknown style: %d %s", r.Code, r.Raw)
	}

	// Stylesheets: validation, one per name, listing.
	for name, css := range map[string]string{"@import": "@import url(http://x.example/a.css); p{color:red}", "script": "p{background:url(javascript:alert(1))}",
		"tag": "p{color:red}</style><script>x</script>", "action": "p{color:{{.x}}}", "empty": " ", "unbalanced": "p{color:red", "escape": `p{c\6fl\6fr:red}`} {
		if r := e.adm(admin, "POST", wsBase+"/template-styles", map[string]any{"name": "brand", "css": css}); r.Code != 400 {
			t.Fatalf("css %s: %d %s", name, r.Code, r.Raw)
		}
	}
	if r := e.adm(admin, "POST", wsBase+"/template-styles", map[string]any{"name": "Bad Name", "css": "p{color:red}"}); r.Code != 400 {
		t.Fatalf("style name: %d", r.Code)
	}
	styleID := str(e.admOK(admin, "POST", wsBase+"/template-styles", map[string]any{"name": "brand", "css": "p{color:#0a5}"}).JSON, "id")
	if r := e.adm(admin, "POST", wsBase+"/template-styles", map[string]any{"name": "brand", "css": "p{color:red}"}); r.Code != 409 {
		t.Fatalf("duplicate style: %d", r.Code)
	}
	if n := len(e.admOK(admin, "GET", wsBase+"/template-styles", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("styles: %d", n)
	}
	e.admOK(admin, "PUT", wsBase+"/templates/"+tplID+"/draft", map[string]any{"variable_schema": schema, "content": content, "style": "brand"})
	pv := e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/preview", map[string]any{"locale": "fr", "variables": map[string]any{"name": "Zawadi"}})
	if pv.JSON["subject"] != "Bienvenue Zawadi" || !strings.Contains(str(pv.JSON, "html"), "<style>p{color:#0a5}</style><p>Bonjour Zawadi</p>") {
		t.Fatalf("fr preview: %s", pv.Raw)
	}
	pv = e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/preview", map[string]any{"locale": "en"})
	if h := str(pv.JSON, "html"); !strings.Contains(h, "<style>p{color:#0a5}</style></head>") {
		t.Fatalf("style goes in the head: %s", h)
	}
	// An unknown language falls back to English.
	if pv := e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/preview", map[string]any{"locale": "de"}); !strings.HasPrefix(str(pv.JSON, "subject"), "Welcome") {
		t.Fatalf("fallback: %s", pv.Raw)
	}

	// Published versions keep the CSS they were saved with.
	e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/publish", nil)
	e.admOK(admin, "PUT", wsBase+"/template-styles/"+styleID, map[string]any{"name": "brand", "css": "p{color:#c00}"})
	pv = e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/preview", map[string]any{"locale": "fr"})
	if !strings.Contains(str(pv.JSON, "html"), "#0a5") || strings.Contains(str(pv.JSON, "html"), "#c00") {
		t.Fatalf("published version changed with the stylesheet: %s", pv.Raw)
	}
	e.admOK(admin, "DELETE", wsBase+"/template-styles/"+styleID, nil)
	pv = e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/preview", map[string]any{"locale": "fr"})
	if !strings.Contains(str(pv.JSON, "html"), "#0a5") {
		t.Fatalf("published version lost its CSS when the stylesheet was deleted: %s", pv.Raw)
	}
	if r := e.adm(admin, "DELETE", wsBase+"/template-styles/"+styleID, nil); r.Code != 404 {
		t.Fatalf("delete twice: %d", r.Code)
	}
	// Another project does not see it, nor can it use it.
	e.provision("other")
	if n := len(e.admOK(admin, "GET", "/admin/v1/w/other/sandbox/template-styles", nil).JSON["items"].([]any)); n != 0 {
		t.Fatalf("other project's styles: %d", n)
	}

	// Sending in a new language is accepted and recorded.
	send := func(locale, id string) resp {
		return e.post(k.sbx, idem(id), map[string]any{"from": liveFrom, "to": testRcpt, "template": "welcome-fr", "template_version": 1,
			"locale": locale, "variables": map[string]any{"name": "Zawadi"}})
	}
	if r := send("pt_BR", "l1"); r.Code != 202 {
		t.Fatalf("pt_BR send: %d %s", r.Code, r.Raw)
	}
	if r := send("fr", "l2"); r.Code != 202 {
		t.Fatalf("fr send: %d %s", r.Code, r.Raw)
	}
	if r := send("english", "l3"); r.Code != 422 || errCode(r) != "invalid_locale" {
		t.Fatalf("bad locale: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM messages WHERE locale IN ('pt-br','fr')`); n != 2 {
		t.Fatalf("locales recorded: %d", n)
	}
}

func TestTemplateExportImportAndHTMLImport(t *testing.T) {
	e := setup(t)
	admin := e.user("admin@njiwa.co.tz", "admin")
	e.provision("sacas")
	e.provision("other")
	other := "/admin/v1/w/other/sandbox"

	e.admOK(admin, "POST", wsBase+"/template-styles", map[string]any{"name": "brand", "css": "p{color:#0a5}"})
	tplID := str(e.admOK(admin, "POST", wsBase+"/templates", map[string]any{"slug": "receipt", "description": "A receipt"}).JSON, "id")
	if r := e.adm(admin, "GET", wsBase+"/templates/"+tplID+"/export", nil); r.Code != 400 {
		t.Fatalf("export without a version: %d %s", r.Code, r.Raw)
	}
	e.admOK(admin, "PUT", wsBase+"/templates/"+tplID+"/draft", map[string]any{
		"variable_schema": []map[string]any{{"name": "total", "type": "string", "max_len": 20}},
		"content":         map[string]any{"en": map[string]any{"subject": "Receipt {{.total}}", "text": "Total {{.total}}", "html": "<p>Total {{.total}}</p>"}},
		"style":           "brand"})
	e.admOK(admin, "POST", wsBase+"/templates/"+tplID+"/versions/1/publish", nil)
	bundle := e.admOK(admin, "GET", wsBase+"/templates/"+tplID+"/export", nil).JSON
	if bundle["format"] != "njiwa-template/1" || bundle["slug"] != "receipt" || bundle["style"].(map[string]any)["css"] != "p{color:#0a5}" {
		t.Fatalf("bundle: %v", bundle)
	}

	// Import into another project: a draft, with the stylesheet created there.
	if r := e.adm(admin, "POST", other+"/templates/import", map[string]any{"bundle": map[string]any{"format": "x"}}); r.Code != 400 {
		t.Fatalf("wrong format: %d", r.Code)
	}
	res := e.admOK(admin, "POST", other+"/templates/import", map[string]any{"bundle": bundle})
	if res.JSON["slug"] != "receipt" || res.JSON["version"] != float64(1) {
		t.Fatalf("import: %s", res.Raw)
	}
	if r := e.adm(admin, "POST", other+"/templates/import", map[string]any{"bundle": bundle}); r.Code != 409 {
		t.Fatalf("same slug twice: %d %s", r.Code, r.Raw)
	}
	res = e.admOK(admin, "POST", other+"/templates/import", map[string]any{"bundle": bundle, "slug": "receipt-2"})
	if w, _ := res.JSON["warnings"].([]any); len(w) != 1 {
		t.Fatalf("existing style kept with a warning: %s", res.Raw)
	}
	if n := len(e.admOK(admin, "GET", other+"/template-styles", nil).JSON["items"].([]any)); n != 1 {
		t.Fatalf("styles in other: %d", n)
	}
	imp := e.admOK(admin, "GET", other+"/templates/"+str(res.JSON, "id"), nil).JSON
	v := imp["versions"].([]any)[0].(map[string]any)
	if v["published_at"] != nil || v["style"] != "brand" {
		t.Fatalf("imported version: %v", v)
	}
	// A broken bundle does not leave a half-made template behind.
	bad := map[string]any{"format": "njiwa-template/1", "slug": "broken", "variable_schema": []any{}, "content": map[string]any{"en": map[string]any{"subject": "x", "text": "{{.nope", "html": "<p>x</p>"}}}
	if r := e.adm(admin, "POST", other+"/templates/import", map[string]any{"bundle": bad}); r.Code != 400 {
		t.Fatalf("broken bundle: %d %s", r.Code, r.Raw)
	}
	if n := e.count(`SELECT count(*) FROM templates WHERE slug='broken'`); n != 0 {
		t.Fatalf("broken bundle left %d templates", n)
	}

	// HTML import.
	page := `<html><head><title>Hello {{name}}</title><style>p{color:red}</style></head><body onload="x()">
<script>alert(1)</script><p onclick="evil()">Dear {{ name }},</p><p>See <a href="https://example.com/a">your order</a> <a href="javascript:evil()">bad</a> for {{order_id}}.</p></body></html>`
	hi := e.admOK(admin, "POST", wsBase+"/templates/import-html", map[string]any{"slug": "from-html", "html": page, "locale": "FR"})
	if vars := hi.JSON["variables"].([]any); len(vars) != 2 || vars[0] != "name" || vars[1] != "order_id" {
		t.Fatalf("variables: %s", hi.Raw)
	}
	got := e.admOK(admin, "GET", wsBase+"/templates/"+str(hi.JSON, "id"), nil).JSON["versions"].([]any)[0].(map[string]any)["content"].(map[string]any)
	fr := got["fr"].(map[string]any)
	html, text := str(fr, "html"), str(fr, "text")
	if str(fr, "subject") != "Hello {{.name}}" || strings.Contains(html, "<script") || strings.Contains(html, "onclick") || strings.Contains(html, "onload") || strings.Contains(html, "javascript:") || !strings.Contains(html, "Dear {{.name}}") {
		t.Fatalf("sanitised html/subject: %v", fr)
	}
	if !strings.Contains(text, "Dear {{.name}},") || !strings.Contains(text, "your order (https://example.com/a)") || strings.Contains(text, "color:red") || strings.Contains(text, "<") {
		t.Fatalf("text: %q", text)
	}
	if got["en"] == nil {
		t.Fatal("English fallback missing")
	}
	pv := e.admOK(admin, "POST", wsBase+"/templates/"+str(hi.JSON, "id")+"/versions/1/preview", map[string]any{"locale": "fr", "variables": map[string]any{"name": "Neema", "order_id": "A1"}})
	if !strings.Contains(str(pv.JSON, "html"), "Dear Neema,") || str(pv.JSON, "subject") != "Hello Neema" {
		t.Fatalf("import preview: %s", pv.Raw)
	}
	for name, body := range map[string]map[string]any{
		"no html":    {"slug": "a1", "html": ""},
		"no subject": {"slug": "a2", "html": "<p>hi</p>"},
		"bad action": {"slug": "a3", "subject": "s", "html": "<p>{{ range .x }}</p>"},
		"no text":    {"slug": "a4", "subject": "s", "html": "<img src='x'>"},
		"bad locale": {"slug": "a5", "subject": "s", "html": "<p>hi</p>", "locale": "english"},
		"bad slug":   {"slug": "Bad Slug", "subject": "s", "html": "<p>hi</p>"},
		"exists":     {"slug": "from-html", "subject": "s", "html": "<p>hi</p>"},
	} {
		want := 400
		if name == "exists" {
			want = 409
		}
		if r := e.adm(admin, "POST", wsBase+"/templates/import-html", body); r.Code != want {
			t.Fatalf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	// Shared templates can be exported but not imported into by anyone else's project.
	if r := e.adm(admin, "GET", other+"/templates/"+tplID+"/export", nil); r.Code != 404 {
		t.Fatalf("another project's template: %d", r.Code)
	}
}
