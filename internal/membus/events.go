package membus

// EpisodeCreated is published after a new episodic summary is written to the database.
type EpisodeCreated struct {
	AgentName string
	EpisodeID int64
	Summary   string
	SourceID  string
}

func (e EpisodeCreated) EventName() string { return "episode_created" }

// DreamCompleted is published after a dreaming sweep finishes.
type DreamCompleted struct {
	AgentName     string
	EpisodesCount int
}

func (e DreamCompleted) EventName() string { return "dream_completed" }

// PruneTick is published periodically by the timer worker to trigger pruning.
type PruneTick struct{}

func (e PruneTick) EventName() string { return "prune_tick" }
