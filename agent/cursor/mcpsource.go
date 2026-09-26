package cursor

import (
	"net"
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

var defaultPorts = map[string]string{"http": "80", "https": "443"}

// mcpURLSource names a remote server by its host and port and the first
// path segment. One host can serve many servers under different paths. A
// first segment that names the transport, such as "sse", is not part of
// the name. The host drops a trailing dot and the default port of the
// scheme, so each spelling of one host gives one name.
func mcpURLSource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if port := u.Port(); port != "" && port != defaultPorts[strings.ToLower(u.Scheme)] {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if first == "" || slices.Contains(mcpTransportSegments, strings.ToLower(first)) {
		return host
	}
	return host + "/" + first
}

// launcher describes a program that starts an MCP server from a package,
// a module, an image or a script. The server name is the value of a
// package flag, or else the first operand after the subcommand.
type launcher struct {
	subcommands [][]string
	valueFlags  []string
	boolFlags   []string
	// flagsTakeValue is true when a flag that is in neither list takes the
	// next word as its value.
	flagsTakeValue bool
	targetFlags    []string
	// packageFlags name the package that holds the command. The command
	// name is free text, so the package names the server.
	packageFlags []string
	trimVersion  func(string) string
}

var (
	npmFlags         = []string{"-c", "--call", "-w", "--workspace"}
	npmPackageFlags  = []string{"-p", "--package"}
	pypiFlags        = []string{"--with", "--with-requirements", "--python", "-p", "--index", "--index-url", "--extra-index-url", "--directory", "--project", "--pip-args"}
	pypiPackageFlags = []string{"--from", "--spec"}

	npmLauncher = launcher{valueFlags: npmFlags, packageFlags: npmPackageFlags, trimVersion: trimNPMVersion}

	mcpLaunchers = map[string]launcher{
		"npx":    npmLauncher,
		"pnpx":   npmLauncher,
		"bunx":   npmLauncher,
		"npm":    {subcommands: [][]string{{"exec"}, {"x"}}, valueFlags: npmFlags, packageFlags: npmPackageFlags, trimVersion: trimNPMVersion},
		"pnpm":   {subcommands: [][]string{{"dlx"}, {"exec"}}, valueFlags: npmFlags, packageFlags: npmPackageFlags, trimVersion: trimNPMVersion},
		"yarn":   {subcommands: [][]string{{"dlx"}}, valueFlags: npmFlags, packageFlags: npmPackageFlags, trimVersion: trimNPMVersion},
		"bun":    {subcommands: [][]string{{"x"}, {"run"}}, valueFlags: npmFlags, packageFlags: npmPackageFlags, trimVersion: trimNPMVersion},
		"uvx":    {valueFlags: pypiFlags, packageFlags: pypiPackageFlags, trimVersion: trimPyPIVersion},
		"uv":     {subcommands: [][]string{{"run"}, {"tool", "run"}}, valueFlags: pypiFlags, packageFlags: pypiPackageFlags, trimVersion: trimPyPIVersion},
		"pipx":   {subcommands: [][]string{{"run"}}, valueFlags: pypiFlags, packageFlags: pypiPackageFlags, trimVersion: trimPyPIVersion},
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
// is the launcher. Any other program keeps the path as written, because a
// base name such as github-mcp-server can come from any directory.
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
		return words[0]
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

// target returns the package flag value when the launcher has one. More
// than one package flag makes the command ambiguous, so target then returns
// no name.
func (l launcher) target(args []string) string {
	operand, packages := l.scan(args)
	switch len(packages) {
	case 0:
		return operand
	case 1:
		return packages[0]
	default:
		return ""
	}
}

// scan returns the first operand after the subcommand and the value of
// each package flag before it.
func (l launcher) scan(args []string) (operand string, packages []string) {
	needSub := len(l.subcommands) > 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			if needSub || i+1 >= len(args) {
				return "", packages
			}
			return args[i+1], packages
		case strings.HasPrefix(arg, "-") && arg != "-":
			name, value, inline := strings.Cut(arg, "=")
			if !inline && (slices.Contains(l.targetFlags, name) || slices.Contains(l.packageFlags, name) || l.takesValue(name)) {
				if i+1 < len(args) {
					i++
					value = args[i]
				}
			}
			switch {
			case slices.Contains(l.targetFlags, name):
				return value, packages
			case slices.Contains(l.packageFlags, name):
				packages = append(packages, value)
			}
		case needSub:
			sub := l.matchSubcommand(args[i:])
			if sub == 0 {
				return "", packages
			}
			i += sub - 1
			needSub = false
		default:
			return arg, packages
		}
	}
	return "", packages
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

// trimNPMVersion removes a version range or a dist-tag. npm reads any other
// spec, such as "good@.", "good@x.TGZ" or "good@file:/x", as code from
// another place, so the spec stays in the name.
func trimNPMVersion(pkg string) string {
	i := strings.LastIndex(pkg, "@")
	if i <= 0 {
		return pkg
	}
	spec := pkg[i+1:]
	if npmTag.MatchString(spec) || (npmRange.MatchString(spec) && !npmArchive.MatchString(spec)) {
		return pkg[:i]
	}
	return pkg
}

// trimPyPIVersion removes a version specifier, extras or markers. After "@",
// only "latest" or a PEP 440 version is trimmed. uv reads any other reference,
// and every spaced "name @ X", as a direct reference that runs code from
// another place, as in "good @ 1.0" or "good@1evil". So the whole spec stays
// in the name.
func trimPyPIVersion(pkg string) string {
	if name, ref, ok := strings.Cut(pkg, "@"); ok {
		if strings.TrimSpace(name) != name || !pypiVersion.MatchString(ref) {
			return pkg
		}
		pkg = name
	}
	if i := strings.IndexAny(pkg, "=<>!~[; "); i > 0 {
		return pkg[:i]
	}
	return pkg
}

// npmArchive copies the archive pattern of npm-package-arg. Its "tar.gz"
// dot matches any character, so "1.tarxgz" is an archive too.
var (
	npmTag      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	npmRange    = regexp.MustCompile(`^[\^~<>=v *]*[0-9*xX][0-9A-Za-z.+*| <>=^~-]*$`)
	npmArchive  = regexp.MustCompile(`(?i)[.](tgz|tar.gz|tar)$`)
	pypiVersion = regexp.MustCompile(`(?i)^(latest|v?([0-9]+!)?[0-9]+(\.[0-9]+)*` +
		`([-_.]?(a|b|c|rc|alpha|beta|pre|preview)[-_.]?[0-9]*)?` +
		`(-[0-9]+|[-_.]?(post|rev|r)[-_.]?[0-9]*)?` +
		`([-_.]?dev[-_.]?[0-9]*)?(\+[a-z0-9]+([-_.][a-z0-9]+)*)?)$`)
)

func trimImageVersion(image string) string {
	image, _, _ = strings.Cut(image, "@")
	slash := strings.LastIndex(image, "/")
	if i := strings.LastIndex(image, ":"); i > slash {
		return image[:i]
	}
	return image
}
