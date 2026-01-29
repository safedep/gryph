package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/spf13/cobra"
)

// NewHookCmd creates the internal _hook command.
func NewHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "_hook <agent> <type>",
		Short:  "Internal command invoked by agent hooks",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			agentName := args[0]
			hookType := args[1]

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return err
			}
			defer app.Close()

			// Read event data from stdin
			rawData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}

			// Get adapter
			adapter, ok := app.Registry.Get(agentName)
			if !ok {
				return fmt.Errorf("unknown agent: %s", agentName)
			}

			// Parse event - parser extracts session_id and converts to deterministic UUID
			event, err := adapter.ParseEvent(ctx, hookType, rawData)
			if err != nil {
				return fmt.Errorf("failed to parse event: %w", err)
			}

			// Get or create session using the session ID from the parsed event
			sess, err := app.Store.GetSession(ctx, event.SessionID)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			if sess == nil {
				// Create new session with the ID from the event
				sess = session.NewSessionWithID(event.SessionID, agentName)
				sess.AgentSessionID = event.AgentSessionID // Store original agent session ID for correlation
				sess.WorkingDirectory = event.WorkingDirectory
				sess.ProjectName = detectProjectName(event.WorkingDirectory)
				if err := app.Store.SaveSession(ctx, sess); err != nil {
					return fmt.Errorf("failed to save session: %w", err)
				}
			}

			// Set sequence number
			event.Sequence = sess.TotalActions + 1

			// Save event
			if err := app.Store.SaveEvent(ctx, event); err != nil {
				return fmt.Errorf("failed to save event: %w", err)
			}

			// Update session counts
			sess.TotalActions++
			switch event.ActionType {
			case events.ActionFileRead:
				sess.FilesRead++
			case events.ActionFileWrite:
				sess.FilesWritten++
			case events.ActionCommandExec:
				sess.CommandsExecuted++
			}
			if event.ResultStatus == events.ResultError {
				sess.Errors++
			}
			if err := app.Store.UpdateSession(ctx, sess); err != nil {
				return fmt.Errorf("failed to update session: %w", err)
			}

			// Handle session end events
			if (agentName == "cursor" && hookType == "stop") ||
				(agentName == "claude-code" && hookType == "SessionEnd") {
				sess.End()
				if err := app.Store.UpdateSession(ctx, sess); err != nil {
					return fmt.Errorf("failed to end session: %w", err)
				}
			}

			// For Cursor hooks, output allow response
			if agentName == "cursor" {
				response := cursor.GenerateResponse(true, "")
				os.Stdout.Write(response)
			}

			return nil
		},
	}

	return cmd
}

// hookResponse is used for responding to agent hooks.
type hookResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// writeHookResponse writes a JSON response to stdout for agents that expect it.
func writeHookResponse(allow bool, message string) {
	resp := hookResponse{
		Status: "allow",
	}
	if !allow {
		resp.Status = "deny"
		resp.Message = message
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}

// detectProjectName detects the project name from the working directory.
// It checks for common project manifest files and extracts the name.
func detectProjectName(workDir string) string {
	if workDir == "" {
		return ""
	}

	// Project indicators in priority order
	indicators := []struct {
		file  string
		parse func(string) string
	}{
		{"package.json", parsePackageJSON},
		{"Cargo.toml", parseCargoToml},
		{"go.mod", parseGoMod},
		{"pyproject.toml", parsePyprojectToml},
		{"setup.py", nil}, // fallback to directory name
		{".git", nil},     // fallback to directory name
	}

	for _, ind := range indicators {
		path := filepath.Join(workDir, ind.file)
		if _, err := os.Stat(path); err == nil {
			if ind.parse != nil {
				if name := ind.parse(path); name != "" {
					return name
				}
			}
			// Fallback to directory basename
			return filepath.Base(workDir)
		}
	}

	return filepath.Base(workDir)
}

// parsePackageJSON extracts the name from a package.json file.
func parsePackageJSON(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &pkg) == nil {
		return pkg.Name
	}
	return ""
}

// parseCargoToml extracts the name from a Cargo.toml file.
func parseCargoToml(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Simple parser for [package] name = "..."
	content := string(data)
	for _, line := range filepath.SplitList(content) {
		if len(line) > 7 && line[:7] == "name = " {
			name := line[7:]
			name = trimQuotes(name)
			return name
		}
	}
	return ""
}

// parseGoMod extracts the module name from a go.mod file.
func parseGoMod(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// First line should be "module <name>"
	content := string(data)
	for _, line := range splitLines(content) {
		if len(line) > 7 && line[:7] == "module " {
			mod := line[7:]
			// Return the last path component
			if idx := lastIndex(mod, '/'); idx >= 0 {
				return mod[idx+1:]
			}
			return mod
		}
	}
	return ""
}

// parsePyprojectToml extracts the name from a pyproject.toml file.
func parsePyprojectToml(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Simple parser for name = "..."
	content := string(data)
	for _, line := range splitLines(content) {
		line = trimSpace(line)
		if len(line) > 7 && line[:7] == "name = " {
			name := line[7:]
			name = trimQuotes(name)
			return name
		}
	}
	return ""
}

// trimQuotes removes surrounding quotes from a string.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitLines splits a string into lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// lastIndex returns the index of the last occurrence of substr in s.
func lastIndex(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// trimSpace removes leading and trailing whitespace.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
