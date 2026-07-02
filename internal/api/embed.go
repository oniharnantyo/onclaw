package api

import "embed"

// Assets contains the static assets for the web management console.
//
//go:embed assets/*
var Assets embed.FS
