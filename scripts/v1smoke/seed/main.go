// seed command seeds one persisted session event so the /v1 wire smoke can
// bind turns via metadata.onclaw_session (which must reference an existing
// session). Usage:
//
//	go run ./scripts/v1smoke/seed -dsn <url> -workspace <ws-slug> -session <session-id>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres DSN")
	wsSlug := flag.String("workspace", "", "workspace slug")
	sessionID := flag.String("session", "", "session id to seed")
	flag.Parse()
	if *dsn == "" || *wsSlug == "" || *sessionID == "" {
		fmt.Fprintln(os.Stderr, "-dsn, -workspace and -session are required")
		os.Exit(2)
	}

	ctx := context.Background()
	st, err := postgres.New(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	ws, err := st.Workspaces().BySlug(ctx, *wsSlug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace %q: %v\n", *wsSlug, err)
		os.Exit(1)
	}

	adapter := agents.NewADKSessionAdapter(st.SessionEvents(), st.SessionCheckpoints(), ws.ID)
	err = adapter.AppendEvents(ctx, *sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{
		{EventID: "seed-" + fmt.Sprint(time.Now().UnixNano()), TurnID: "seed-turn", Timestamp: time.Now().UTC(), Message: schema.UserAgenticMessage("seed")},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed session: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("seeded session", *sessionID, "in workspace", ws.Slug)
}
