package whatsappmd

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// deviceClient is the narrow protocol seam between the adapter and the
// whatsmeow multi-device client (add-whatsapp-gateway design D6). It wraps
// exactly the surface the adapter uses — connect/disconnect, send/edit,
// chat presence, mark-read, media download, and the pairing entry points —
// so unit tests inject a fake and never spin the real protocol. The
// interface (and every whatsmeow import) stays inside this package; the
// adapter's exported API speaks stdlib/plain types only, keeping whatsmeow
// out of cloud-only builds that never construct this adapter.
type deviceClient interface {
	// HasSession reports whether a paired device identity exists in the
	// session store (design D5: the md lane's "token" is the device session).
	HasSession() bool
	Connect(ctx context.Context) error
	Disconnect()
	IsConnected() bool
	IsLoggedIn() bool
	// Logout unlinks the device from the account, disconnects, and deletes
	// the session rows from the store (design D6 teardown).
	Logout(ctx context.Context) error
	// DeleteSession drops the local session rows without server contact —
	// the force path when a Logout request fails.
	DeleteSession(ctx context.Context) error
	// LinkedJID returns the paired account's JID, if any.
	LinkedJID() (types.JID, bool)

	SendMessage(ctx context.Context, to types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error)
	BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message
	SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error
	MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID) error
	DownloadAny(ctx context.Context, msg *waE2E.Message) ([]byte, error)

	// GetQRChannel starts the pairing QR stream; it must be called before
	// Connect and only works while the store has no device identity.
	GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error)
	// PairPhone requests an 8-digit pairing code for the given account
	// phone number; it resolves once the socket is up (after the first QR
	// channel item).
	PairPhone(ctx context.Context, phone string) (string, error)

	SetEventHandler(handler func(evt any))
}

// realDevice is the whatsmeow-backed deviceClient.
type realDevice struct {
	cli *whatsmeow.Client
}

func (d *realDevice) HasSession() bool {
	return d.cli.Store.ID != nil
}

func (d *realDevice) Connect(ctx context.Context) error {
	return d.cli.ConnectContext(ctx)
}

func (d *realDevice) Disconnect() {
	d.cli.Disconnect()
}

func (d *realDevice) IsConnected() bool {
	return d.cli.IsConnected()
}

func (d *realDevice) IsLoggedIn() bool {
	return d.cli.IsLoggedIn()
}

func (d *realDevice) Logout(ctx context.Context) error {
	return d.cli.Logout(ctx)
}

func (d *realDevice) DeleteSession(ctx context.Context) error {
	return d.cli.Store.Delete(ctx)
}

func (d *realDevice) LinkedJID() (types.JID, bool) {
	if d.cli.Store.ID == nil {
		return types.JID{}, false
	}
	return *d.cli.Store.ID, true
}

func (d *realDevice) SendMessage(ctx context.Context, to types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
	return d.cli.SendMessage(ctx, to, message)
}

func (d *realDevice) BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message {
	return d.cli.BuildEdit(chat, id, newContent)
}

func (d *realDevice) SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	return d.cli.SendChatPresence(ctx, jid, state, media)
}

func (d *realDevice) MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID) error {
	return d.cli.MarkRead(ctx, ids, timestamp, chat, sender)
}

func (d *realDevice) DownloadAny(ctx context.Context, msg *waE2E.Message) ([]byte, error) {
	return d.cli.DownloadAny(ctx, msg)
}

func (d *realDevice) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return d.cli.GetQRChannel(ctx)
}

func (d *realDevice) PairPhone(ctx context.Context, phone string) (string, error) {
	return d.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, pairCodeClientName)
}

func (d *realDevice) SetEventHandler(handler func(evt any)) {
	d.cli.AddEventHandler(func(evt any) { handler(evt) })
}

// buildRealDevice bridges whatsmeow's sqlstore over the composition root's
// PostgreSQL pool (add-whatsapp-gateway design D6 as amended): the composition
// root opens a stdlib *sql.DB via the pgx/v5/stdlib adapter on the same
// Postgres instance and hands it to the adapter constructor; the sqlstore
// runs its own schema upgrades (whatsmeow_-namespaced tables — the main
// migration chain never manages them).
//
// Per-gateway scoping: this whatsmeow release keys devices by JID in the
// shared whatsmeow_* tables and its container has no key parameter, so the
// handed-over pool must be scoped to one gateway. The composition root owns
// that choice: each gateway gets its own device DATABASE (see the deviation
// note on mdDatabaseName in internal/server — dbutil's information_schema
// existence probes are database-wide, which a per-gateway schema cannot
// survive once any other schema holds whatsmeow_* tables). The container is
// intentionally never Closed here — closing it would close the shared pool
// the composition root owns.
func buildRealDevice(ctx context.Context, db *sql.DB, log waLog.Logger) (deviceClient, error) {
	container := sqlstore.NewWithDB(db, "postgres", log)
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("whatsappmd device store: upgrade: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsappmd device store: load device: %w", err)
	}
	return &realDevice{cli: whatsmeow.NewClient(device, log)}, nil
}

// slogLogger bridges whatsmeow's waLog into slog. Debug output is discarded
// on purpose: whatsmeow logs session keys and other credential material at
// DEBUG level, and the gateway must never log credentials.
type slogLogger struct {
	module string
}

func (l slogLogger) Errorf(msg string, args ...any) {
	slog.Error(fmt.Sprintf(msg, args...), "module", l.module)
}

func (l slogLogger) Warnf(msg string, args ...any) {
	slog.Warn(fmt.Sprintf(msg, args...), "module", l.module)
}

func (l slogLogger) Infof(msg string, args ...any) {
	slog.Info(fmt.Sprintf(msg, args...), "module", l.module)
}

func (l slogLogger) Debugf(string, ...any) {}

func (l slogLogger) Sub(module string) waLog.Logger {
	return slogLogger{module: l.module + "/" + module}
}
