package shellcmd

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// parsedArgs holds the arguments of a command split the way getopt splits
// them.
type parsedArgs struct {
	operands []string
	values   map[string][]string
}

func (p parsedArgs) value(flags ...string) []string {
	var out []string
	for _, f := range flags {
		out = append(out, p.values[f]...)
	}
	return out
}

func (p parsedArgs) has(flags ...string) bool {
	return slices.ContainsFunc(flags, func(f string) bool { return len(p.values[f]) > 0 })
}

// parseArgs splits args into operands and option values. A short option in
// valueFlags takes the rest of its word as the value, or the next word when
// the rest is empty, so "-fNR 9000:h:22" and "-d@file" both parse. A long
// option takes "=value", or the next word when it is in valueFlags. The
// values of other options are not kept.
func parseArgs(args []string, valueFlags map[string]bool) parsedArgs {
	p := parsedArgs{values: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			p.operands = append(p.operands, args[i+1:]...)
			return p
		case strings.HasPrefix(a, "--"):
			if name, v, ok := strings.Cut(a, "="); ok {
				p.values[name] = append(p.values[name], v)
			} else if valueFlags[a] && i+1 < len(args) {
				i++
				p.values[a] = append(p.values[a], args[i])
			}
		case strings.HasPrefix(a, "-") && a != "-":
			for j := 1; j < len(a); j++ {
				flag := "-" + a[j:j+1]
				if !valueFlags[flag] {
					continue
				}
				v := a[j+1:]
				if v == "" && i+1 < len(args) {
					i++
					v = args[i]
				}
				p.values[flag] = append(p.values[flag], v)
				break
			}
		default:
			p.operands = append(p.operands, a)
		}
	}
	return p
}

func flagSet(flags ...string) map[string]bool {
	set := make(map[string]bool, len(flags))
	for _, f := range flags {
		set[f] = true
	}
	return set
}

// readCommands read every file operand. The value is the set of options
// that take a value, so the value is not read as a file.
var readCommands = map[string]map[string]bool{
	"cat": nil, "tac": nil, "less": nil, "more": nil, "strings": flagSet("-n", "-t"),
	"xxd": flagSet("-c", "-g", "-l", "-s", "-o"), "od": flagSet("-A", "-j", "-N", "-t", "-w"),
	"hexdump": flagSet("-e", "-f", "-n", "-s"), "base64": flagSet("-w"), "base32": flagSet("-w"),
	"nl": flagSet("-b", "-s", "-v", "-w"), "wc": nil, "uniq": flagSet("-f", "-s", "-w"),
	"cut": flagSet("-b", "-c", "-d", "-f"), "diff": nil, "cmp": nil, "md5sum": nil,
	"sha1sum": nil, "sha256sum": nil, "sha512sum": nil, "gzip": nil, "zcat": nil, "bat": nil,
	"vim": flagSet("-c", "-S", "-u", "-i", "-t", "-T"), "vi": flagSet("-c", "-S", "-u", "-t"),
	"nano": nil, "head": flagSet("-n", "-c", "--lines", "--bytes"),
	"tail": flagSet("-n", "-c", "--lines", "--bytes", "-s", "--pid"),
	"sort": flagSet("-o", "-k", "-t", "-S", "-T", "--output", "--key", "--field-separator"),
}

