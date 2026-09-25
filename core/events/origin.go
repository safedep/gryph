package events

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

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

const (
	mcpPrefix    = "mcp__"
	mcpSeparator = "__"
)

// SplitMCPTool splits a tool name of the form mcp__<server>__<tool>. A server
// name can hold "__", so a name with more than one separator has no single
// reading. The server is then empty, and a rule that trusts one server fails
// closed. MCPServers gives every reading for a rule that denies one server.
func SplitMCPTool(name string) (server, tool string, ok bool) {
	servers := MCPServers(name)
	switch len(servers) {
	case 0:
		return "", "", false
	case 1:
		return servers[0], name[len(mcpPrefix)+len(servers[0])+len(mcpSeparator):], true
	default:
		return "", strings.TrimPrefix(name, mcpPrefix), true
	}
}

// MCPServers returns every server name that an MCP tool name can hold, one
// for each "__" that has text on both sides. The MCP server author chooses
// the tool name, so mcp__evil__read__file gives "evil" and "evil__read".
func MCPServers(name string) []string {
	rest, found := strings.CutPrefix(name, mcpPrefix)
	if !found {
		return nil
	}
	var servers []string
	for i := 1; i+len(mcpSeparator) < len(rest); i++ {
		if strings.HasPrefix(rest[i:], mcpSeparator) {
			servers = append(servers, rest[:i])
		}
	}
	return servers
}

// OriginSources returns every MCP server that an MCP origin can name. The
// source that the adapter claims is the only one. Without a claim, each
// reading of the tool name is a source.
func OriginSources(origin privacy.Origin, source, tool string) []string {
	switch {
	case origin != privacy.OriginMCP:
		return nil
	case source != "":
		return []string{source}
	default:
		return MCPServers(tool)
	}
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
// A map response gives each key and then its string values, joined by new
// lines in key order. A tool, such as an MCP server, controls its keys, so
// the keys count as content. Over MaxObservedBytes, each string keeps a fair share of the budget, so one
// long value cannot push out the others, and OutputTruncated is set.
func (e *Event) ObserveOutput(response any) {
	if e.FullContent != "" || response == nil {
		return
	}
	var values []string
	collectStrings(response, &values)
	e.FullContent, e.OutputTruncated = joinWithin(values, MaxObservedBytes)
}

func collectStrings(v any, values *[]string) {
	switch v := v.(type) {
	case string:
		if v != "" {
			*values = append(*values, v)
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			collectStrings(k, values)
			collectStrings(v[k], values)
		}
	case []any:
		for _, item := range v {
			collectStrings(item, values)
		}
	}
}

// joinWithin joins values by new lines in at most budget bytes. A short
// value keeps all of its bytes, and the long values share the rest equally.
// Each cut falls on a rune boundary.
func joinWithin(values []string, budget int) (string, bool) {
	total := len(values) - 1
	for _, v := range values {
		total += len(v)
	}
	if total <= budget {
		return strings.Join(values, "\n"), false
	}

	keep := make([]int, len(values))
	order := make([]int, len(values))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(values[a]) - len(values[b]) })
	left := max(budget-(len(values)-1), 0)
	for n, i := range order {
		keep[i] = min(len(values[i]), left/(len(values)-n))
		left -= keep[i]
	}

	var b strings.Builder
	b.Grow(budget)
	for i, v := range values {
		n := keep[i]
		for n > 0 && n < len(v) && !utf8.RuneStart(v[n]) {
			n--
		}
		if n == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(v[:n])
	}
	return b.String(), true
}
