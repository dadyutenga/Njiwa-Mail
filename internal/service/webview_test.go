package service

import (
	"strings"
	"testing"
)

func TestWithWebViewLink(t *testing.T) {
	text, html := WithWebViewLink("https://mail.example.com/v1/view/x.y", "Hello", `<html><body class="a"><h1>Hi</h1></body></html>`)
	if !strings.HasPrefix(text, "View this email in your browser: https://mail.example.com/v1/view/x.y\n\nHello") {
		t.Fatal(text)
	}
	if !strings.HasPrefix(html, `<html><body class="a"><p `) || !strings.Contains(html, `href="https://mail.example.com/v1/view/x.y"`) {
		t.Fatal(html)
	}
	// A fragment without <body> gets the link first; an empty part stays empty.
	text, html = WithWebViewLink("https://h/v1/view/a\"b", "", "<p>x</p>")
	if text != "" || !strings.HasPrefix(html, "<p ") || !strings.Contains(html, `a&#34;b`) {
		t.Fatalf("%q %q", text, html)
	}
}
