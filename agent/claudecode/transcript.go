package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/cost"
)

type transcriptMessage struct {
	Type    string           `json:"type"`
	Message *messageEnvelope `json:"message"`
}

type messageEnvelope struct {
	Model string        `json:"model"`
	Usage *messageUsage `json:"usage"`
}

type messageUsage struct {
	InputTokens                int64 `json:"input_tokens"`
	OutputTokens               int64 `json:"output_tokens"`
	CacheReadInputTokens       int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens   int64 `json:"cache_creation_input_tokens"`
}

// TranscriptCollector implements cost.TokenCollector by parsing Claude Code transcript files.
type TranscriptCollector struct{}

func NewTranscriptCollector() *TranscriptCollector {
	return &TranscriptCollector{}
}

func (c *TranscriptCollector) Source() cost.CostSource {
	return cost.CostSourceTranscript
}

func (c *TranscriptCollector) Collect(_ context.Context, transcriptPath string) (*cost.SessionUsage, error) {
	if transcriptPath == "" {
		return nil, nil
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	usageByModel := make(map[string]*cost.ModelUsage)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg transcriptMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Debugf("skipping malformed transcript line: %v", err)
			continue
		}

		if msg.Type != "assistant" || msg.Message == nil || msg.Message.Usage == nil {
			continue
		}

		model := msg.Message.Model
		if model == "" {
			model = "unknown"
		}

		mu, ok := usageByModel[model]
		if !ok {
			mu = &cost.ModelUsage{Model: model}
			usageByModel[model] = mu
		}

		mu.InputTokens += msg.Message.Usage.InputTokens
		mu.OutputTokens += msg.Message.Usage.OutputTokens
		mu.CacheReadTokens += msg.Message.Usage.CacheReadInputTokens
		mu.CacheWriteTokens += msg.Message.Usage.CacheCreationInputTokens
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(usageByModel) == 0 {
		return nil, nil
	}

	usage := &cost.SessionUsage{}
	for _, mu := range usageByModel {
		usage.Models = append(usage.Models, *mu)
	}
	usage.Aggregate()

	return usage, nil
}
