package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type Dreamer struct {
	EpisodicStore    EpisodicStore
	CoreStore        CoreStore
	StagedWriteStore StagedWriteStore
	ReviewModel      model.AgenticModel
	AgentName        string
	Workspace        string
	Threshold        int
	DebounceDuration time.Duration
	WriteApproval    bool
	ReviewModelName  string

	mu         sync.Mutex
	lastRun    map[string]time.Time
	runCounter int64
}

// NewDreamer constructs a Dreamer. threshold=0 defaults to 5, debounce=0 defaults to 10m.
func NewDreamer(
	episodicStore EpisodicStore,
	coreStore CoreStore,
	stagedWriteStore StagedWriteStore,
	reviewModel model.AgenticModel,
	agentName string,
	workspace string,
	threshold int,
	debounce time.Duration,
	writeApproval bool,
	reviewModelName string,
) *Dreamer {
	if threshold <= 0 {
		threshold = 5
	}
	if debounce <= 0 {
		debounce = 10 * time.Minute
	}
	return &Dreamer{
		EpisodicStore:    episodicStore,
		CoreStore:        coreStore,
		StagedWriteStore: stagedWriteStore,
		ReviewModel:      reviewModel,
		AgentName:        agentName,
		Workspace:        workspace,
		Threshold:        threshold,
		DebounceDuration: debounce,
		WriteApproval:    writeApproval,
		ReviewModelName:  reviewModelName,
		lastRun:          make(map[string]time.Time),
	}
}

func (d *Dreamer) MaybeDream(ctx context.Context) error {
	if d.EpisodicStore == nil {
		return nil
	}

	d.mu.Lock()
	last, ok := d.lastRun[d.AgentName]
	now := time.Now()
	if ok && now.Sub(last) < d.DebounceDuration {
		d.mu.Unlock()
		return nil
	}
	d.lastRun[d.AgentName] = now
	d.mu.Unlock()

	count, err := d.EpisodicStore.CountUnpromoted(ctx, d.AgentName)
	if err != nil {
		return fmt.Errorf("count unpromoted: %w", err)
	}
	if count < d.Threshold {
		return nil
	}

	return d.dream(ctx)
}

func (d *Dreamer) dream(ctx context.Context) error {
	episodes, err := d.EpisodicStore.ListUnpromoted(ctx, d.AgentName)
	if err != nil {
		return fmt.Errorf("list unpromoted: %w", err)
	}
	if len(episodes) == 0 {
		return nil
	}

	digest := FormatDreamDigest(episodes)
	if digest == "" {
		return nil
	}

	var synthesis string
	var synthesisErr error
	if d.ReviewModel != nil {
		synthesis, synthesisErr = synthesizeFacts(ctx, d.ReviewModel, digest)
	} else {
		synthesis, synthesisErr = extractiveFallbackDigest(episodes)
	}

	if synthesisErr != nil {
		return fmt.Errorf("synthesize facts: %w", synthesisErr)
	}
	if synthesis == "" || strings.TrimSpace(synthesis) == "" || synthesis == "NONE" {
		for _, ep := range episodes {
			_ = d.EpisodicStore.MarkPromoted(ctx, ep.ID)
		}
		return nil
	}

	lines := parseSynthesizedFacts(synthesis)
	if len(lines) == 0 {
		for _, ep := range episodes {
			_ = d.EpisodicStore.MarkPromoted(ctx, ep.ID)
		}
		return nil
	}

	var promotions []string
	var supersessions []string

	for _, fact := range lines {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}

		if d.WriteApproval && d.StagedWriteStore != nil {
			_, stageErr := d.StagedWriteStore.StageWrite(ctx, d.AgentName, "add", "", fact)
			if stageErr != nil {
				return fmt.Errorf("stage dream write: %w", stageErr)
			}
			promotions = append(promotions, fact)
		} else {
			newContent, writeErr := d.CoreStore.WriteCore(ctx, d.Workspace, "add", "", fact)
			if writeErr != nil {
				if strings.Contains(writeErr.Error(), "character limit") || strings.Contains(writeErr.Error(), "exceed") {
					existingCore, readErr := d.CoreStore.ReadCore(ctx, d.Workspace)
					if readErr == nil && existingCore != "" {
						consolidated := consolidateFacts(existingCore, fact)
						_, writeErr2 := d.CoreStore.WriteCore(ctx, d.Workspace, "replace", extractSupersessionTarget(existingCore), consolidated)
						if writeErr2 != nil {
							if d.StagedWriteStore != nil {
								_, _ = d.StagedWriteStore.StageWrite(ctx, d.AgentName, "add", "", fact)
							}
						} else {
							supersessions = append(supersessions, fact)
						}
					}
				}
				continue
			}
			promotions = append(promotions, fact)
			_ = newContent
		}
	}

	for _, ep := range episodes {
		_ = d.EpisodicStore.MarkPromoted(ctx, ep.ID)
	}

	d.writeDreamsRecord(promotions, supersessions, len(episodes))

	return nil
}

