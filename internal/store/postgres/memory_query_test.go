package postgres

import "testing"

func TestShapeMemoryLexicalQuery(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantTSQuery   string
		wantTermCount int
	}{
		{
			name:          "empty query yields zero terms",
			query:         "",
			wantTSQuery:   "",
			wantTermCount: 0,
		},
		{
			name:          "pure punctuation yields zero terms",
			query:         "!!! ???",
			wantTSQuery:   "",
			wantTermCount: 0,
		},
		{
			name:          "plain multi-word query joins with OR",
			query:         "payment provider billing",
			wantTSQuery:   "payment | provider | billing",
			wantTermCount: 3,
		},
		{
			name:          "mixed case is lowercased",
			query:         "Payment PROVIDER Billing",
			wantTSQuery:   "payment | provider | billing",
			wantTermCount: 3,
		},
		{
			name:          "punctuation splits and tsquery operator characters are stripped",
			query:         "(payment & provider) | !billing:*",
			wantTSQuery:   "payment | provider | billing",
			wantTermCount: 3,
		},
		{
			name:          "underscore-joined words split into plain terms",
			query:         "agent_memory notes",
			wantTSQuery:   "agent | memory | notes",
			wantTermCount: 3,
		},
		{
			name:          "terms are capped",
			query:         "t01 t02 t03 t04 t05 t06 t07 t08 t09 t10 t11 t12 t13 t14 t15 t16 t17 t18",
			wantTSQuery:   "t01 | t02 | t03 | t04 | t05 | t06 | t07 | t08 | t09 | t10 | t11 | t12 | t13 | t14 | t15 | t16",
			wantTermCount: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shapeMemoryLexicalQuery(tt.query)
			if got.tsquery != tt.wantTSQuery {
				t.Fatalf("tsquery = %q, want %q", got.tsquery, tt.wantTSQuery)
			}
			if got.termCount != tt.wantTermCount {
				t.Fatalf("termCount = %d, want %d", got.termCount, tt.wantTermCount)
			}
		})
	}
}
