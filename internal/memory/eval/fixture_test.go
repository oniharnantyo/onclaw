package eval

import (
	"strings"
	"testing"
)

func TestFixtureDataValidates(t *testing.T) {
	if err := Fixture.ValidateFixture(); err != nil {
		t.Fatalf("Fixture.ValidateFixture() = %v, want nil", err)
	}
}

func TestFixtureCoversAllQuestionTypes(t *testing.T) {
	// Hardened fixture scale (harden-memory-eval-multihop design D1/D2/D3):
	// multihop grows to 10 (associative-shaped included) and recall to 12 so
	// both categories report statistically meaningful scores.
	want := map[QuestionType]int{
		TypeRecall:     12,
		TypeUpdate:     1,
		TypeTemporal:   1,
		TypeMultihop:   10,
		TypeAbstention: 1,
		TypeScope:      1,
	}
	got := map[QuestionType]int{}
	for _, q := range Fixture.Questions {
		got[q.Type]++
	}
	for typ, min := range want {
		if got[typ] < min {
			t.Errorf("fixture has %d %s questions, want >= %d", got[typ], typ, min)
		}
	}
	if len(Fixture.Questions) != 28 {
		t.Errorf("fixture has %d questions, want 28 (12 recall + 10 multihop + 1 update + 2 temporal + 2 abstention + 1 scope)", len(Fixture.Questions))
	}
	if len(Fixture.Sessions) != 30 {
		t.Errorf("fixture has %d sessions, want 30", len(Fixture.Sessions))
	}
	turns := 0
	for _, s := range Fixture.Sessions {
		turns += len(s.Turns)
	}
	if turns != 75 {
		t.Errorf("fixture has %d scripted turns, want 75", turns)
	}
	if len(Fixture.Actors) != 2 {
		t.Errorf("fixture has %d actors, want 2 (Budi + Sari)", len(Fixture.Actors))
	}
}

// TestFixtureRecurringEntitiesAndDecoys pins the corpus-expansion contract
// (tasks 1.2/1.3): the three recurring entities (project, vendor, person)
// must each appear in 3+ separate sessions so associative queries have real
// cross-session distance, and every decoy near-miss must be grounded in the
// corpus while differing from the scored value it shadows.
func TestFixtureRecurringEntitiesAndDecoys(t *testing.T) {
	sessionHits := func(token string) int {
		n := 0
		for _, s := range Fixture.Sessions {
			text := ""
			for _, turn := range s.Turns {
				text += " " + normalizeText(turn)
			}
			if strings.Contains(text, normalizeText(token)) {
				n++
			}
		}
		return n
	}
	for _, entity := range []string{FactProjectName, FactVendorName, FactPersonName} {
		if n := sessionHits(entity); n < 3 {
			t.Errorf("recurring entity %q appears in %d sessions, want >= 3", entity, n)
		}
	}

	corpus := ""
	for _, s := range Fixture.Sessions {
		for _, turn := range s.Turns {
			corpus += " " + normalizeText(turn)
		}
	}
	decoys := []struct {
		name  string
		token string
		twin  string
	}{
		{"beta date", DecoyProjectBetaDate, FactProjectBetaDate},
		{"launch date", DecoyProjectLaunchDate, FactProjectLaunchDateStale},
		{"invoice amount", DecoyVendorAmount, FactVendorAmount},
		{"seats", DecoyVendorSeats, FactVendorSeats},
		{"airport", DecoyPort, FactRafiAirport},
	}
	for _, d := range decoys {
		if !strings.Contains(corpus, normalizeText(d.token)) {
			t.Errorf("decoy %s %q is not grounded in any scripted turn", d.name, d.token)
		}
		if d.token == d.twin {
			t.Errorf("decoy %s %q equals its scored twin", d.name, d.token)
		}
	}
}

func TestFixtureHasExactlyOnePrivateSession(t *testing.T) {
	private := 0
	for _, s := range Fixture.Sessions {
		if s.Kind == "private" {
			private++
			if s.Actor != "sari@eval.local" {
				t.Errorf("private session %s belongs to %s, want sari", s.ID, s.Actor)
			}
		}
	}
	if private != 1 {
		t.Errorf("fixture has %d private sessions, want exactly 1 (Sari's)", private)
	}
}

func TestFixtureValidationRejectsBrokenCatalogs(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*FixtureData)
		wantErr string
	}{
		{
			name: "expected fact missing from hinted session",
			mutate: func(f *FixtureData) {
				f.Questions[0].ExpectedFact = "Aurora 9"
				f.Questions[0].FactAliases = nil
			},
			wantErr: "not found in session",
		},
		{
			name: "abstention marker recorded in corpus",
			mutate: func(f *FixtureData) {
				for i := range f.Questions {
					if f.Questions[i].Type == TypeAbstention {
						f.Questions[i].Forbidden = []string{"stripe"}
					}
				}
			},
			wantErr: "must not appear in any scripted turn",
		},
		{
			name: "scope fact grounded in a non-private session",
			mutate: func(f *FixtureData) {
				for i := range f.Sessions {
					if f.Sessions[i].Kind == "private" {
						f.Sessions[i].Kind = "fact"
					}
				}
			},
			wantErr: "non-private session",
		},
		{
			name: "update without a stale predecessor",
			mutate: func(f *FixtureData) {
				for i := range f.Questions {
					if f.Questions[i].Type == TypeUpdate {
						f.Questions[i].StaleAliases = nil
						f.Questions[i].StaleFact = "PayPal"
					}
				}
			},
			wantErr: "recorded nowhere",
		},
		{
			name: "unknown asking actor",
			mutate: func(f *FixtureData) {
				f.Questions[0].AskAs = "nobody@eval.local"
			},
			wantErr: "unknown actor",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone := cloneFixture()
			tc.mutate(&clone)
			err := clone.ValidateFixture()
			if err == nil {
				t.Fatalf("ValidateFixture() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateFixture() = %q, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// cloneFixture deep-copies the fixture so mutations cannot leak between
// subtests (slice fields are re-sliced into fresh backing arrays where the
// mutators touch them).
func cloneFixture() FixtureData {
	f := FixtureData{
		Actors:    append([]Actor(nil), Fixture.Actors...),
		Sessions:  make([]SessionScript, len(Fixture.Sessions)),
		Questions: make([]Question, len(Fixture.Questions)),
	}
	for i, s := range Fixture.Sessions {
		s.Turns = append([]string(nil), s.Turns...)
		f.Sessions[i] = s
	}
	for i, q := range Fixture.Questions {
		q.FactAliases = append([]string(nil), q.FactAliases...)
		q.StaleAliases = append([]string(nil), q.StaleAliases...)
		q.Forbidden = append([]string(nil), q.Forbidden...)
		also := make([][]string, len(q.AlsoGroups))
		for j, g := range q.AlsoGroups {
			also[j] = append([]string(nil), g...)
		}
		q.AlsoGroups = also
		f.Questions[i] = q
	}
	return f
}
