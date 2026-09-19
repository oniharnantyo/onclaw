// Package eval implements the wave-0 memory evaluation harness for the
// integrate-agent-zero-memory change (design D15): it seeds a fixture
// workspace through the real HTTP API, drives LongMemEval-protocol questions
// through the real chat path (/v1 OpenResponses turns against a live model),
// and scores the answers for recall, citation validity, scope safety, and
// abstention — producing the scoreboard that gates the later capability
// waves.
//
// The harness is a pure HTTP client over a running onclaw server: it holds no
// stores and never touches the database directly, so the same binary scores a
// pre-change server (kept docs only — the baseline) and a post-change server.
// Live model keys are REQUIRED: agent prompt generation, the fixture turns,
// and every scored question are real model runs.
package eval

import (
	"fmt"
	"strings"
)

// QuestionType is the LongMemEval family a question belongs to. The type
// decides which scorer arms apply (score.go).
type QuestionType string

const (
	// TypeRecall: a known fact stated in a single earlier session must be
	// recalled verbatim enough to hit its aliases.
	TypeRecall QuestionType = "recall"
	// TypeUpdate: an old fact was superseded by a newer one; the answer must
	// assert the NEW fact and not present the stale one as current.
	TypeUpdate QuestionType = "update"
	// TypeTemporal: the answer is a date or time marker recorded inside a
	// scripted turn.
	TypeTemporal QuestionType = "temporal"
	// TypeMultihop: the answer chains two facts recorded across sessions
	// (resolve entity A, then A's attribute B).
	TypeMultihop QuestionType = "multihop"
	// TypeAbstention: nothing was ever recorded; the correct answer states
	// that nothing is recorded and fabricates no specifics.
	TypeAbstention QuestionType = "abstention"
	// TypeScope: the fact exists only in the other member's private
	// (user-visibility) memory; the correct answer does not leak it.
	TypeScope QuestionType = "scope"
)

// Actor is one fixture identity the harness drives turns as. Actors are
// separate from the --email/--password credentials (the admin identity that
// creates or reuses the workspace): the harness logs the actors in with their
// fixed passwords so chat-key identity — which structurally scopes memory —
// is deterministic.
type Actor struct {
	Email    string
	Name     string
	Password string
	Role     string // workspace role name granted on membership ("Member")
}

// SessionScript is one scripted multi-turn conversation: raw user turns
// POSTed through the real chat path as the actor's DM with the fixture agent.
// The participant rule (one human → user-visibility ceiling) is what keeps
// each actor's facts private to them.
type SessionScript struct {
	ID     string   // stable logical id, e.g. "budi-payments-v1"
	Actor  string   // actor Email driving the turns
	Turns  []string // raw user turn text, in order
	Kind   string   // "fact" (grounds questions) | "noise" (distractor) | "private"
	Marker string   // semantic label used by fixture validation, e.g. "payments-v1"
}

// Question is one scored LongMemEval-protocol question.
type Question struct {
	ID string
	// Type selects the scoring arms (see QuestionType).
	Type QuestionType
	// AskAs is the actor Email whose chat key drives the question turn.
	// Recall/update/temporal/multihop questions are asked as the actor who
	// owns the facts; abstention and scope questions are asked as the actor
	// whose view must NOT contain the answer.
	AskAs string
	// Text is the raw question POSTed as a fresh-session turn.
	Text string
	// ExpectedFact is the fact (or its primary form) that a correct answer
	// contains. For update questions this is the NEW fact.
	ExpectedFact string
	// FactAliases are alternative surface forms; any-of counts as a hit for
	// ExpectedFact.
	FactAliases []string
	// AlsoGroups are additional alias groups every one of which must hit
	// (multihop chains: group i is the i-th hop's payload).
	AlsoGroups [][]string
	// StaleFact (+ StaleAliases) is the superseded fact for update questions:
	// presenting it as current fails recall.
	StaleFact    string
	StaleAliases []string
	// Forbidden are leak/fabrication detectors: scope questions fail when any
	// token (the other member's private specifics) appears; abstention
	// questions fail when any invented-specific marker appears.
	Forbidden []string
	// SessionHint names the fixture session whose turns ground the fact
	// (validation cross-checks ExpectedFact against that session's turns).
	SessionHint string
	// CitationHint describes where a citation should trace (session or note
	// provenance) — carried into the scoreboard for human review, not
	// machine-scored beyond the evidence-presence rule.
	CitationHint string
}

// FixtureData is the whole scenario set: fixture actors, scripted sessions,
// and the question catalog.
type FixtureData struct {
	Actors    []Actor
	Sessions  []SessionScript
	Questions []Question
}

