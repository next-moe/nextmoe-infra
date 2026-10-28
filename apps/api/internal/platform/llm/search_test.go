package llm

import "testing"

func TestParseResponsesKeepsTextAndDedupesCitations(t *testing.T) {
	body := []byte(`{"output":[
		{"type":"reasoning"},
		{"type":"web_search_call","action":{"type":"search","query":"galgame 2026"}},
		{"type":"message","content":[{"type":"output_text","text":"今天是 2026年10月28日。[[1]](https://a.example/x)",
			"annotations":[
				{"type":"url_citation","url":"https://a.example/x","title":"1"},
				{"type":"url_citation","url":"https://www.b.example/y","title":"B 站新闻"},
				{"type":"url_citation","url":"https://a.example/x","title":"1"}
			]}]}
	],"usage":{"input_tokens":120,"output_tokens":30}}`)
	got := parseResponses(body)
	if got.Text != "今天是 2026年10月28日。[[1]](https://a.example/x)" {
		t.Fatalf("text = %q", got.Text)
	}
	want := []Citation{{URL: "https://a.example/x", Title: "a.example"}, {URL: "https://www.b.example/y", Title: "B 站新闻"}}
	if len(got.Citations) != len(want) || got.Citations[0] != want[0] || got.Citations[1] != want[1] {
		t.Fatalf("citations = %+v", got.Citations)
	}
}
