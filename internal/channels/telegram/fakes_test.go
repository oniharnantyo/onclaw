package telegram

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// ---- fake Bot API ----

// botCall is one recorded Bot API call.
type botCall struct {
	Method    string
	ChatID    int64
	MessageID int64
	Text      string
	ParseMode string
	Markup    string
	Document  string
	DocLen    int
	Action    string
	Offset    int64
	Timeout   int
	At        time.Time
}

// fakeBot is the scripted Bot API: getUpdates pops batches from feed (with a
// short stand-in for the long poll), other methods record calls and can
// inject per-method or sequenced errors. All fakes in this package are
// mutex-guarded: the service is goroutine-heavy by design.
type fakeBot struct {
	mu      sync.Mutex
	me      botapi.User
	feed    chan []botapi.Update
	wait    time.Duration // empty-feed long-poll stand-in
	errSeq  map[string][]error
	errFor  map[string]error
	calls   []botCall
	files   map[string][]byte      // DownloadFile by file path
	getFile map[string]botapi.File // GetFile by file_id
	msgSeq  int64
	wedge   chan struct{}
	wedged  int // one-shot wedge count: this many getUpdates calls block until ctx ends
}

func newFakeBot() *fakeBot {
	return &fakeBot{
		me:      botapi.User{ID: 900, Username: "buddies_bot", FirstName: "Buddies"},
		feed:    make(chan []botapi.Update, 128),
		wait:    15 * time.Millisecond,
		errSeq:  make(map[string][]error),
		errFor:  make(map[string]error),
		files:   make(map[string][]byte),
		getFile: make(map[string]botapi.File),
	}
}

func (f *fakeBot) takeErr(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if seq := f.errSeq[method]; len(seq) > 0 {
		err := seq[0]
		f.errSeq[method] = seq[1:]
		return err
	}
	return f.errFor[method]
}

func (f *fakeBot) record(c botCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.At = time.Now()
	f.calls = append(f.calls, c)
}

// wedgeOnce arms the one-shot wedge: the next getUpdates blocks until its
// context ends (the heartbeat restart path releases it).
func (f *fakeBot) wedgeOnce() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wedged++
}

// push enqueues one polled batch.
func (f *fakeBot) push(batch ...botapi.Update) {
	f.feed <- batch
}

func (f *fakeBot) callsOf(method string) []botCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []botCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeBot) lastOffset() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == "getUpdates" {
			return f.calls[i].Offset
		}
	}
	return 0
}

func (f *fakeBot) pollCount() int {
	return len(f.callsOf("getUpdates"))
}

