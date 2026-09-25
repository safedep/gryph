package cursor

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/shell"
)

// source names the MCP server, because a Cursor MCP hook sends no server
// name. See mcpCommandSource and mcpURLSource for the naming rules.
func (s MCPServer) source() string {
	if name := mcpURLSource(s.URL); name != "" {
		return name
	}
	return mcpCommandSource(s.Command)
}

var mcpTransportSegments = []string{"sse", "mcp"}

// mcpURLSource names a remote server by its host and port and the first
// path segment. One host can serve many servers under different paths. A
// first segment that names the transport, such as "sse", is not part of
// the name.
func mcpURLSource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	first, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if first == "" || slices.Contains(mcpTransportSegments, strings.ToLower(first)) {
		return host
	}
	return host + "/" + first
}

// launcher describes a program that starts an MCP server from a package,
// a module, an image or a script. The server name is the first operand
// after the subcommand.
type launcher struct {
	subcommands [][]string
	valueFlags  []string
	boolFlags   []string
	// flagsTakeValue is true when a flag that is in neither list takes the
	// next word as its value.
	flagsTakeValue bool
	targetFlags    []string
	trimVersion    func(string) string
}

var (
	npmFlags  = []string{"-p", "--package", "-c", "--call", "-w", "--workspace"}
	pypiFlags = []string{"--from", "--with", "--with-requirements", "--python", "-p", "--index", "--index-url", "--extra-index-url", "--directory", "--project", "--spec", "--pip-args"}

	npmLauncher = launcher{valueFlags: npmFlags, trimVersion: trimNPMVersion}

	mcpLaunchers = map[string]launcher{
		"npx":    npmLauncher,
		"pnpx":   npmLauncher,
		"bunx":   npmLauncher,
		"npm":    {subcommands: [][]string{{"exec"}, {"x"}}, valueFlags: npmFlags, trimVersion: trimNPMVersion},
		"pnpm":   {subcommands: [][]string{{"dlx"}, {"exec"}}, valueFlags: npmFlags, trimVersion: trimNPMVersion},
		"yarn":   {subcommands: [][]string{{"dlx"}}, valueFlags: npmFlags, trimVersion: trimNPMVersion},
		"bun":    {subcommands: [][]string{{"x"}, {"run"}}, valueFlags: npmFlags, trimVersion: trimNPMVersion},
		"uvx":    {valueFlags: pypiFlags, trimVersion: trimPyPIVersion},
		"uv":     {subcommands: [][]string{{"run"}, {"tool", "run"}}, valueFlags: pypiFlags, trimVersion: trimPyPIVersion},
		"pipx":   {subcommands: [][]string{{"run"}}, valueFlags: pypiFlags, trimVersion: trimPyPIVersion},
		"python": {valueFlags: []string{"-X", "-W"}, targetFlags: []string{"-m"}},
		"node":   {valueFlags: []string{"-r", "--require", "--import", "--loader"}},
		"deno":   {subcommands: [][]string{{"run"}}, valueFlags: []string{"-c", "--config", "--import-map"}},
		"docker": dockerLauncher,
		"podman": dockerLauncher,
	}

	dockerLauncher = launcher{
		subcommands:    [][]string{{"run"}},
		boolFlags:      []string{"-i", "-t", "-d", "-q", "-P", "--rm", "--init", "--interactive", "--tty", "--detach", "--privileged", "--read-only", "--quiet"},
		flagsTakeValue: true,
		trimVersion:    trimImageVersion,
	}

	pythonProgram = regexp.MustCompile(`^python[0-9.]*$`)
)

// mcpCommandSource names a local server by its program. When the program is
// a known launcher, such as npx, uvx, python -m or docker run, the name is
// the package, the module, the image or the script that it starts, with no
// version, tag or digest. When the launcher operand is not found, the name
// is the launcher.
func mcpCommandSource(command string) string {
	words := commandWords(command)
	if len(words) == 0 {
		return ""
	}
	program := path.Base(strings.ReplaceAll(words[0], `\`, "/"))
	key := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(program), ".exe"), ".cmd")
	if pythonProgram.MatchString(key) {
		key = "python"
	}
	l, ok := mcpLaunchers[key]
	if !ok {
		return program
	}
	target := l.target(words[1:])
	if target == "" {
		return key
	}
	if l.trimVersion != nil {
		target = l.trimVersion(target)
	}
	return target
}

func commandWords(command string) []string {
	words, err := shell.Fields(command, func(string) string { return "" })
	if err != nil {
		return strings.Fields(command)
	}
	return words
}

func (l launcher) target(args []string) string {
	needSub := len(l.subcommands) > 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			if needSub || i+1 >= len(args) {
				return ""
			}
			return args[i+1]
		case strings.HasPrefix(arg, "-") && arg != "-":
			name, _, inline := strings.Cut(arg, "=")
			if slices.Contains(l.targetFlags, name) {
				if inline {
					return arg[len(name)+1:]
				}
				if i+1 < len(args) {
					return args[i+1]
				}
				return ""
			}
			if !inline && l.takesValue(name) {
				i++
			}
		case needSub:
			sub := l.matchSubcommand(args[i:])
			if sub == 0 {
				return ""
			}
			i += sub - 1
			needSub = false
		default:
			return arg
		}
	}
	return ""
}

func (l launcher) takesValue(flag string) bool {
	if slices.Contains(l.valueFlags, flag) {
		return true
	}
	if slices.Contains(l.boolFlags, flag) {
		return false
	}
	if l.flagsTakeValue && !strings.HasPrefix(flag, "--") && len(flag) > 2 {
		for _, c := range flag[1:] {
			if !slices.Contains(l.boolFlags, "-"+string(c)) {
				return true
			}
		}
		return false
	}
	return l.flagsTakeValue
}

func (l launcher) matchSubcommand(args []string) int {
	for _, sub := range l.subcommands {
		if len(args) >= len(sub) && slices.Equal(args[:len(sub)], sub) {
			return len(sub)
		}
	}
	return 0
}

func trimNPMVersion(pkg string) string {
	if i := strings.LastIndex(pkg, "@"); i > 0 {
		return pkg[:i]
	}
	return pkg
}

func trimPyPIVersion(pkg string) string {
	if i := strings.IndexAny(pkg, "=<>!~[@; "); i > 0 {
		return pkg[:i]
	}
	return pkg
}

func trimImageVersion(image string) string {
	image, _, _ = strings.Cut(image, "@")
	slash := strings.LastIndex(image, "/")
	if i := strings.LastIndex(image, ":"); i > slash {
		return image[:i]
	}
	return image
}
