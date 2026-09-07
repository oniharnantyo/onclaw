package cli

// Composition root driver registrations.
// Blank imports ensure drivers (postgres store, local storage, password auth) run their init() functions.
import (
	_ "github.com/oniharnantyo/onclaw/internal/services"
	_ "github.com/oniharnantyo/onclaw/internal/storage/local"
	_ "github.com/oniharnantyo/onclaw/internal/store/postgres"
)
