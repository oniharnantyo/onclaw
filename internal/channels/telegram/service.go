package telegram

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Pinned behavior timings and caps (add-telegram-channel design D6–D12).
const (
	// PollTimeoutSeconds is the getUpdates server-side long-poll duration.
	PollTimeoutSeconds = 50
	// DebounceDelay merges multi-part pastes: inbound text for one chat
	// waits this long before dispatching (design D5).
	DebounceDelay = 600 * time.Millisecond
	// EditInterval is the streaming-preview edit cadence (design D6: 2s —
	// Hermes measured 0.8s earning flood penalties).
	EditInterval = 2 * time.Second
	// PacingSlot is the per-chat minimum spacing shared by sends and edits
	// (design D8).
	PacingSlot = 1 * time.Second
	// FloodInlineMax is the largest retry_after the channel sleeps inline;
	// larger penalties fail closed into a cooldown (design D8).
	FloodInlineMax = 5 * time.Second
	// CooldownCap bounds the fail-closed flood cooldown (design D8).
	CooldownCap = 300 * time.Second
	// HoldQueueCap bounds the update hold queue (design D9).
	HoldQueueCap = 64
	// ChatQueueCap bounds the per-chat FIFO turn queue (design D5); over
	// the cap the oldest queued message drops with a chat notice.
	ChatQueueCap = 64
	// PairingCap bounds the total pending pairing requests (design D4).
	PairingCap = 5
	// HeartbeatInterval is the liveness probe cadence (design D10).
	HeartbeatInterval = 90 * time.Second
	// BackoffMin is the first rebuild delay after a retryable failure.
	BackoffMin = 30 * time.Second
	// BackoffMax caps the doubling rebuild delay.
	BackoffMax = 300 * time.Second
	// ChunkLimit is Telegram's message-size cap in UTF-16 code units, the
	// unit the markdown chunker accounts.
	ChunkLimit = 4096
	// MaxArtifactsPerTurn caps the documents one turn's completion delivers
	// (design D12).
	MaxArtifactsPerTurn = 10
	// MaxArtifactBytes caps one artifact document's size; larger files are
	// skipped with a chat note (design D12).
	MaxArtifactBytes = 50 << 20
	// FreshFinalAfter is the stream duration past which the final answer is
	// sent as a fresh message instead of editing the stale preview
	// (design D6, Hermes' fresh_final_after rule).
	FreshFinalAfter = 5 * time.Minute
	// TypingRefresh re-raises the typing indicator while a turn runs; the
	// server-side bubble lasts ~5s.
	TypingRefresh = 5 * time.Second
	// MaxPhotoBytes caps the inbound photo size (design D11).
	MaxPhotoBytes = 20 << 20
	// ReceiptTTL is the processed-update retention window (design D9: the
	// Bot API redelivers unacked updates for 24h).
	ReceiptTTL = 24 * time.Hour
	// StallAfter is the quiet period without a successful getUpdates
	// round-trip the heartbeat reads as a wedged long poll (design D10).
	// Two long-poll windows plus grace.
	StallAfter = 2 * (PollTimeoutSeconds*time.Second + botapiLongPollGrace)
	// DegradedAfterRebuilds is the rebuild count past which the state
	// reads degraded instead of connected (design D10).
	DegradedAfterRebuilds = 2
	// StopJoinWait bounds the teardown join: a hung HTTP handler must not
	// block Close forever (design D1's bounded-teardown rule).
	StopJoinWait = 5 * time.Second
	// EnvAPIBase is the Bot API base URL override the headless smoke test
	// points at a fake server.
	EnvAPIBase = "BUDDIES_TELEGRAM_API_BASE"
)

// botapiLongPollGrace mirrors the client's long-poll grace so StallAfter can
// account for it without importing the unexported constant.
const botapiLongPollGrace = 15 * time.Second

// DefaultChannelID is the channel row the service serves (design D3: one
// channel today, the table shaped for more).
const DefaultChannelID = "telegram"