var (
	grepValueFlags = flagSet("-e", "-f", "-m", "-A", "-B", "-C", "-d", "-D", "-g", "-t", "-T",
		"--regexp", "--file", "--max-count", "--context", "--glob", "--type", "--type-not",
		"--include", "--exclude", "--exclude-dir")
	awkValueFlags = flagSet("-F", "-v", "-f", "--file", "--assign", "--field-separator")
	jqValueFlags  = flagSet("-f", "--from-file", "--arg", "--argjson", "--slurpfile",
		"--rawfile", "--indent")
	rsyncValueFlags = flagSet("-e", "--rsh", "-f", "--filter", "--exclude", "--include",
		"--files-from", "--exclude-from", "--include-from", "--password-file", "--log-file",
		"--port", "-T", "--temp-dir", "--backup-dir", "--suffix", "--chmod", "--chown", "-B",
		"--block-size", "--rsync-path", "-M", "--remote-option")
	scpValueFlags = flagSet("-P", "-i", "-o", "-F", "-l", "-S", "-c", "-J", "-D", "-X")
	sshValueFlags = flagSet("-B", "-b", "-c", "-D", "-E", "-e", "-F", "-I", "-i", "-J", "-L",
		"-l", "-m", "-O", "-o", "-P", "-p", "-Q", "-R", "-S", "-W", "-w")
	ncValueFlags = flagSet("-e", "-c", "-g", "-G", "-i", "-I", "-O", "-p", "-q", "-s", "-T",
		"-V", "-w", "-x", "-X")
	curlValueFlags = flagSet("-o", "--output", "-H", "--header", "-d", "--data", "--data-raw",
		"--data-binary", "--data-ascii", "--data-urlencode", "--json", "-X", "--request", "-u",
		"--user", "-A", "--user-agent", "-e", "--referer", "-b", "--cookie", "-c", "--cookie-jar",
		"-F", "--form", "--form-string", "-T", "--upload-file", "-x", "--proxy", "-m", "--max-time",
		"--connect-timeout", "-w", "--write-out", "-r", "--range", "-K", "--config", "--cert", "-E",
		"--key", "--cacert", "--resolve", "--retry", "-C", "--continue-at", "-U", "--proxy-user",
		"-t", "--telnet-option", "-P", "--ftp-port", "-z", "--time-cond", "-Y", "-y", "--url",
		"--connect-to", "--interface", "--dns-servers", "--output-dir")
	wgetValueFlags = flagSet("-O", "--output-document", "-o", "--output-file", "-a",
		"--append-output", "-P", "--directory-prefix", "-U", "--user-agent", "-t", "--tries",
		"-T", "--timeout", "-e", "--execute", "-i", "--input-file", "-B", "--base", "-w", "--wait",
		"-Q", "--quota", "-l", "--level", "-A", "--accept", "-R", "--reject", "-D", "--domains",
		"-I", "-X", "--header", "--post-data", "--post-file", "--body-data", "--body-file",
		"--method", "--user", "--password", "--referer", "--load-cookies", "--save-cookies")
	gitValueFlags = flagSet("-C", "-c", "--git-dir", "--work-tree", "--namespace",
		"--exec-path", "-b", "--branch", "-o", "--origin", "--depth", "-u", "--upload-pack",
		"--reference", "--template", "-j", "--jobs", "--filter", "--separate-git-dir", "--config",
		"--receive-pack", "--push-option", "-t", "--track", "-m", "--shallow-since",
		"--shallow-exclude")
	tarValueFlags = flagSet("-f", "--file", "-C", "--directory", "-T", "--files-from", "-X",
		"--exclude-from", "-b", "--blocking-factor", "-I", "--use-compress-program", "--exclude")
)

// scriptTool records the file operands of a tool whose first operand is a
// program or pattern, such as grep or awk. The program comes from a file
// option or an expression option instead when either is set, and then every
// operand is a file.
func (w *walker) scriptTool(p parsedArgs, fileFlags, exprFlags []string, cwds dirs) {
	w.addAll(p.value(fileFlags...), AccessRead, cwds)
	ops := p.operands
	if !p.has(fileFlags...) && !p.has(exprFlags...) && len(ops) > 0 {
		ops = ops[1:]
	}
	w.addAll(ops, AccessRead, cwds)
}

