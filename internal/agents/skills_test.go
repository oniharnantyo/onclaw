package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The skills wiring tests (tasks.md 3.1–3.3): the two-tier backend resolve,
// the eligibility gate, the metadata-only turn context, the on-demand body
// load, and the $name invocation preprocessing — driven end to end through
// the Manager with the fake model.

// skillEnv is a skills test environment: a global skills library directory
// and the workspaces root, both under one temp dir.
type skillEnv struct {
	root      string
	globalDir string
	wsRoot    string
}

func newSkillEnv(t *testing.T) *skillEnv {
	t.Helper()
	root := t.TempDir()
	env := &skillEnv{
		root:      root,
		globalDir: filepath.Join(root, "skills"),
		wsRoot:    filepath.Join(root, "workspaces"),
	}
	require.NoError(t, os.MkdirAll(env.globalDir, 0700))
	return env
}

// seedSkill writes one spec-shaped skill into dir.
func seedSkill(t *testing.T, dir, name, description, bodyLine string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0700))
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\n" + bodyLine + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0600))
}

// slugAgent gives the fixture agent a slug so it owns a workspace.
func (f *fixture) slugAgent(t *testing.T, slug string, skills ...string) store.Agent {
	t.Helper()
	agent := f.agent
	agent.Slug = slug
	agent.Skills = skills
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return agent
}

func TestStoreSkillEligibility(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} })
	ctx := t.Context()
	require.NoError(t, f.store.Skills().Upsert(ctx, &store.Skill{Name: "on-skill", Source: "directory", Enabled: true}))
	require.NoError(t, f.store.Skills().Upsert(ctx, &store.Skill{Name: "off-skill", Source: "directory", Enabled: true}))
	require.NoError(t, f.store.Skills().SetEnabled(ctx, "off-skill", false))

	disabled, err := storeSkillEligibility{registry: f.store.Skills()}.DisabledSkillNames(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"off-skill"}, disabled, "only the disabled row is reported; absent and enabled names are eligible")
}

func TestResolveSkills(t *testing.T) {
	ctx := t.Context()

	t.Run("capability off or slugless agent is a null object", func(t *testing.T) {
		env := newSkillEnv(t)
		f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} })

		// No WithSkills: off.
		b, err := f.mgr.resolveSkills(ctx, f.agent)
		require.NoError(t, err)
		require.Nil(t, b)

		// Capability on but the agent has no slug: nowhere to root the tier.
		f.mgr.skillsDir = env.globalDir
		b, err = f.mgr.resolveSkills(ctx, f.agent)
		require.NoError(t, err)
		require.Nil(t, b)
	})

	t.Run("happy path roots the agent tier and narrows by allowlist", func(t *testing.T) {
		env := newSkillEnv(t)
		f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{text: "ok"} },
			WithWorkspaces(workspaces.NewManager(env.wsRoot)), WithSkills(env.globalDir))
		seedSkill(t, env.globalDir, "trip-planner", "Plan trips", "Body.")
		seedSkill(t, env.globalDir, "solo", "Solo skill", "Body.")
		f.slugAgent(t, "tester", "solo")

		b, err := f.mgr.resolveSkills(ctx, f.agent)
		require.NoError(t, err)
		require.NotNil(t, b)

		// The agent tier directory was created on demand inside the workspace.
		require.DirExists(t, filepath.Join(env.wsRoot, "tester", "skills"))

		names, err := b.AvailableSkillNames(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"solo"}, names, "the allowlist narrows the global tier")
	})
}

