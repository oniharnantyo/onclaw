// Package shellpolicy defines the catastrophic denylist floor: the small set of
// patterns matched against the FULL shell command string that onclaw refuses to
// run under the default "denylist" policy, without asking.
//
// It is the single source of truth for both sides of the feature:
//   - config seeds ShellConfig.Denylist from FloorPatterns()
//   - the shell matcher derives its human-readable category map from
//     CatastrophicFloor (pattern -> category)
//
// Keeping both derivations here means editing a pattern cannot silently leave
// the category map or the default config out of sync.
package shellpolicy

// FloorEntry pairs a compiled-once regexp pattern with the human-readable
// category surfaced in the blocked result and audit log. Custom user patterns
// that are not floor entries fall back to the raw pattern string.
type FloorEntry struct {
	Pattern  string
	Category string
}

// CatastrophicFloor is matched against the entire command string. Order does
// not matter; matching returns on the first hit.
//
// Categories:
//   - mass-destruction: rm -rf of a broad target (/, ~, $HOME, *, or none),
//     mkfs, dd to a block device, fork bombs, shutdown/reboot/halt/poweroff,
//     init 0/6.
//   - rce-pipe: a downloader (curl/wget/fetch) piped into an interpreter/shell.
//   - reverse-shell: /dev/tcp|udp, bash -i with socket redir, nc -e/-c, mkfifo.
var CatastrophicFloor = []FloorEntry{
	{Pattern: `(?i)\brm\b(?:\s+--[a-z-]+)*\s+-[a-z]*(?:r[a-z]*f[a-z]*|f[a-z]*r[a-z]*)(?:\s+--[a-z-]+)*\s+/(?:\s|$|\S)`, Category: "mass-destruction"},
	{Pattern: `(?i)\brm\b(?:\s+--[a-z-]+)*\s+-[a-z]*(?:r[a-z]*f[a-z]*|f[a-z]*r[a-z]*)(?:\s+--[a-z-]+)*\s+~(?:\s|$|\S)`, Category: "mass-destruction"},
	{Pattern: `(?i)\brm\b(?:\s+--[a-z-]+)*\s+-[a-z]*(?:r[a-z]*f[a-z]*|f[a-z]*r[a-z]*)(?:\s+--[a-z-]+)*\s+\$HOME(?:\s|$)`, Category: "mass-destruction"},
	{Pattern: `(?i)\brm\b(?:\s+--[a-z-]+)*\s+-[a-z]*(?:r[a-z]*f[a-z]*|f[a-z]*r[a-z]*)(?:\s+--[a-z-]+)*\s+\*(?:\s|$)`, Category: "mass-destruction"},
	{Pattern: `(?i)\brm\b(?:\s+--[a-z-]+)*\s+-[a-z]*(?:r[a-z]*f[a-z]*|f[a-z]*r[a-z]*)(?:\s+--[a-z-]+)*\s+$`, Category: "mass-destruction"},
	{Pattern: `\bmkfs(?:\.\w+)?\b`, Category: "mass-destruction"},
	{Pattern: `(?i)\bdd\b.*\bof=/dev/(?:sd|nvme|disk|mmcblk|hd|vd)`, Category: "mass-destruction"},
	{Pattern: `:\s*\(\s*\)\s*\{`, Category: "mass-destruction"},
	{Pattern: `\b(?:shutdown|reboot|halt|poweroff)\b`, Category: "mass-destruction"},
	{Pattern: `\binit\s+[06]\b`, Category: "mass-destruction"},
	{Pattern: `(?i)\b(?:curl|wget|fetch)\b.*\|.*\b(?:(?:ba)?sh|zsh|dash|python3?|perl|ruby|node)\b`, Category: "rce-pipe"},
	{Pattern: `\/dev\/(?:tcp|udp)\/`, Category: "reverse-shell"},
	{Pattern: `(?i)\bbash\b.*\-i.*(?:>&|0>&1|&>)`, Category: "reverse-shell"},
	{Pattern: `(?i)\b(?:nc|ncat|netcat)\b.*\-[ec]\b`, Category: "reverse-shell"},
	{Pattern: `\bmkfifo\b`, Category: "reverse-shell"},
}

// FloorPatterns returns the floor patterns as a flat []string, mirroring the
// shape consumed by ShellConfig.Denylist and Scope.ShellDenylist.
func FloorPatterns() []string {
	ps := make([]string, 0, len(CatastrophicFloor))
	for _, e := range CatastrophicFloor {
		ps = append(ps, e.Pattern)
	}
	return ps
}
