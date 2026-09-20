package epub

import "testing"

func TestResolveTarget(t *testing.T) {
	cases := []struct {
		base, ref, href, fragment string
	}{
		{"OPS/content.opf", "text/ch1.xhtml", "OPS/text/ch1.xhtml", ""},
		{"OPS/nav/nav.xhtml", "../text/ch1.xhtml#start", "OPS/text/ch1.xhtml", "start"},
		{"OPS/nav/nav.xhtml", "./../text/./ch1.xhtml", "OPS/text/ch1.xhtml", ""},
		{"content.opf", "Text/Chapter%201.xhtml", "Text/Chapter 1.xhtml", ""},
		{"OPS/content.opf", "text/ch1.xhtml#caf%C3%A9", "OPS/text/ch1.xhtml", "café"},
		{"OPS/text/ch1.xhtml", "#note", "OPS/text/ch1.xhtml", "note"},
		{"OPS/content.opf", "text/ch1.xhtml?v=2", "OPS/text/ch1.xhtml", ""},
		{"OPS/content.opf", "  text/ch1.xhtml  ", "OPS/text/ch1.xhtml", ""},
		{"OPS/content.opf", "100%.xhtml", "OPS/100%.xhtml", ""},
		{"OPS/content.opf", "Text/MiXeD.XHTML", "OPS/Text/MiXeD.XHTML", ""},
		{"", "images/pic.png", "images/pic.png", ""},
		{"OPS/a/b/c.xhtml", "../../d.xhtml", "OPS/d.xhtml", ""},
	}
	for _, c := range cases {
		got, err := resolveTarget(c.base, c.ref)
		if err != nil {
			t.Errorf("resolveTarget(%q, %q): %v", c.base, c.ref, err)
			continue
		}
		if got.Href != c.href || got.Fragment != c.fragment {
			t.Errorf("resolveTarget(%q, %q) = %+v, want {%q %q}", c.base, c.ref, got, c.href, c.fragment)
		}
	}
}

func TestResolveTargetRejects(t *testing.T) {
	cases := []struct{ base, ref string }{
		{"OPS/content.opf", ""},
		{"OPS/content.opf", "   "},
		{"OPS/content.opf", "../../../etc/passwd"},
		{"content.opf", "../secret.txt"},
		{"OPS/content.opf", ".."},
		{"OPS/content.opf", "."},
		{"OPS/content.opf", "/etc/passwd"},
		{"OPS/content.opf", "//cdn.example.com/x.js"},
		{"OPS/content.opf", "http://example.com/x.xhtml"},
		{"OPS/content.opf", "HTTPS://example.com/x.xhtml"},
		{"OPS/content.opf", "mailto:a@example.com"},
		{"OPS/content.opf", "data:text/html,x"},
		{"OPS/content.opf", "javascript:alert(1)"},
		{"OPS/content.opf", "..%2f..%2fsecret.txt"},
		{"OPS/content.opf", "text%5Cch1.xhtml"},
		{"", "#fragment-only"},
	}
	for _, c := range cases {
		if got, err := resolveTarget(c.base, c.ref); err == nil {
			t.Errorf("resolveTarget(%q, %q) = %+v, want an error", c.base, c.ref, got)
		}
	}
}

func TestNormalizeArchivePathKeepsEntryNamesVerbatim(t *testing.T) {
	names := []string{
		"OPS/images/pic one.png",
		"OPS/images/pic%20one.png",
		"OPS/images/pic#one.png",
		"OPS/images/pic?one.png",
		"OPS/images/100%.png",
		"cover.jpg",
	}
	for _, name := range names {
		got, err := NormalizeArchivePath(name)
		if err != nil || got != name {
			t.Errorf("NormalizeArchivePath(%q) = %q, %v", name, got, err)
		}
	}

	if got, err := NormalizeArchivePath("OPS/./text/../images/pic.png"); err != nil || got != "OPS/images/pic.png" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestNormalizeArchivePathRejects(t *testing.T) {
	for _, name := range []string{"", "../../secret.jpg", "OPS/../../secret.jpg", "/etc/passwd",
		"OPS\\images\\pic.png", "OPS/images/", "OPS/images/..", "OPS/images/pic.png\x00"} {
		if got, err := NormalizeArchivePath(name); err == nil {
			t.Errorf("NormalizeArchivePath(%q) = %q, want an error", name, got)
		}
	}
}