func (f *fakeBot) GetUpdates(ctx context.Context, p botapi.GetUpdatesParams) ([]botapi.Update, error) {
	// The wire call is recorded even when it fails — poll-count assertions
	// reason about attempts, not successes.
	f.record(botCall{Method: "getUpdates", Offset: p.Offset, Timeout: p.TimeoutSeconds})
	if err := f.takeErr("getUpdates"); err != nil {
		return nil, err
	}
	if f.wedge != nil {
		select {
		case <-f.wedge:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	wedged := f.wedged > 0
	f.mu.Unlock()
	if wedged {
		// A one-shot wedge: this call blocks until its ctx ends (the
		// heartbeat's restart path), then clears for later calls.
		<-ctx.Done()
		f.mu.Lock()
		f.wedged--
		f.mu.Unlock()
		return nil, ctx.Err()
	}
	select {
	case batch := <-f.feed:
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(f.wait):
		return nil, nil
	}
}

func (f *fakeBot) SendMessage(_ context.Context, p botapi.SendMessageParams) (*botapi.Message, error) {
	// Recorded before error injection: assertions count attempts.
	f.record(botCall{Method: "sendMessage", ChatID: p.ChatID, Text: p.Text, ParseMode: p.ParseMode, Markup: string(p.ReplyMarkup)})
	if err := f.takeErr("sendMessage"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgSeq++
	return &botapi.Message{MessageID: f.msgSeq}, nil
}

func (f *fakeBot) EditMessageText(_ context.Context, p botapi.EditMessageTextParams) (*botapi.Message, error) {
	// Recorded before error injection: assertions count attempts.
	f.record(botCall{Method: "editMessageText", ChatID: p.ChatID, MessageID: p.MessageID, Text: p.Text, ParseMode: p.ParseMode})
	if err := f.takeErr("editMessageText"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &botapi.Message{MessageID: p.MessageID}, nil
}

func (f *fakeBot) SendChatAction(_ context.Context, chatID int64, action string) error {
	if err := f.takeErr("sendChatAction"); err != nil {
		return err
	}
	f.record(botCall{Method: "sendChatAction", ChatID: chatID, Action: action})
	return nil
}

func (f *fakeBot) SendDocument(_ context.Context, p botapi.SendDocumentParams) (*botapi.Message, error) {
	if err := f.takeErr("sendDocument"); err != nil {
		return nil, err
	}
	n, _ := p.Document.(interface{ Len() int })
	docLen := 0
	if n != nil {
		docLen = n.Len()
	}
	f.record(botCall{Method: "sendDocument", ChatID: p.ChatID, Document: p.FileName, DocLen: docLen})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgSeq++
	return &botapi.Message{MessageID: f.msgSeq}, nil
}

func (f *fakeBot) GetFile(_ context.Context, fileID string) (*botapi.File, error) {
	if err := f.takeErr("getFile"); err != nil {
		return nil, err
	}
	// Text carries the requested file id for assertions.
	f.record(botCall{Method: "getFile", Text: fileID})
	f.mu.Lock()
	defer f.mu.Unlock()
	if got, ok := f.getFile[fileID]; ok {
		return &got, nil
	}
	return &botapi.File{FileID: fileID, FilePath: "downloads/" + fileID}, nil
}

func (f *fakeBot) DownloadFile(_ context.Context, filePath string) ([]byte, error) {
	if err := f.takeErr("DownloadFile"); err != nil {
		return nil, err
	}
	f.record(botCall{Method: "downloadFile"})
	f.mu.Lock()
	defer f.mu.Unlock()
	if data, ok := f.files[filePath]; ok {
		return data, nil
	}
	return []byte("file-bytes"), nil
}

func (f *fakeBot) AnswerCallbackQuery(_ context.Context, callbackQueryID string) error {
	if err := f.takeErr("answerCallbackQuery"); err != nil {
		return err
	}
	f.record(botCall{Method: "answerCallbackQuery", Text: callbackQueryID})
	return nil
}

func (f *fakeBot) GetMe(_ context.Context) (*botapi.User, error) {
	if err := f.takeErr("getMe"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	me := f.me
	return &me, nil
}

func (f *fakeBot) GetWebhookInfo(_ context.Context) (*botapi.WebhookInfo, error) {
	if err := f.takeErr("getWebhookInfo"); err != nil {
		return nil, err
	}
	f.record(botCall{Method: "getWebhookInfo"})
	return &botapi.WebhookInfo{}, nil
}

func (f *fakeBot) SetMyCommands(_ context.Context, commands []botapi.BotCommand) error {
	if err := f.takeErr("setMyCommands"); err != nil {
		return err
	}
	f.record(botCall{Method: "setMyCommands"})
	return nil
}

// ---- fake runtime ----

// fakeSub is a live session subscription.
type fakeSub struct {
	ch     chan sessions.Event
	mu     sync.Mutex
	closed bool
}

func (s *fakeSub) Events() <-chan sessions.Event { return s.ch }

func (s *fakeSub) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (s *fakeSub) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// fakeTurn is a submitted turn handle.
type fakeTurn struct {
	id   string
	done chan struct{}
	once sync.Once
}

func (t *fakeTurn) TurnID() string        { return t.id }
func (t *fakeTurn) Done() <-chan struct{} { return t.done }
func (t *fakeTurn) finish()               { t.once.Do(func() { close(t.done) }) }

// approvalCall is one recorded ResolveApproval.
type approvalCall struct {
	SessionID   string
	InterruptID string
	Approved    bool
}

// fakeRuntime is the fake agent runtime: turns end only when a test ends
// them, events flow only where a test emits them. Created sessions mirror
// into the store through the real sessions service, so titles and session
// rows behave like production.
type fakeRuntime struct {
	sess             sessions.Service
	mu               sync.Mutex
	sessSeq          int
	turnSeq          int
	sessions         []sessions.Session
	submits          []agents.SendRequest
	busy             map[string]bool
	turnOf           map[string]*fakeTurn
	subs             map[string][]*fakeSub
	cancels          []string
	approvals        []approvalCall
	pendingApprovals map[string]bool // key "session:interrupt"
	submitErr        map[string]error
	createErr        error
	turnEnded        chan string // notified per ended turn (buffered)
}

func newFakeRuntime(sess sessions.Service) *fakeRuntime {
	return &fakeRuntime{
		sess:             sess,
		busy:             make(map[string]bool),
		turnOf:           make(map[string]*fakeTurn),
		subs:             make(map[string][]*fakeSub),
		pendingApprovals: make(map[string]bool),
		submitErr:        make(map[string]error),
		turnEnded:        make(chan string, 64),
	}
}

func (f *fakeRuntime) CreateSession(ctx context.Context, agentID string) (sessions.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return sessions.Session{}, f.createErr
	}
	f.sessSeq++
	sess, err := f.sess.CreateSession(ctx, agentID, "")
	if err != nil {
		return sessions.Session{}, err
	}
	f.sessions = append(f.sessions, sess)
	return sess, nil
}

func (f *fakeRuntime) Submit(_ context.Context, req agents.SendRequest) (TurnView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Rejected submits are not recorded: submittedReqs() is the successful
	// dispatch log, matching what production Submit accepts.
	if err := f.submitErr[req.SessionID]; err != nil {
		return nil, err
	}
	if f.busy[req.SessionID] {
		return nil, agents.ErrTurnInProgress
	}
	f.submits = append(f.submits, req)
	f.turnSeq++
	t := &fakeTurn{id: fmt.Sprintf("turn-%d", f.turnSeq), done: make(chan struct{})}
	f.busy[req.SessionID] = true
	f.turnOf[req.SessionID] = t
	return t, nil
}

func (f *fakeRuntime) Subscribe(_ context.Context, sessionID string) (agents.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub := &fakeSub{ch: make(chan sessions.Event, 128)}
	f.subs[sessionID] = append(f.subs[sessionID], sub)
	return sub, nil
}

func (f *fakeRuntime) Busy(_ context.Context, sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy[sessionID]
}

func (f *fakeRuntime) Cancel(_ context.Context, sessionID string) error {
	f.mu.Lock()
	turn := f.turnOf[sessionID]
	f.cancels = append(f.cancels, sessionID)
	f.mu.Unlock()

	if turn != nil {
		f.emit(sessionID, sessions.Event{TurnID: turn.TurnID(), Kind: sessions.KindTurnCancelled})
	}
	f.endTurn(sessionID)
	return nil
}

func (f *fakeRuntime) ResolveApproval(_ context.Context, sessionID, interruptID string, approved bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvals = append(f.approvals, approvalCall{sessionID, interruptID, approved})
	key := sessionID + ":" + interruptID
	if f.pendingApprovals[key] {
		delete(f.pendingApprovals, key)
		return nil
	}
	return agents.ErrApprovalNotPending
}

// emit fans one event to the session's subscriptions (drop on a full buffer,
// like the runtime).
func (f *fakeRuntime) emit(sessionID string, ev sessions.Event) {
	f.mu.Lock()
	subs := append([]*fakeSub(nil), f.subs[sessionID]...)
	f.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

// endTurn ends the session's current turn.
func (f *fakeRuntime) endTurn(sessionID string) {
	f.mu.Lock()
	turn := f.turnOf[sessionID]
	delete(f.turnOf, sessionID)
	f.busy[sessionID] = false
	f.mu.Unlock()
	if turn != nil {
		turn.finish()
		select {
		case f.turnEnded <- sessionID:
		default:
		}
	}
}

// sessionCount reports how many sessions the runtime created.
func (f *fakeRuntime) sessionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

func (f *fakeRuntime) firstSession() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sessions) == 0 {
		return ""
	}
	return f.sessions[0].ID
}

func (f *fakeRuntime) currentTurnID(sessionID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.turnOf[sessionID]; t != nil {
		return t.id
	}
	return ""
}

func (f *fakeRuntime) submittedReqs() []agents.SendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agents.SendRequest(nil), f.submits...)
}

func (f *fakeRuntime) subscribed(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs[sessionID]) > 0
}