// tunables carries the service's timings and caps. New fills them from the
// pinned constants; tests shorten them in place (same package) so behavior
// tests stay fast without weakening the pinned values.
type tunables struct {
	pollTimeoutSecs int
	debounce        time.Duration
	editInterval    time.Duration
	pacingSlot      time.Duration
	floodInlineMax  time.Duration
	cooldownCap     time.Duration
	heartbeatEvery  time.Duration
	backoffMin      time.Duration
	backoffMax      time.Duration
	stallAfter      time.Duration
	freshFinalAfter time.Duration
	typingRefresh   time.Duration
	queueWaitRetry  time.Duration
}

func defaultTunables() tunables {
	return tunables{
		pollTimeoutSecs: PollTimeoutSeconds,
		debounce:        DebounceDelay,
		editInterval:    EditInterval,
		pacingSlot:      PacingSlot,
		floodInlineMax:  FloodInlineMax,
		cooldownCap:     CooldownCap,
		heartbeatEvery:  HeartbeatInterval,
		backoffMin:      BackoffMin,
		backoffMax:      BackoffMax,
		stallAfter:      StallAfter,
		freshFinalAfter: FreshFinalAfter,
		typingRefresh:   TypingRefresh,
		queueWaitRetry:  time.Second,
	}
}

// Service is the Telegram channel: one bot token's poll loop, authorization,
// chat-to-session mapping, and outbound delivery. Construct with New, attach
// to the gateway with Attach, and stop with Stop.
type Service struct {
	rt     Runtime
	sess   sessions.Service
	chans  store.ChannelStore
	defs   DefaultAgents
	atts   AttachmentRegistry
	arts   Artifacts
	newBot BotFactory
	sink   StateSink

	channelID string
	t         tunables

	// root scopes every channel goroutine (pumps, typing loops, queue
	// drains, pacer flushes); Stop cancels it.
	root     context.Context
	rootStop context.CancelFunc

	// mu guards the client identity, the dedupe set, and the liveness
	// bookkeeping below.
	mu          sync.Mutex
	client      BotAPI
	clientToken string
	clientProxy string
	me          *botapi.User
	seen        map[int64]time.Time
	lastPrune   time.Time
	rebuilds    int
	healthy     int
	state       string
	detail      string
	lastPoll    time.Time

	// coldBoot runs the process's first poll sync exactly once (design D9):
	// reconnects and gateway restarts within the process never re-drop.
	coldBoot sync.Once

	// hold is the bounded queue of updates admitted while holding (design
	// D9); dispatchMu serializes admission-drain races with live dispatch.
	hold       []botapi.Update
	holding    bool
	dispatchMu sync.Mutex

	// loopMu guards the gateway attachment (the ticker.go pattern).
	loopMu sync.Mutex
	att    *attachment

	chatsMu sync.Mutex
	chats   map[int64]*chatState

	paceMu sync.Mutex
	pacers map[int64]*chatPacer
}

// Option configures the Service at construction.
type Option func(*Service)

// WithChannelID points the service at a channel row other than "telegram"
// (design D3 shapes the table for more channels; one exists today).
func WithChannelID(id string) Option {
	return func(s *Service) { s.channelID = id }
}

// WithStateSink receives every connection-state transition (design D10).
// Unset — the default — transitions are only logged.
func WithStateSink(sink StateSink) Option {
	return func(s *Service) {
		if sink != nil {
			s.sink = sink
		}
	}
}

// WithBotFactory replaces the Bot API client factory. Tests fake the client
// through it; production leaves the default (honoring BUDDIES_TELEGRAM_API_BASE).
func WithBotFactory(f BotFactory) Option {
	return func(s *Service) {
		if f != nil {
			s.newBot = f
		}
	}
}

// WithArtifacts supplies the turn-artifact reader enabling sendDocument
// delivery on turn completion (design D12). Unset — the default — artifacts
// are logged, not sent.
func WithArtifacts(a Artifacts) Option {
	return func(s *Service) {
		if a != nil {
			s.arts = a
		}
	}
}