// tar records the archive and the members. The first word can be a bundle
// of flags without a dash, as in "tar czf out.tgz dir". A create, append or
// update writes the archive and reads the members. Other modes read the
// archive.
func (w *walker) tar(args []string, cwds dirs) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && isLetters(args[0]) {
		args = append([]string{"-" + args[0]}, args[1:]...)
	}
	p := parseArgs(args, tarValueFlags)
	archive := AccessRead
	for _, a := range args {
		if a == "--create" || a == "--append" || a == "--update" ||
			(strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(shortFlags(a, tarValueFlags), "cru")) {
			archive = AccessWrite
			break
		}
	}
	for _, f := range p.value("-f", "--file") {
		if f != "-" {
			w.add(f, archive, cwds)
		}
	}
	w.addAll(p.value("-T", "--files-from", "-X", "--exclude-from"), AccessRead, cwds)
	if archive == AccessWrite {
		w.addAll(p.operands, AccessRead, cwds)
	}
}

// shortFlags returns the flag letters of a short option word, up to and
// including the first letter that takes a value.
func shortFlags(word string, valueFlags map[string]bool) string {
	for i := 1; i < len(word); i++ {
		if valueFlags["-"+word[i:i+1]] {
			return word[1 : i+1]
		}
	}
	return word[1:]
}

func isLetters(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
	}) < 0
}

// curl records the hosts, the uploaded files, and the output file.
func (w *walker) curl(p parsedArgs, cwds dirs) {
	for _, u := range append(p.operands, p.value("--url")...) {
		w.addHost(u)
	}
	for _, v := range p.value("-d", "--data", "--data-binary", "--data-ascii", "--json") {
		if file, ok := strings.CutPrefix(v, "@"); ok {
			w.addUpload(file, cwds)
		}
	}
	for _, v := range p.value("--data-urlencode") {
		if name, file, ok := strings.Cut(v, "@"); ok && !strings.Contains(name, "=") {
			w.addUpload(file, cwds)
		}
	}
	for _, v := range p.value("-F", "--form") {
		_, field, _ := strings.Cut(v, "=")
		if file, ok := strings.CutPrefix(field, "@"); ok {
			w.addUpload(file, cwds)
		} else if file, ok := strings.CutPrefix(field, "<"); ok {
			w.addUpload(file, cwds)
		}
	}
	for _, f := range p.value("-T", "--upload-file", "-K", "--config") {
		w.addUpload(f, cwds)
	}
	for _, f := range p.value("-o", "--output") {
		if f != "-" {
			w.add(f, AccessWrite, cwds)
		}
	}
}

// wget records the hosts, the uploaded files, and the output files.
func (w *walker) wget(p parsedArgs, cwds dirs) {
	for _, u := range p.operands {
		w.addHost(u)
	}
	w.addAll(p.value("--post-file", "--body-file", "-i", "--input-file"), AccessRead, cwds)
	for _, f := range p.value("-O", "--output-document", "-o", "--output-file", "-a", "--append-output") {
		if f != "-" {
			w.add(f, AccessWrite, cwds)
		}
	}
}

// addUpload records a file that a command sends. The value "-" is stdin.
func (w *walker) addUpload(file string, cwds dirs) {
	if file, _, _ = strings.Cut(file, ";"); file != "" && file != "-" {
		w.add(file, AccessRead, cwds)
	}
}

func (w *walker) remoteShell(p parsedArgs) {
	if len(p.operands) > 0 {
		w.addHost(p.operands[0])
	}
}

// netcat records the host a client connects to. A listener has no host.
func (w *walker) netcat(args []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(shortFlags(a, ncValueFlags), "l") {
			return
		}
	}
	w.remoteShell(parseArgs(args, ncValueFlags))
}

// remoteCopy handles rsync and scp. The last operand is the destination.
// Each remote operand gives a host. A local source is a read, and a local
// destination is a write of the destination and of each source name in it.
func (w *walker) remoteCopy(p parsedArgs, cwds dirs) {
	w.addAll(p.value("--files-from", "--exclude-from", "--include-from", "--password-file"), AccessRead, cwds)
	if len(p.operands) < 2 {
		return
	}
	dest := p.operands[len(p.operands)-1]
	sources := p.operands[:len(p.operands)-1]
	for _, src := range sources {
		if host, _, remote := splitRemote(src); remote {
			w.addHost(host)
		} else {
			w.add(src, AccessRead, cwds)
		}
	}
	if host, _, remote := splitRemote(dest); remote {
		w.addHost(host)
		return
	}
	w.addDest(dest, sources, cwds)
}

