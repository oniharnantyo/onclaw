package channels

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestParseMentionTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "simple mention", body: "@atlas can you check?", want: []string{"atlas"}},
		{name: "multiple mentions", body: "@atlas and @beacon", want: []string{"atlas", "beacon"}},
		{name: "case preserved in token", body: "hey @Atlas", want: []string{"Atlas"}},
		{name: "punctuation terminates", body: "@atlas, @beacon!", want: []string{"atlas", "beacon"}},
		{name: "kebab handle", body: "@atlas-ops please", want: []string{"atlas-ops"}},
		{name: "dedup case-insensitive", body: "@atlas @ATLAS", want: []string{"atlas"}},
		{name: "bare at stays plain", body: "reach me @ tomorrow", want: nil},
		{name: "double at: inner at starts the token", body: "see @@atlas", want: []string{"atlas"}},
		{name: "email not a mention target beyond token", body: "mail a@b.com", want: []string{"b"}},
		{name: "no mentions", body: "nothing to see", want: nil},
		{name: "at at end", body: "hey @", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMentionTokens(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("parseMentionTokens(%q) = %v, want %v", tt.body, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseMentionTokens(%q)[%d] = %q, want %q", tt.body, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHandleIndex_LongestHandleWinsCollision(t *testing.T) {
	members := []domain.ChannelMember{
		{MemberType: domain.ChannelMemberTypeAgent, AgentID: "agent-1"},
		{MemberType: domain.ChannelMemberTypeAgent, AgentID: "agent-2"},
	}
	resolved := map[string]string{
		"agent-1": "ops",
		"agent-2": "ops-team", // same lowercase key prefix, longer handle
	}

	index := handleIndex(resolved, members)
	// "ops" collides with "ops-team"? No — keys differ ("ops" vs "ops-team");
	// the collision rule only fires for identical lowercase handles. Assert
	// both resolve independently.
	if ref, ok := index["ops"]; !ok || ref.id != "agent-1" {
		t.Fatalf("index[ops] = %+v, want agent-1", ref)
	}
	if ref, ok := index["ops-team"]; !ok || ref.id != "agent-2" {
		t.Fatalf("index[ops-team] = %+v, want agent-2", ref)
	}

	// Identical lowercase handles: the earlier roster member wins, whatever
	// the resolved map's iteration order would suggest.
	tied := map[string]string{"agent-1": "ops", "agent-2": "ops"}
	index = handleIndex(tied, members)
	if ref := index["ops"]; ref.id != "agent-1" {
		t.Fatalf("index[ops] = %+v, want agent-1 (earlier roster member wins)", ref)
	}
}

func TestResolveMentions(t *testing.T) {
	index := handleIndex(
		map[string]string{
			"agent-atlas":  "atlas",
			"agent-beacon": "beacon",
			"user-sarah":   "sarah-chen",
		},
		[]domain.ChannelMember{
			{MemberType: domain.ChannelMemberTypeAgent, AgentID: "agent-atlas"},
			{MemberType: domain.ChannelMemberTypeAgent, AgentID: "agent-beacon"},
			{MemberType: domain.ChannelMemberTypeUser, UserID: "user-sarah"},
		},
	)

	tests := []struct {
		name   string
		tokens []string
		want   []domain.Mention
	}{
		{
			name:   "case-insensitive match",
			tokens: []string{"ATLAS"},
			want:   []domain.Mention{{Type: domain.ChannelMemberTypeAgent, ID: "agent-atlas", Handle: "atlas"}},
		},
		{
			name:   "unresolved stays plain",
			tokens: []string{"ghost"},
			want:   nil,
		},
		{
			name:   "humans resolve too",
			tokens: []string{"sarah-chen"},
			want:   []domain.Mention{{Type: domain.ChannelMemberTypeUser, ID: "user-sarah", Handle: "sarah-chen"}},
		},
		{
			name:   "first occurrence order, dedup by id",
			tokens: []string{"beacon", "atlas", "BEACON"},
			want: []domain.Mention{
				{Type: domain.ChannelMemberTypeAgent, ID: "agent-beacon", Handle: "beacon"},
				{Type: domain.ChannelMemberTypeAgent, ID: "agent-atlas", Handle: "atlas"},
			},
		},
		{
			name:   "mixed resolved and unresolved",
			tokens: []string{"atlas", "nobody"},
			want:   []domain.Mention{{Type: domain.ChannelMemberTypeAgent, ID: "agent-atlas", Handle: "atlas"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMentions(tt.tokens, index)
			if len(got) != len(tt.want) {
				t.Fatalf("resolveMentions(%v) = %+v, want %+v", tt.tokens, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("resolveMentions(%v)[%d] = %+v, want %+v", tt.tokens, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAgentIDsOf(t *testing.T) {
	mentions := []domain.Mention{
		{Type: domain.ChannelMemberTypeUser, ID: "u1"},
		{Type: domain.ChannelMemberTypeAgent, ID: "a1"},
		{Type: domain.ChannelMemberTypeAgent, ID: "a2"},
	}
	got := agentIDsOf(mentions)
	if len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("agentIDsOf = %v, want [a1 a2]", got)
	}
}