func (f *fakeRuntime) openSub(sessionID string) *fakeSub {
	f.mu.Lock()
	defer f.mu.Unlock()
	if subs := f.subs[sessionID]; len(subs) > 0 {
		return subs[len(subs)-1]
	}
	return nil
}

// event builders stamp the session's current channel turn.
func (f *fakeRuntime) delta(sessionID, text string) sessions.Event {
	return sessions.Event{TurnID: f.currentTurnID(sessionID), Kind: sessions.KindTextDelta, Text: text}
}

func (f *fakeRuntime) assistant(sessionID, text string) sessions.Event {
	return sessions.Event{TurnID: f.currentTurnID(sessionID), Kind: sessions.KindAssistantMessage, Text: text}
}

func (f *fakeRuntime) completed(sessionID string, arts ...sessions.Artifact) sessions.Event {
	return sessions.Event{TurnID: f.currentTurnID(sessionID), Kind: sessions.KindTurnCompleted, Artifacts: arts}
}

func (f *fakeRuntime) turnError(sessionID, msg string) sessions.Event {
	return sessions.Event{TurnID: f.currentTurnID(sessionID), Kind: sessions.KindTurnError, Error: msg}
}

func (f *fakeRuntime) cancelled(sessionID string) sessions.Event {
	return sessions.Event{TurnID: f.currentTurnID(sessionID), Kind: sessions.KindTurnCancelled}
}

