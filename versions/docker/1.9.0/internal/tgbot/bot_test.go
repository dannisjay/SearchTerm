package tgbot

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestExtractDownloadLinksMixedText(t *testing.T) {
	text := "混在一起的文本\nmagnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=A\n" +
		"ed2k://|file|B.avi|123|hash1|/ 中间还有字 " +
		"magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&dn=B，结尾" +
		"ed2k://|file|C.avi|456|hash2|/。"
	want := []string{
		"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=A",
		"ed2k://|file|B.avi|123|hash1|/",
		"magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&dn=B",
		"ed2k://|file|C.avi|456|hash2|/",
	}
	got := ExtractDownloadLinks(text)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestExtractDownloadLinksManyMixed(t *testing.T) {
	var sb strings.Builder
	var want []string
	for i := 0; i < 12; i++ {
		magnet := fmt.Sprintf("magnet:?xt=urn:btih:%040d&dn=item%d", i, i)
		sb.WriteString(fmt.Sprintf("第%d个 %s 中间文字", i, magnet))
		want = append(want, magnet)
		if i%3 == 0 {
			ed2k := fmt.Sprintf("ed2k://|file|%d.avi|%d|hash%d|/", i, i, i)
			sb.WriteString(ed2k)
			want = append(want, ed2k)
		}
	}
	got := ExtractDownloadLinks(sb.String())
	if len(got) != len(want) {
		t.Fatalf("got %d links want %d: %#v", len(got), len(want), got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestExtractDownloadLinksDedupe(t *testing.T) {
	link := "magnet:?xt=urn:btih:cccccccccccccccccccccccccccccccccccccccc"
	got := ExtractDownloadLinks(link + " 重复 " + link)
	if len(got) != 1 || got[0] != link {
		t.Fatalf("dedupe failed: %#v", got)
	}
}