// synthesizeFacts calls the review model to extract durable facts from a digest.
func synthesizeFacts(ctx context.Context, reviewModel model.AgenticModel, digest string) (string, error) {
	prompt := fmt.Sprintf(`You are reviewing recent agent sessions to extract durable long-term memory facts.

From the session digests below, extract:
1. **User preferences** — coding style, tool choices, workflow habits
2. **Project facts** — architecture decisions, tech stack choices, project structure
3. **Recurring patterns** — repeated tasks, common issues, workflow patterns
4. **Key decisions** — important technical or design decisions made

Rules:
- Keep each fact extremely concise (one line each, max ~200 chars)
- Format each fact as a separate line starting with "- "
- Only include facts that are DURABLE — likely to be useful across multiple sessions
- Skip temporary or one-off details
- If no durable facts are found, reply only with "NONE"

Session Digests:
%s`, digest)

	resp, err := reviewModel.Generate(ctx, []*schema.AgenticMessage{
		schema.UserAgenticMessage(prompt),
	})
	if err != nil {
		return "", fmt.Errorf("generate synthesis: %w", err)
	}
	if resp == nil {
		return "NONE", nil
	}
	return getAgenticMessageText(resp), nil
}

// extractiveFallbackDigest produces an extractive fallback from episodes when
// the review model is unavailable.
func extractiveFallbackDigest(episodes []*EpisodicSummary) (string, error) {
	var lines []string
	seen := make(map[string]bool)

	for _, ep := range episodes {
		if ep == nil {
			continue
		}
		// Use l0_abstract as the durable fact
		abstract := strings.TrimSpace(ep.L0Abstract)
		if abstract != "" && !seen[abstract] {
			lines = append(lines, fmt.Sprintf("- %s", abstract))
			seen[abstract] = true
		}
		// Extract topic-based facts
		if ep.KeyTopics != "" {
			for _, topic := range strings.Split(ep.KeyTopics, ",") {
				topic = strings.TrimSpace(topic)
				if topic != "" && !seen[topic] {
					lines = append(lines, fmt.Sprintf("- Related topic: %s", topic))
					seen[topic] = true
				}
			}
		}
	}

	if len(lines) == 0 {
		return "NONE", nil
	}
	return strings.Join(lines, "\n"), nil
}

// parseSynthesizedFacts parses "- " prefixed lines from a synthesis response.
func parseSynthesizedFacts(text string) []string {
	var facts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			fact := strings.TrimSpace(line[2:])
			if fact != "" && strings.ToUpper(fact) != "NONE" {
				facts = append(facts, fact)
			}
		}
	}
	return facts
}

// consolidateFacts merges a new fact into existing core content.
// Simple append strategy with dedup — avoids duplicates by checking for substrings.
func consolidateFacts(existing, newFact string) string {
	if existing == "" {
		return newFact
	}
	// Check for duplicate
	lowerExisting := strings.ToLower(existing)
	lowerFact := strings.ToLower(newFact)
	if strings.Contains(lowerExisting, lowerFact) {
		return existing
	}
	if !strings.HasSuffix(existing, "\n") {
		return existing + "\n" + newFact
	}
	return existing + newFact + "\n"
}

// extractSupersessionTarget finds the first line in the core to replace with
// consolidated content. Returns the full content as target if no clear candidate.
func extractSupersessionTarget(coreContent string) string {
	lines := strings.SplitN(coreContent, "\n", 2)
	if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		return strings.TrimSpace(lines[0])
	}
	return strings.TrimSpace(coreContent)
}