func (f *fakeRuntime) approvalRequired(sessionID, interruptID, command string) sessions.Event {
	return sessions.Event{
		TurnID:   f.currentTurnID(sessionID),
		Kind:     sessions.KindApprovalRequired,
		Approval: &sessions.Approval{InterruptID: interruptID, Command: command},
	}
}

// ---- fake gateway ----

type fakeGateway struct {
	mu     sync.Mutex
	state  gateway.State
	subs   map[int]gateway.StateListener
	nextID int
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{state: gateway.StateStopped, subs: make(map[int]gateway.StateListener)}
}

func (g *fakeGateway) Start(_ context.Context) error { g.transition(gateway.StateRunning); return nil }
func (g *fakeGateway) Stop() error                   { g.transition(gateway.StateStopped); return nil }
func (g *fakeGateway) Pause() error                  { g.transition(gateway.StatePaused); return nil }
func (g *fakeGateway) Resume() error                 { g.transition(gateway.StateRunning); return nil }

func (g *fakeGateway) State() gateway.State {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

func (g *fakeGateway) Subscribe(fn gateway.StateListener) (cancel func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := g.nextID
	g.nextID++
	g.subs[id] = fn
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.subs, id)
		})
	}
}

// transition commits a state change and notifies listeners outside the lock.
func (g *fakeGateway) transition(to gateway.State) {
	g.mu.Lock()
	g.state = to
	listeners := make([]gateway.StateListener, 0, len(g.subs))
	for _, fn := range g.subs {
		listeners = append(listeners, fn)
	}
	g.mu.Unlock()
	for _, fn := range listeners {
		fn(gateway.StateRunning, to)
	}
}

// ---- fake attachments and artifacts ----

type fakeSave struct {
	Name string
	Mime string
	Size int
}

type fakeAttachments struct {
	mu          sync.Mutex
	saves       []fakeSave
	keys        int
	rejectMimes map[string]bool
	err         error
}

func (f *fakeAttachments) Save(_ context.Context, name, mime string, data []byte) (sessions.Attachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, fakeSave{Name: name, Mime: mime, Size: len(data)})
	if f.err != nil {
		return sessions.Attachment{}, f.err
	}
	if f.rejectMimes[mime] {
		return sessions.Attachment{}, fmt.Errorf("%w: unsupported file type %q", errTestRejected, mime)
	}
	f.keys++
	return sessions.Attachment{Key: fmt.Sprintf("key-%d", f.keys), Name: name, Mime: mime, Size: int64(len(data))}, nil
}

// errTestRejected mirrors attachments.ErrRejected's role in rejection paths.
var errTestRejected = fmt.Errorf("attachment rejected")

type fakeArtifacts struct {
	mu    sync.Mutex
	reads []string
	files map[string][]byte
	err   error
}

func (f *fakeArtifacts) Read(_ context.Context, _, relPath string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, relPath)
	if f.err != nil {
		return nil, f.err
	}
	if data, ok := f.files[relPath]; ok {
		return data, nil
	}
	return []byte("artifact-bytes"), nil
}

// ---- fixture ----

type stateRec struct {
	Channel string
	State   string
	Detail  string
}

