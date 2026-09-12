package agents

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
)

// DocumentPublisher publishes an agent-created document into the workspace's
// blob storage and returns its capability URL (add-document-create-tool
// design.md D6): the tool copies the created file into workspace storage
// through this port and stamps the returned URL on the tool result, so the
// transcript card's download link rides the same capability-serving path chat
// attachments already use. The workspace storage resolver implements it; the
// runner binds it per run onto ToolContext, and the registration closure
// passes it through to the create tool's constructor.
type DocumentPublisher interface {
	PublishCreatedDocument(ctx context.Context, workspaceID, name, sourcePath string) (capabilityURL string, err error)
}

// The runner-side port and the create tool's constructor port are the same
// seam — a drift breaks this build, not delivery.
var _ tools.DocumentPublisher = DocumentPublisher(nil)