// writeDreamsRecord appends a sweep record to DREAMS.md in the agent workspace.
func (d *Dreamer) writeDreamsRecord(promotions, supersessions []string, episodeCount int) {
	if d.Workspace == "" {
		return
	}

	path := filepath.Join(d.Workspace, "DREAMS.md")
	now := time.Now().Format(time.RFC3339)

	var score float64
	if episodeCount > 0 {
		score = float64(len(promotions)) / float64(episodeCount)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Dreaming Sweep %d\n", d.runCounter))
	d.runCounter++
	sb.WriteString(fmt.Sprintf("- **Timestamp:** %s\n", now))
	sb.WriteString(fmt.Sprintf("- **Agent:** %s\n", d.AgentName))
	sb.WriteString(fmt.Sprintf("- **Episodes Reviewed:** %d\n", episodeCount))
	sb.WriteString(fmt.Sprintf("- **Score:** %.2f\n", score))
	if d.ReviewModelName != "" {
		sb.WriteString(fmt.Sprintf("- **Review Model:** %s\n", d.ReviewModelName))
	}

	if len(promotions) > 0 {
		sb.WriteString("\n### Promotions\n\n")
		for _, p := range promotions {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
	}
	if len(supersessions) > 0 {
		sb.WriteString("\n### Supersessions\n\n")
		for _, s := range supersessions {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}
	sb.WriteString("\n---\n\n")

	// Append to file
	existing, err := os.ReadFile(path)
	if err != nil {
		existing = []byte("# Dreaming Review Records\n\n")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, append(existing, sb.String()...), 0644)
}

// ParseDreamSweeps reads a DREAMS.md file and returns structured sweep records.
func ParseDreamSweeps(path string) ([]*DreamSweepRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dreams file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var records []*DreamSweepRecord
	var current *DreamSweepRecord
	inPromotions := false
	inSupersessions := false

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## Dreaming Sweep ") {
			if current != nil {
				records = append(records, current)
			}
			current = &DreamSweepRecord{}
			inPromotions = false
			inSupersessions = false
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(trimmed, "### Promotions") {
			inPromotions = true
			inSupersessions = false
			continue
		}
		if strings.HasPrefix(trimmed, "### Supersessions") {
			inPromotions = false
			inSupersessions = true
			continue
		}

		if trimmed == "" || trimmed == "---" {
			continue
		}

		if strings.HasPrefix(trimmed, "- **Timestamp:**") {
			current.Timestamp = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Timestamp:**"))
			continue
		}
		if strings.HasPrefix(trimmed, "- **Agent:**") {
			current.Agent = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Agent:**"))
			continue
		}
		if strings.HasPrefix(trimmed, "- **Episodes Reviewed:**") {
			countStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Episodes Reviewed:**"))
			if count, err := strconv.Atoi(countStr); err == nil {
				current.EpisodesCount = count
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- **Score:**") {
			scoreStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Score:**"))
			if score, err := strconv.ParseFloat(scoreStr, 64); err == nil {
				current.Score = score
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- **Review Model:**") {
			current.ReviewModel = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Review Model:**"))
			continue
		}

		if inPromotions && strings.HasPrefix(trimmed, "- ") {
			current.Promotions = append(current.Promotions, strings.TrimSpace(trimmed[2:]))
			continue
		}
		if inSupersessions && strings.HasPrefix(trimmed, "- ") {
			current.Supersessions = append(current.Supersessions, strings.TrimSpace(trimmed[2:]))
			continue
		}
	}

	if current != nil {
		records = append(records, current)
	}

	return records, nil
}

// ListDreamFiles scans a directory tree for DREAMS.md files.
func ListDreamFiles(workspaceDirs ...string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, dir := range workspaceDirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, "DREAMS.md")
		if _, err := os.Stat(path); err == nil && !seen[path] {
			files = append(files, path)
			seen[path] = true
		}
	}
	return files, nil
}

// PeriodicPruner periodically deletes expired episodic summaries.
type PeriodicPruner struct {
	EpisodicStore EpisodicStore
	Interval      time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewPeriodicPruner creates a new PeriodicPruner. interval=0 defaults to 1 hour.
func NewPeriodicPruner(episodicStore EpisodicStore, interval time.Duration) *PeriodicPruner {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	return &PeriodicPruner{
		EpisodicStore: episodicStore,
		Interval:      interval,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the periodic pruning goroutine.
func (p *PeriodicPruner) Start(ctx context.Context) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				n, err := p.EpisodicStore.PruneExpired(ctx)
				if err == nil && n > 0 {
					// Log would go here in production
				}
			case <-p.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the pruner goroutine to exit and waits for it.
func (p *PeriodicPruner) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}