// New builds the channel service over its collaborators. The runtime, session
// service, channel store, gateway-defaults reader, and attachment registry are
// required; everything else is optional. Nothing polls until Attach observes a
// Running gateway.
func New(rt Runtime, sess sessions.Service, chans store.ChannelStore, defs DefaultAgents, atts AttachmentRegistry, opts ...Option) *Service {
	s := &Service{
		rt:        rt,
		sess:      sess,
		chans:     chans,
		defs:      defs,
		atts:      atts,
		newBot:    defaultBotFactory,
		channelID: DefaultChannelID,
		t:         defaultTunables(),
		seen:      make(map[int64]time.Time),
		chats:     make(map[int64]*chatState),
		pacers:    make(map[int64]*chatPacer),
		state:     "",
	}
	s.root, s.rootStop = context.WithCancel(context.Background())
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// defaultBotFactory is the production Bot API client factory: a *botapi.Client
// honoring the BUDDIES_TELEGRAM_API_BASE base-URL override (the headless smoke
// test points it at a fake server) and the row's optional proxy.
func defaultBotFactory(token, proxyURL string) (BotAPI, error) {
	var opts []botapi.Option
	if base := os.Getenv(EnvAPIBase); base != "" {
		opts = append(opts, botapi.WithBaseURL(base))
	}
	if proxyURL != "" {
		opts = append(opts, botapi.WithProxy(proxyURL))
	}
	return botapi.New(token, opts...), nil
}

// setState reports a connection-state transition through the sink (design
// D10), deduplicating unchanged transitions so a 90s heartbeat does not spam
// the pane. Unknown states are logged and dropped.
func (s *Service) setState(state, detail string) {
	switch state {
	case StateConnected, StateDegraded, StateError, StateDisabled:
	default:
		return
	}
	s.mu.Lock()
	changed := state != s.state || detail != s.detail
	s.state, s.detail = state, detail
	sink := s.sink
	s.mu.Unlock()

	if !changed {
		return
	}
	if sink != nil {
		sink(s.channelID, state, detail)
	}
	slog.Info("telegram: channel state", "channel", s.channelID, "state", state, "detail", detail)
}

// currentState returns the last reported state (tests assert on it).
func (s *Service) currentState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// ensureClient returns the Bot API client for the row's connection settings,
// (re)building it when the token or proxy changed since the last build
// (design D3: the row is re-read on every start, so a fix takes effect on the
// next reconnect). Callers must hold s.mu.
func (s *Service) ensureClientLocked(row *store.Channel) BotAPI {
	if s.client != nil && s.clientToken == row.BotToken && s.clientProxy == row.ProxyURL {
		return s.client
	}
	client, err := s.newBot(row.BotToken, row.ProxyURL)
	if err != nil {
		// Factories fail only on programmer error (the default never does);
		// a broken custom factory degrades every call to its error.
		slog.Error("telegram: bot factory failed", "error", err)
		return nil
	}
	s.client, s.clientToken, s.clientProxy = client, row.BotToken, row.ProxyURL
	return s.client
}

// bot returns the current client and identity, nil when the loop has never
// connected (the announcer lazily builds one instead).
func (s *Service) botFor(ctx context.Context) (BotAPI, *botapi.User, error) {
	s.mu.Lock()
	client, me := s.client, s.me
	s.mu.Unlock()
	if client != nil {
		return client, me, nil
	}
	row, err := s.chans.GetChannel(ctx, s.channelID)
	if err != nil {
		return nil, nil, err
	}
	if !row.Enabled {
		return nil, nil, errChannelDisabled
	}
	s.mu.Lock()
	client = s.ensureClientLocked(row)
	s.mu.Unlock()
	if client == nil {
		return nil, nil, errChannelDisabled
	}
	me, err = client.GetMe(ctx)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	s.me = me
	s.mu.Unlock()
	return client, me, nil
}

// errChannelDisabled marks operations the channel must not perform while its
// row is disabled.
var errChannelDisabled = errDisabled{}

type errDisabled struct{}

func (errDisabled) Error() string { return "telegram: channel disabled" }
