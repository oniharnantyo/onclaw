// Package services provides application-layer services.
//
// Auth domain (auth.go, auth_provider.go, auth_password.go,
// auth_password_provider.go, auth_jwt.go) handles identity: the AuthService
// orchestrates provider-based login (registry-registered providers) and token
// issuance/verification (HS256 JWT). The PasswordHasher (argon2id) backs the
// built-in password provider. Types use the Auth* prefix to keep the two
// domains distinct inside one package.
//
// Model catalog domain (modelcatalog.go) resolves provider models through a
// two-tier strategy (live ListModels, then cached models.dev fallback) and
// enriches effort/temperature/context-limit metadata.
//
// File naming convention: each domain's files are prefixed with the domain name
// (auth_*, modelcatalog.go) so the two domains remain grep-distinguishable.
// New domains follow the same prefix rule.
package services
