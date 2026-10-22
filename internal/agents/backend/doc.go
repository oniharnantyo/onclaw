// Package backend is the agent's jailed working surface: a filesystem
// backend (eino adk/filesystem.Backend) that confines every fs tool path to
// the agent's workspace directory, the jailed shell
// (eino adk/filesystem.Shell) bound to the same directory, and the
// workspace-scoped delete_file tool the fs middleware does not provide.
//
// The jail is a resolved-path convention, not an OS sandbox: every path a
// tool accepts is resolved (symlinks evaluated) and must land inside the
// workspace root (or an explicitly attached read-only root); escape
// attempts — "..", raw absolute paths, symlinks pointing outside — are
// rejected as errors the model reads. The DefaultMountPoint prefix
// (/workspace) is the model-facing contract shared by the fs tools and the
// shell, so a path the model writes in one lane resolves identically in the
// other. The shell is likewise a working-directory convention: commands run
// with the workspace as cwd, a bounded timeout, a bounded output capture,
// and a scrubbed child environment that leaks none of the host app's
// credentials.
package backend
