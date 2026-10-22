package services

import (
	"context"
)

// Application identity reported by AppInfo. The version tracks the build
// assets' version in build/config.yml.
const (
	// AppName is the application name.
	AppName = "buddies"
	// AppVersion is the application version.
	AppVersion = "0.1.0"
)

// AppInfo is the bound service exposing static application metadata.
type AppInfo struct{}

// NewAppInfo returns the application metadata service.
func NewAppInfo() *AppInfo {
	return &AppInfo{}
}

// Metadata describes the running application.
type Metadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Get returns the application's name and version.
func (a *AppInfo) Get(_ context.Context) (Metadata, error) {
	return Metadata{Name: AppName, Version: AppVersion}, nil
}