type fixture struct {
	st     *fake.Store
	bot    *fakeBot
	rt     *fakeRuntime
	atts   *fakeAttachments
	arts   *fakeArtifacts
	gw     *fakeGateway
	svc    *Service
	states []stateRec
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith builds the fixture then applies extra seed work.
func newFixtureWith(t *testing.T, seed func(t *testing.T, fx *fixture)) *fixture {
	t.Helper()
	st := fake.New()
	ctx := context.Background()
	require.NoError(t, st.Channels().SaveChannel(ctx, &store.Channel{ID: DefaultChannelID, Enabled: true, BotToken: "tok"}))
	require.NoError(t, st.Channels().AddChannelUser(ctx, store.ChannelUser{TelegramUserID: 42, DisplayName: "Alice"}))
	require.NoError(t, st.GatewaySettings().Save(ctx, &store.GatewaySettings{Enabled: true, DefaultAgentID: "agent-default"}))

	fx := &fixture{
		st:   st,
		bot:  newFakeBot(),
		atts: &fakeAttachments{},
		arts: &fakeArtifacts{files: map[string][]byte{}},
		gw:   newFakeGateway(),
	}
	sessSvc := sessions.NewService(st)
	fx.rt = newFakeRuntime(sessSvc)
	fx.svc = New(fx.rt, sessSvc, st.Channels(), st.GatewaySettings(), fx.atts,
		WithArtifacts(fx.arts),
		WithBotFactory(func(string, string) (BotAPI, error) { return fx.bot, nil }),
		WithStateSink(fx.recordState),
	)
	shortenTunables(fx.svc)
	if seed != nil {
		seed(t, fx)
	}
	t.Cleanup(fx.svc.Stop)
	return fx
}

func (fx *fixture) recordState(channelID, state, detail string) {
	fx.svc.mu.Lock()
	defer fx.svc.mu.Unlock()
	fx.states = append(fx.states, stateRec{channelID, state, detail})
}

// SessionAgent returns a store session's agent id (session-mapping asserts).
func (fx *fixture) SessionAgent(t *testing.T, id string) string {
	t.Helper()
	row, err := fx.st.Sessions().ByID(context.Background(), id)
	require.NoError(t, err)
	return row.AgentID
}

func (fx *fixture) recordedStates() []stateRec {
	fx.svc.mu.Lock()
	defer fx.svc.mu.Unlock()
	return append([]stateRec(nil), fx.states...)
}

// shortenTunables shrinks the pinned timings so behavior tests stay fast; the
// pinned constants stay untouched (the tunables struct documents this seam).
func shortenTunables(s *Service) {
	s.t.debounce = 20 * time.Millisecond
	s.t.editInterval = 40 * time.Millisecond
	s.t.pacingSlot = 2 * time.Millisecond
	s.t.floodInlineMax = 30 * time.Millisecond
	s.t.cooldownCap = 250 * time.Millisecond
	s.t.heartbeatEvery = 25 * time.Millisecond
	s.t.backoffMin = 5 * time.Millisecond
	s.t.backoffMax = 20 * time.Millisecond
	s.t.stallAfter = 150 * time.Millisecond
	s.t.freshFinalAfter = 150 * time.Millisecond
	s.t.typingRefresh = 40 * time.Millisecond
	s.t.queueWaitRetry = 5 * time.Millisecond
}

// eventually polls cond until it holds or the budget lapses.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---- update builders ----

func dmUpdate(id int64, userID int64, text string) botapi.Update {
	return botapi.Update{ID: id, Message: &botapi.Message{
		MessageID: id,
		From:      &botapi.User{ID: userID, FirstName: "Alice", Username: "alice"},
		Chat:      &botapi.Chat{ID: 42, Type: "private"},
		Text:      text,
	}}
}

func groupUpdate(id int64, userID int64, text string) botapi.Update {
	return botapi.Update{ID: id, Message: &botapi.Message{
		MessageID: id,
		From:      &botapi.User{ID: userID, FirstName: "Bob", Username: "bob"},
		Chat:      &botapi.Chat{ID: -100, Type: "supergroup", Title: "Ops"},
		Text:      text,
	}}
}

// seedGroup enrolls the -100 group chat.
func seedGroup(t *testing.T, fx *fixture, freeResponse bool) {
	t.Helper()
	require.NoError(t, fx.st.Channels().SaveChannelChat(context.Background(), store.ChannelChat{
		ChatID: -100, Kind: store.ChannelChatKindGroup, Title: "Ops", FreeResponse: freeResponse,
	}))
}

// dispatchOne runs one message update through the pipeline directly (no
// gateway needed) and waits for the debounce to flush.
func dispatchOne(t *testing.T, fx *fixture, u botapi.Update) {
	t.Helper()
	fx.svc.handleMessage(context.Background(), u.Message)
}
