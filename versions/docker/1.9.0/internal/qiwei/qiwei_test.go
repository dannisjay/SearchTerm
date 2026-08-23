package qiwei

import "testing"

func TestParseDetailMagnetSize(t *testing.T) {
	html := `<html><h1>进击的巨人真人版：后篇·世界终结</h1>
<ul class="gdt content">
<li class="down-list2">
<a href="magnet:?xt=urn:btih:6d12321b96c81f47689b0444c437967c51f34f13&dn=Attack.on.Titan.Part.2.2015.LIMITED.720p.BluRay.x264-USURY[rarbg]" title="Attack.on.Titan.Part.2.2015.LIMITED.720p.BluRay.x264-USURY[rarbg][4.37G]" class="folder">Attack.on.Titan.Part.2.2015.LIMITED.720p.BluRay.x264-USURY[rarbg][4.37G]</a>
</li>
</ul></html>`
	results := parseDetail(suggestItem{ID: 1, Name: "test"}, []byte(html), "https://www.qwmp4.com")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Size != "4.37GB" {
		t.Fatalf("want size 4.37GB, got %q", results[0].Size)
	}
	if results[0].UpdatedAt != "" {
		t.Fatalf("want empty updated_at, got %q", results[0].UpdatedAt)
	}
}
