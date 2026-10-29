package botapi

import (
	"encoding/json"
	"io"
)

// User is a Telegram user — only the fields the channel consumes.
type User struct {
	// ID is the numeric Telegram user id (the pairing allowlist key).
	ID int64 `json:"id"`
	// FirstName is the display name shown on pairing cards.
	FirstName string `json:"first_name,omitempty"`
	// Username is the @handle (empty when the user has none).
	Username string `json:"username,omitempty"`
}

// Chat is a Telegram chat — a private (DM) or group conversation.
type Chat struct {
	ID int64 `json:"id"`
	// Type is "private" | "group" | "supergroup" (the channel keys its
	// DM-vs-group rules off it).
	Type string `json:"type,omitempty"`
	// Title is the group title (empty for DMs; DMs show the user instead).
	Title string `json:"title,omitempty"`
}

// PhotoSize is one entry of a photo message's size ladder (Telegram delivers
// the same photo in progressively larger sizes; the channel picks the
// largest whose file size fits the download cap).
type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// Document is a generic file message.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	// MimeType is the declared MIME type (the media-inbound path lanes the
	// attachment registry by it); empty when Telegram omits it.
	MimeType string `json:"mime_type,omitempty"`
}

// Message is a Telegram message — inbound (in updates) or the echo of a
// send (sendMessage/editMessageText return the message, whose MessageID the
// streaming-edit pipeline needs). Only consumed fields are decoded.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      *Chat  `json:"chat,omitempty"`
	Text      string `json:"text,omitempty"`
	Caption   string `json:"caption,omitempty"`
	// Photo holds the size ladder of a photo message (ascending).
	Photo    []PhotoSize `json:"photo,omitempty"`
	Document *Document   `json:"document,omitempty"`
	// ReplyToMessage is the quoted message (only message_id/from are
	// consumed — the reply-to-bot group trigger).
	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
	// ViaBot is set when the message was sent through an inline bot
	// (foreign-bot filter).
	ViaBot *User `json:"via_bot,omitempty"`
}

// CallbackQuery is a tap on an inline keyboard button (approval cards).
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from,omitempty"`
	Data    string   `json:"data,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// Update is one delivered update. Exactly one of the payload pointers is set
// on a well-formed update; ID is the offset cursor (last processed + 1
// acknowledges).
type Update struct {
	ID            int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// File is the getFile result. FilePath is the server-relative download name:
// the file lives at <base>/file/bot<token>/<file_path>.
type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path,omitempty"`
}

// WebhookInfo is the getWebhookInfo result. The channel runs polling-only,
// so PendingUpdateCount is the poll-path liveness probe (design D10: a
// wedged long poll would pile up pending updates).
type WebhookInfo struct {
	PendingUpdateCount int `json:"pending_update_count"`
}

// BotCommand is one entry of the bot's command menu (setMyCommands).
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// GetUpdatesParams are the getUpdates long-poll parameters.
type GetUpdatesParams struct {
	// Offset is the update_id of the last processed update + 1; 0 (and any
	// negative) means "no confirmation yet" and is omitted from the body.
	Offset int64 `json:"offset,omitempty"`
	// TimeoutSeconds is the server-side long-poll duration; 0 short-polls
	// and is omitted.
	TimeoutSeconds int `json:"timeout,omitempty"`
	// AllowedUpdates restricts the delivered update types; nil/empty means
	// the Telegram default and is omitted.
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

// SendMessageParams are the sendMessage parameters.
type SendMessageParams struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
	// ParseMode is "" (plain), "MarkdownV2" or "HTML"; "" omits the field.
	ParseMode string `json:"parse_mode,omitempty"`
	// ReplyMarkup carries a caller-serialized inline keyboard (the approval
	// cards build JSON directly); nil omits the field.
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

// EditMessageTextParams are the editMessageText parameters.
type EditMessageTextParams struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	// ParseMode is "" (plain), "MarkdownV2" or "HTML"; "" omits the field.
	ParseMode string `json:"parse_mode,omitempty"`
	// ReplyMarkup carries a caller-serialized inline keyboard; nil omits
	// the field.
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

// SendDocumentParams are the sendDocument upload parameters.
type SendDocumentParams struct {
	ChatID int64 `json:"chat_id"`
	// Document is the file body, streamed into the multipart part.
	Document io.Reader
	// FileName names the multipart part (what the recipient sees).
	FileName string
	// Caption is an optional plain-text caption.
	Caption string
}

// responseParameters carries the Bot API flood-control hint.
type responseParameters struct {
	RetryAfter int `json:"retry_after"`
}

// response is the Bot API envelope every method answers with:
// {"ok":bool,"result":...,"error_code":int,"description":str,"parameters":{"retry_after":int}}.
type response struct {
	OK          bool               `json:"ok"`
	Result      json.RawMessage    `json:"result"`
	ErrorCode   int                `json:"error_code"`
	Description string             `json:"description"`
	Parameters  responseParameters `json:"parameters"`
}