// ValidateFixture cross-checks the catalog against the scripted turns so a
// broken edit to the data fails in tests, not in a live run:
//
//   - every non-abstention question's ExpectedFact appears in the hinted
//     session's turns (case/whitespace-normalized);
//   - multihop AlsoGroups resolve within the whole corpus;
//   - scope questions' Forbidden tokens appear ONLY in private sessions;
//   - abstention questions' Forbidden markers appear NOWHERE in the corpus
//     (they must be inventions, not recorded facts);
//   - update questions' stale fact appears in an earlier session than the
//     new fact;
//   - every AskAs is a known fixture actor.
func (f FixtureData) ValidateFixture() error {
	actors := map[string]bool{}
	for _, a := range f.Actors {
		actors[a.Email] = true
	}

	// sessionsByID + full-corpus normalized text.
	byID := map[string]*SessionScript{}
	var corpus strings.Builder
	for i := range f.Sessions {
		s := &f.Sessions[i]
		if _, dup := byID[s.ID]; dup {
			return fmt.Errorf("eval fixture: duplicate session id %q", s.ID)
		}
		byID[s.ID] = s
		for _, t := range s.Turns {
			corpus.WriteString(" ")
			corpus.WriteString(normalizeText(t))
		}
	}
	corpusText := corpus.String()

	// The global private leak tokens must be grounded in exactly one private
	// session and recorded in no fact/noise session — they are forbidden on
	// every scored answer, so any legitimate mention would be a false fail.
	privateGrounded := 0
	for _, s := range f.Sessions {
		sessionText := ""
		for _, t := range s.Turns {
			sessionText += " " + normalizeText(t)
		}
		hit := containsAny(sessionText, privateLeakTokens)
		if hit && s.Kind != "private" {
			return fmt.Errorf("eval fixture: private leak token grounded in non-private session %s", s.ID)
		}
		if hit {
			privateGrounded++
		}
	}
	if privateGrounded == 0 {
		return fmt.Errorf("eval fixture: private leak tokens %v grounded in no private session", privateLeakTokens)
	}

	for _, q := range f.Questions {
		if !actors[q.AskAs] {
			return fmt.Errorf("eval fixture: question %s asks as unknown actor %q", q.ID, q.AskAs)
		}

		switch q.Type {
		case TypeAbstention:
			// Forbidden markers must be absent everywhere — they are the
			// fabricated-specifics detectors.
			for _, tok := range append([]string{q.ExpectedFact}, q.Forbidden...) {
				if tok != "" && strings.Contains(corpusText, normalizeText(tok)) {
					return fmt.Errorf("eval fixture: abstention question %s: marker %q must not appear in any scripted turn", q.ID, tok)
				}
			}
		case TypeScope:
			// Forbidden tokens must be recorded in exactly one private
			// session and nowhere the asking actor could legitimately read.
			hits := 0
			for _, s := range f.Sessions {
				inSession := false
				for _, t := range s.Turns {
					nt := normalizeText(t)
					for _, tok := range q.Forbidden {
						if strings.Contains(nt, normalizeText(tok)) {
							inSession = true
						}
					}
				}
				if inSession {
					if s.Kind != "private" {
						return fmt.Errorf("eval fixture: scope question %s: token grounded in non-private session %s", q.ID, s.ID)
					}
					if s.Actor == q.AskAs {
						return fmt.Errorf("eval fixture: scope question %s: private session %s belongs to the asking actor", q.ID, s.ID)
					}
					hits++
				}
			}
			if hits == 0 {
				return fmt.Errorf("eval fixture: scope question %s: no scripted turn records the private fact", q.ID)
			}
		default:
			// recall / update / temporal / multihop: the expected fact must be
			// grounded in the hinted session.
			s, ok := byID[q.SessionHint]
			if !ok {
				return fmt.Errorf("eval fixture: question %s hints unknown session %q", q.ID, q.SessionHint)
			}
			sessionText := ""
			for _, t := range s.Turns {
				sessionText += " " + normalizeText(t)
			}
			if !aliasesHit(sessionText, q.ExpectedFact, q.FactAliases) {
				return fmt.Errorf("eval fixture: question %s: expected fact %q not found in session %s turns", q.ID, q.ExpectedFact, q.SessionHint)
			}
			for _, group := range q.AlsoGroups {
				if !anyHit(sessionText, group) && !anyHit(corpusText, group) {
					return fmt.Errorf("eval fixture: question %s: also-group %v grounded nowhere", q.ID, group)
				}
			}
			if q.Type == TypeUpdate {
				// The stale fact must exist and predate the update.
				staleSession := ""
				for _, s2 := range f.Sessions {
					t := ""
					for _, turn := range s2.Turns {
						t += " " + normalizeText(turn)
					}
					if aliasesHit(t, q.StaleFact, q.StaleAliases) {
						staleSession = s2.ID
						break
					}
				}
				if staleSession == "" {
					return fmt.Errorf("eval fixture: update question %s: stale fact %q recorded nowhere", q.ID, q.StaleFact)
				}
				if staleSession == q.SessionHint {
					return fmt.Errorf("eval fixture: update question %s: stale and new fact share session %s (no update)", q.ID, q.SessionHint)
				}
			}
		}
	}
	return nil
}
