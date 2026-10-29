// Package botapi is a thin, hand-written Telegram Bot API client
// (add-telegram-channel design D2): raw net/http, every call a JSON POST to
// <base>/bot<token>/<method>, minimal response structs carrying exactly the
// fields the telegram channel consumes, and a typed error classifier so
// callers can branch with errors.As (rate limited, forbidden, not found,
// conflict, auth invalid, parse, transient).
//
// The transport is deliberately dependency-free: tgbotapi/telebot were
// rejected because the transport is the small part and raw HTTP keeps the
// parse surface auditable.
package botapi