func TestInjectSkillInvocations(t *testing.T) {
	ctx := t.Context()

	t.Run("eligible token becomes a blocking instruction ahead of the input", func(t *testing.T) {
		env := newSkillEnv(t)
		seedSkill(t, env.globalDir, "trip-planner", "Plan trips", "Body.")
		b := backend.NewSkillBackend(env.globalDir, filepath.Join(env.wsRoot, "s", "skills"), nil, nil)

		got := injectSkillInvocations(ctx, b, "$trip-planner plan Rome")
		require.True(t, strings.HasPrefix(got, `The user explicitly invoked the skill "trip-planner"`),
			"the blocking instruction must lead the model input")
		require.Contains(t, got, "\n\n$trip-planner plan Rome", "the user's words must ride after the instruction")
	})

	t.Run("unknown, disabled, and non-skill dollar tokens pass through", func(t *testing.T) {
		env := newSkillEnv(t)
		seedSkill(t, env.globalDir, "known-skill", "Known", "Body.")
		seedSkill(t, env.globalDir, "off-skill", "Off", "Body.")
		elig := staticNames{disabled: []string{"off-skill"}}
		b := backend.NewSkillBackend(env.globalDir, filepath.Join(env.wsRoot, "s", "skills"), elig, nil)

		input := "costs $100 and $HOME stays, $no-such-skill is unknown, $off-skill is disabled"
		require.Equal(t, input, injectSkillInvocations(ctx, b, input),
			"nothing eligible is mentioned: the input must pass through untouched")
	})

	t.Run("repeated tokens inject once and multiple skills each inject", func(t *testing.T) {
		env := newSkillEnv(t)
		seedSkill(t, env.globalDir, "alpha", "Alpha", "Body.")
		seedSkill(t, env.globalDir, "beta", "Beta", "Body.")
		b := backend.NewSkillBackend(env.globalDir, filepath.Join(env.wsRoot, "s", "skills"), nil, nil)

		got := injectSkillInvocations(ctx, b, "$alpha then $beta then $alpha again")
		require.Equal(t, 2, strings.Count(got, "The user explicitly invoked the skill"),
			"one instruction per distinct eligible skill")
	})

	t.Run("nil backend and empty input are no-ops", func(t *testing.T) {
		require.Equal(t, "hello", injectSkillInvocations(ctx, nil, "hello"))
		env := newSkillEnv(t)
		b := backend.NewSkillBackend(env.globalDir, filepath.Join(env.wsRoot, "s", "skills"), nil, nil)
		require.Equal(t, "", injectSkillInvocations(ctx, b, ""))
	})

	t.Run("a failing eligibility read degrades to the raw input", func(t *testing.T) {
		env := newSkillEnv(t)
		seedSkill(t, env.globalDir, "known-skill", "Known", "Body.")
		b := backend.NewSkillBackend(env.globalDir, filepath.Join(env.wsRoot, "s", "skills"), errorNames{}, nil)

		input := "$known-skill please"
		require.Equal(t, input, injectSkillInvocations(ctx, b, input),
			"a lookup failure must not block or mutate the turn")
	})
}

// staticNames is a fixed backend.SkillEligibility fake.
type staticNames struct{ disabled []string }

func (s staticNames) DisabledSkillNames(ctx context.Context) ([]string, error) {
	return s.disabled, nil
}

// errorNames always fails.
type errorNames struct{}

func (errorNames) DisabledSkillNames(ctx context.Context) ([]string, error) {
	return nil, errors.New("registry unavailable")
}

// skillsFixture wires a fixture with the skills capability over env and a
// slug-carrying agent.
func skillsFixture(t *testing.T, env *skillEnv, reply func(input []*schema.Message) fakeReply, opts ...Option) *fixture {
	t.Helper()
	opts = append([]Option{
		WithWorkspaces(workspaces.NewManager(env.wsRoot)),
		WithSkills(env.globalDir),
	}, opts...)
	f := newFixture(t, reply, opts...)
	f.slugAgent(t, "tester")
	return f
}

// TestTurn_SkillMetadataOnlyInContext pins progressive disclosure: an idle
// turn (no skill used) carries the eligible skills' names and descriptions
// in the model's context but never a body.
func TestTurn_SkillMetadataOnlyInContext(t *testing.T) {
	env := newSkillEnv(t)
	seedSkill(t, env.globalDir, "trip-planner", "Plan trips end to end.", "SECRET-BODY-MARKER step one")

	f := skillsFixture(t, env, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := f.model.calls()
	require.NotEmpty(t, calls)
	first := calls[0]
	var sawReminder, sawGuidance bool
	for _, msg := range first {
		if strings.Contains(msg.Content, "trip-planner") &&
			strings.Contains(msg.Content, "Plan trips end to end.") {
			sawReminder = true
		}
		if strings.Contains(msg.Content, "Progressive Disclosure") {
			sawGuidance = true
		}
		require.NotContains(t, msg.Content, "SECRET-BODY-MARKER",
			"an unused skill's body must stay out of the turn context")
	}
	require.True(t, sawReminder, "the metadata reminder must carry name and description")
	require.True(t, sawGuidance, "the middleware's skill guidance rides the instruction")

	// No skill tool calls: the transcript is the plain-turn shape.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))

	// The persisted user message keeps the user's raw words.
	require.Equal(t, "hello", events[0].Text)
}

// TestTurn_SkillBodyLoadsOnDemand pins the second disclosure tier: the model
// invokes the skill tool, the body loads from disk, and the follow-up model
// call reads it.
func TestTurn_SkillBodyLoadsOnDemand(t *testing.T) {
	env := newSkillEnv(t)
	seedSkill(t, env.globalDir, "trip-planner", "Plan trips end to end.", "SECRET-BODY-MARKER step one")

	calls := 0
	f := skillsFixture(t, env, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", "skill", `{"skill":"trip-planner"}`)
		}
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, "SECRET-BODY-MARKER") {
				return fakeReply{text: "skill loaded", promptTokens: 30, completionTokens: 5}
			}
		}
		return fakeReply{err: errors.New("the skill body never reached the model")}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "plan a trip")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the on-demand body load must complete the turn")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.NotEmpty(t, tools)
	require.Equal(t, "skill", tools[0].Tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tools[0].Tool.Status)
	require.Contains(t, tools[0].Tool.Result, "SECRET-BODY-MARKER", "the terminal record carries the loaded body")
}

