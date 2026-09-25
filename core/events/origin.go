package events

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/safedep/gryph/core/privacy"
)

// webTools are the tool names, in lower case, that fetch web content.
var webTools = []string{"webfetch", "websearch", "web_fetch", "web_search", "google_web_search", "fetch"}

// ClaimOrigin sets the origin of the event from its facts when the adapter
// did not set it. The origin is a claim, not a trust decision.
//   - An MCP tool (mcp__<server>__<tool>) gives mcp, with the server.
//   - A web tool or a network request gives web.
//   - A shell command gives command.
//   - A read inside the working directory gives file_project, and any other
//     read gives file_external.
//   - A write or a delete gives agent, because the agent wrote the content.
//   - A prompt that SetPrompt did not label, and other tool calls, give
//     unknown. Lifecycle events have no origin.
func (e *Event) ClaimOrigin() {
	if e.Origin != "" {
		return
	}
	if server, _, ok := SplitMCPTool(e.ToolName); ok {
		e.Origin, e.OriginSource = privacy.OriginMCP, server
		return
	}
	switch e.ActionType {
	case ActionUserPrompt:
		e.Origin = privacy.OriginUnknown
	case ActionNetworkRequest:
		e.Origin = privacy.OriginWeb
	case ActionCommandExec:
		e.Origin = privacy.OriginCommand
	case ActionFileRead:
		e.Origin = e.readOrigin()
	case ActionFileWrite, ActionFileDelete:
		e.Origin = privacy.OriginAgent
	case ActionToolUse:
		e.Origin = privacy.OriginUnknown
		if isWebTool(e.ToolName) {
			e.Origin = privacy.OriginWeb
		}
	}
}

// SplitMCPTool splits a tool name of the form mcp__<server>__<tool>. A server
// name can hold "__", so a name with more than one separator has no single
// reading. The server is then empty, and a rule on the server fails closed.
func SplitMCPTool(name string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(name, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, found = strings.Cut(rest, "__")
	if !found || server == "" {
		return "", "", false
	}
	if strings.Contains(tool, "__") {
		return "", rest, true
	}
	return server, tool, true
}

func isWebTool(name string) bool {
	lower := strings.ToLower(name)
	return slices.Contains(webTools, lower) || strings.HasPrefix(lower, "browser")
}

// readOrigin compares the read path with the working directory. The origin
// is a claim about the path string. It does not resolve symbolic links. A
// read with no path or no working directory has the origin unknown.
func (e *Event) readOrigin() privacy.Origin {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Path == "" || e.WorkingDirectory == "" {
		return privacy.OriginUnknown
	}
	path := p.Path
	if path == "~" || strings.HasPrefix(path, "~/") {
		return privacy.OriginFileExternal
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.WorkingDirectory, path)
	}
	rel, err := filepath.Rel(e.WorkingDirectory, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return privacy.OriginFileExternal
	}
	return privacy.OriginFileProject
}

// MaxObservedBytes bounds the output that ObserveOutput keeps. The content
// matcher reads the same prefix.
const MaxObservedBytes = 1 << 20

// ObserveOutput sets FullContent from the tool response of a post event, so
// content rules and the scorer see what the agent received. It keeps a
// FullContent that the adapter already set, such as the content of a write.
// A map response gives its string values, joined by new lines in key order,
// up to MaxObservedBytes.
func (e *Event) ObserveOutput(response any) {
	if e.FullContent != "" || response == nil {
		return
	}
	var b strings.Builder
	collectStrings(response, &b)
	e.FullContent = b.String()
}

func collectStrings(v any, b *strings.Builder) {
	if b.Len() >= MaxObservedBytes {
		return
	}
	switch v := v.(type) {
	case string:
		if v == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(v[:min(len(v), MaxObservedBytes-b.Len())])
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			collectStrings(v[k], b)
		}
	case []any:
		for _, item := range v {
			collectStrings(item, b)
		}
	}
}
