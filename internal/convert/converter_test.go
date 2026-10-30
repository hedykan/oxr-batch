package convert

import (
	"encoding/json"
	"testing"

	"oxr-batch/internal/oxr"
)

func TestMergeJSONRenumbersPages(t *testing.T) {
	results := []chunkResult{
		{chunk: chunk{Start: 1, End: 2}, result: oxr.ParseResult{TaskID: "a", JSON: json.RawMessage(`{"pages":[{"page_num":0},{"page_num":1}]}`)}},
		{chunk: chunk{Start: 3, End: 3}, result: oxr.ParseResult{TaskID: "b", JSON: json.RawMessage(`{"pages":[{"page_num":0}]}`)}},
	}
	data, err := mergeJSON("book.pdf", 3, results)
	if err != nil {
		t.Fatal(err)
	}
	var merged struct {
		Pages []struct {
			PageNum    int `json:"page_num"`
			SourcePage int `json:"source_page"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatal(err)
	}
	if len(merged.Pages) != 3 || merged.Pages[2].PageNum != 2 || merged.Pages[2].SourcePage != 3 {
		t.Fatalf("unexpected pages: %#v", merged.Pages)
	}
}

func TestMergeMarkdownKeepsOrder(t *testing.T) {
	results := []chunkResult{
		{chunk: chunk{Start: 1, End: 2}, result: oxr.ParseResult{Markdown: "first"}},
		{chunk: chunk{Start: 3, End: 4}, result: oxr.ParseResult{Markdown: "second"}},
	}
	got := mergeMarkdown(results)
	if len(got) == 0 || got[:4] != "<!--" || !(stringIndex(got, "first") < stringIndex(got, "second")) {
		t.Fatalf("unexpected markdown: %q", got)
	}
}

func stringIndex(value, needle string) int {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