// TestTurn_SkillDollarInvocation pins the explicit invocation: the model
// input carries the blocking instruction and the turn loads the body, while
// the persisted user message keeps the user's raw words.
func TestTurn_SkillDollarInvocation(t *testing.T) {
	env := newSkillEnv(t)
	seedSkill(t, env.globalDir, "trip-planner", "Plan trips end to end.", "SECRET-BODY-MARKER step one")

	calls := 0
	f := skillsFixture(t, env, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			// The injected instruction must reach the model ahead of the
			// user's message.
			injected := false
			for _, msg := range input {
				if strings.Contains(msg.Content, `explicitly invoked the skill "trip-planner"`) {
					injected = true
				}
			}
			if !injected {
				return fakeReply{err: errors.New("the $name instruction never reached the model")}
			}
			return toolCallsReply("call-1", "skill", `{"skill":"trip-planner"}`)
		}
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, "SECRET-BODY-MARKER") {
				return fakeReply{text: "skill loaded", promptTokens: 30, completionTokens: 5}
			}
		}
		return fakeReply{err: errors.New("the skill body never reached the model")}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "$trip-planner plan Rome")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// The transcript keeps the user's raw words: the injection is model
	// input only.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, ev := range events {
		if ev.Kind == sessions.KindUserMessage {
			require.Equal(t, "$trip-planner plan Rome", ev.Text)
		}
	}
}

// TestTurn_SkillDisabledExcluded pins the eligibility gate end to end: a
// globally disabled skill is absent from the metadata, its $name stays plain
// text, and a direct tool invocation of it fails without loading a body.
func TestTurn_SkillDisabledExcluded(t *testing.T) {
	env := newSkillEnv(t)
	seedSkill(t, env.globalDir, "off-skill", "Disabled skill.", "SECRET-BODY-MARKER")
	seedSkill(t, env.globalDir, "on-skill", "Enabled skill.", "plain body")

	calls := 0
	f := skillsFixture(t, env, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			// The disabled skill's metadata must ride nowhere: no description
			// anywhere, no reminder entry, no injected instruction — the raw
			// $token in the user's message stays plain text.
			for _, msg := range input {
				if strings.Contains(msg.Content, "Disabled skill.") ||
					strings.Contains(msg.Content, "The user explicitly invoked the skill") {
					return fakeReply{err: errors.New("the disabled skill leaked into the turn context")}
				}
				if strings.Contains(msg.Content, "available for use with the Skill tool") &&
					strings.Contains(msg.Content, "off-skill") {
					return fakeReply{err: errors.New("the disabled skill is advertised in the reminder")}
				}
			}
			return toolCallsReply("call-1", "skill", `{"skill":"off-skill"}`)
		}
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, "not available") {
				return fakeReply{text: "refused", promptTokens: 30, completionTokens: 5}
			}
		}
		return fakeReply{err: errors.New("a disabled skill must not load a body")}
	})
	sess := f.newSession(t)
	// The registry holds the startup scan's rows; the pane's disable switch
	// flips the off-skill row.
	require.NoError(t, f.store.Skills().Upsert(t.Context(), &store.Skill{Name: "off-skill", Source: "directory", Enabled: true}))
	require.NoError(t, f.store.Skills().SetEnabled(t.Context(), "off-skill", false))

	turn, err := f.mgr.Send(t.Context(), sess.ID, "$off-skill do it")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the turn must survive the refused invocation")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, ev := range events {
		if ev.Kind == sessions.KindUserMessage {
			require.Equal(t, "$off-skill do it", ev.Text, "the persisted user message keeps the raw words")
		}
	}
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 1)
	require.Equal(t, sessions.ToolStatusFailed, tools[0].Tool.Status,
		"the disabled skill's invocation must land as a failed tool call")
	require.NotContains(t, tools[0].Tool.Result, "SECRET-BODY-MARKER")
}

// TestTurn_SkillAllowlistNarrows pins the agent-side gate: an allowlisted
// agent sees only its subset.
func TestTurn_SkillAllowlistNarrows(t *testing.T) {
	env := newSkillEnv(t)
	seedSkill(t, env.globalDir, "mine", "Mine.", "body")
	seedSkill(t, env.globalDir, "theirs", "Theirs.", "SECRET-THEIRS")

	f := skillsFixture(t, env, func(input []*schema.Message) fakeReply {
		for _, msg := range input {
			if strings.Contains(msg.Content, "SECRET-THEIRS") {
				return fakeReply{err: errors.New("a non-allowlisted skill leaked into the turn context")}
			}
		}
		saw := false
		for _, msg := range input {
			if strings.Contains(msg.Content, "mine") && strings.Contains(msg.Content, "Mine.") {
				saw = true
			}
		}
		if !saw {
			return fakeReply{err: errors.New("the allowlisted skill never reached the model")}
		}
		return fakeReply{text: "done", promptTokens: 10, completionTokens: 2}
	})
	// Narrow the fixture agent's skill allowlist (skillsFixture already set
	// the slug).
	agent := f.agent
	agent.Skills = []string{"mine"}
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
}