// gitRemoteArg gives, for each git subcommand that names a remote, the
// position of the remote among the subcommand's operands.
var gitRemoteArg = map[string]int{
	"clone": 1, "fetch": 1, "pull": 1, "push": 1, "ls-remote": 1,
}

// git records the host of the remote that a git command names.
func (w *walker) git(args []string) {
	ops := parseArgs(args, gitValueFlags).operands
	if len(ops) == 0 {
		return
	}
	i, ok := gitRemoteArg[ops[0]]
	switch {
	case ops[0] == "remote" && len(ops) > 1 && (ops[1] == "add" || ops[1] == "set-url"):
		i, ok = 3, true
	case ops[0] == "submodule" && len(ops) > 1 && ops[1] == "add":
		i, ok = 2, true
	}
	if !ok || i >= len(ops) {
		return
	}
	if host, _, remote := splitRemote(ops[i]); remote {
		w.addHost(host)
	}
}

// openssl records the files of the -in, -key and similar options, the
// file of -out, and the host of -connect.
func (w *walker) openssl(args []string, cwds dirs) {
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-in", "-inkey", "-key", "-cert", "-CAfile", "-kfile", "-pass", "-passin":
			if v, ok := strings.CutPrefix(args[i+1], "file:"); ok {
				w.add(v, AccessRead, cwds)
			} else if !strings.HasPrefix(args[i], "-pass") {
				w.add(args[i+1], AccessRead, cwds)
			}
		case "-out":
			w.add(args[i+1], AccessWrite, cwds)
		case "-connect":
			w.addHost(args[i+1])
		}
	}
}

// devSocket records the host of a bash network redirect such as
// "/dev/tcp/host/port".
func (w *walker) devSocket(target string) {
	for _, prefix := range []string{"/dev/tcp/", "/dev/udp/"} {
		if rest, ok := strings.CutPrefix(target, prefix); ok {
			host, _, _ := strings.Cut(rest, "/")
			w.addHost(host)
		}
	}
}

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"<>()\[\]{}\x60,;]+`)

// inlineURLs records the host of each URL inside a word, so that a URL in
// an inline script such as "python -c '...'" also counts.
func (w *walker) inlineURLs(words []string) {
	for _, word := range words {
		if !strings.Contains(word, "://") {
			continue
		}
		for _, u := range urlPattern.FindAllString(word, -1) {
			w.addHost(u)
		}
	}
}

// addHost records the host of a URL, of "user@host:path", or of a bare host
// name.
func (w *walker) addHost(value string) {
	host := hostOf(value)
	if host != "" && !slices.Contains(w.hosts, host) {
		w.hosts = append(w.hosts, host)
	}
}

func hostOf(value string) string {
	if host, _, remote := splitRemote(value); remote {
		return host
	}
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		value = value[at+1:]
	}
	if i := strings.LastIndex(value, ":"); i >= 0 && !strings.Contains(value, "]") {
		value = value[:i]
	}
	return normalizeHost(value)
}

// splitRemote splits a URL or an scp-style "[user@]host:path" into its host
// and path. It reports false for a local path.
func splitRemote(value string) (host, p string, ok bool) {
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" {
			return "", "", false
		}
		return normalizeHost(u.Hostname()), u.Path, true
	}
	colon := strings.Index(value, ":")
	if colon <= 0 {
		return "", "", false
	}
	if slash := strings.Index(value, "/"); slash >= 0 && slash < colon {
		return "", "", false
	}
	host = value[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	host = normalizeHost(host)
	if host == "" {
		return "", "", false
	}
	return host, value[colon+1:], true
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if h == "" || strings.ContainsAny(h, " \t\"'") {
		return ""
	}
	return h
}
