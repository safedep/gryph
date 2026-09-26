package shellcmd

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// options is the option table of one tool.
type options struct {
	// values are the options that take a value.
	values map[string]bool
	// flags are long options without a value. A prefix match needs them to
	// find the one option that a prefix names.
	flags map[string]bool
	// abbrev is true for a tool that parses with getopt_long. Such a tool
	// accepts a unique prefix of a long option, so "--cr" is "--create".
	abbrev bool
	// guess is true when the table lists every option of the tool that
	// the walker knows. The value of an option not in the table is then
	// kept as a guess, because the option can write a file.
	guess bool
}

func valueOptions(names ...string) options {
	return options{values: flagSet(names...)}
}

// known reports whether the table lists the option. A long option that
// starts with "--no-" is known when the table lists the option without it.
func (o options) known(name string) bool {
	if o.values[name] || o.flags[name] {
		return true
	}
	rest, ok := strings.CutPrefix(name, "--no-")
	return ok && o.flags["--"+rest]
}

// longName returns the long option that name stands for. An exact name
// wins. A prefix of more than one option stays as it is, because
// getopt_long rejects it.
func (o options) longName(name string) string {
	if !o.abbrev || o.values[name] || o.flags[name] {
		return name
	}
	match := ""
	for _, set := range []map[string]bool{o.values, o.flags} {
		for n := range set {
			if !strings.HasPrefix(n, name) {
				continue
			}
			if match != "" {
				return name
			}
			match = n
		}
	}
	if match == "" {
		return name
	}
	return match
}

// parsedArgs holds the arguments of a command split the way getopt splits
// them.
type parsedArgs struct {
	operands []string
	values   map[string][]string
	seen     map[string]bool
	// guesses are the words that can be the value of an option that the
	// table does not know. The table must set guess. A guess that is
	// the next word stays an operand too.
	guesses []string
	// seq holds the operands and the option values in command line order.
	// An operand has an empty flag.
	seq []flagValue
}

type flagValue struct {
	flag  string
	value string
}

func (p *parsedArgs) addOperands(values ...string) {
	p.operands = append(p.operands, values...)
	for _, v := range values {
		p.seq = append(p.seq, flagValue{value: v})
	}
}

func (p *parsedArgs) addValue(flag, value string) {
	p.values[flag] = append(p.values[flag], value)
	p.seq = append(p.seq, flagValue{flag: flag, value: value})
}

func (p parsedArgs) value(flags ...string) []string {
	var out []string
	for _, f := range flags {
		out = append(out, p.values[f]...)
	}
	return out
}

func (p parsedArgs) has(flags ...string) bool {
	return slices.ContainsFunc(flags, func(f string) bool { return p.seen[f] })
}

// parseArgs splits args into operands and option values. A short option in
// the value table takes the rest of its word as the value, or the next word
// when the rest is empty, so "-fNR 9000:h:22" and "-d@file" both parse. A
// long option takes "=value", or the next word when it is in the value
// table. The values of other options are not kept.
func parseArgs(args []string, o options) parsedArgs {
	p := parsedArgs{values: map[string][]string{}, seen: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			p.addOperands(args[i+1:]...)
			return p
		case strings.HasPrefix(a, "--"):
			name, v, hasValue := strings.Cut(a, "=")
			name = o.longName(name)
			p.seen[name] = true
			if hasValue {
				p.addValue(name, v)
			} else if o.values[name] && i+1 < len(args) {
				i++
				p.addValue(name, args[i])
			}
			if o.guess && !o.known(name) {
				p.guess(v, args, i)
			}
		case strings.HasPrefix(a, "-") && a != "-":
			for j := 1; j < len(a); j++ {
				flag := "-" + a[j:j+1]
				p.seen[flag] = true
				if o.guess && !o.known(flag) {
					p.guess(a[j+1:], args, i)
				}
				if !o.values[flag] {
					continue
				}
				v := a[j+1:]
				if v == "" && i+1 < len(args) {
					i++
					v = args[i]
				}
				p.addValue(flag, v)
				break
			}
		default:
			p.addOperands(a)
		}
	}
	return p
}

// guess keeps the value of an unknown option as a guess. The value is the
// rest of the option word, or the next word when the rest is empty.
func (p *parsedArgs) guess(rest string, args []string, i int) {
	switch {
	case rest != "":
		p.guesses = append(p.guesses, rest)
	case i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
		p.guesses = append(p.guesses, args[i+1])
	}
}

func flagSet(flags ...string) map[string]bool {
	set := make(map[string]bool, len(flags))
	for _, f := range flags {
		set[f] = true
	}
	return set
}

// readCommands read every file operand.
var readCommands = map[string]options{
	"cat": {}, "tac": {}, "less": {}, "more": {}, "strings": valueOptions("-n", "-t"),
	"xxd": valueOptions("-c", "-g", "-l", "-s", "-o"), "od": valueOptions("-A", "-j", "-N", "-t", "-w"),
	"hexdump": valueOptions("-e", "-f", "-n", "-s"), "base64": valueOptions("-w"), "base32": valueOptions("-w"),
	"nl": valueOptions("-b", "-s", "-v", "-w"), "wc": {}, "uniq": valueOptions("-f", "-s", "-w"),
	"cut": valueOptions("-b", "-c", "-d", "-f"), "diff": {}, "cmp": {}, "md5sum": {},
	"sha1sum": {}, "sha256sum": {}, "sha512sum": {}, "zcat": {}, "bzcat": {}, "xzcat": {}, "bat": {},
	"head": valueOptions("-n", "-c", "--lines", "--bytes"),
	"tail": valueOptions("-n", "-c", "--lines", "--bytes", "-s", "--pid"),
}

// recursiveReads are the options of a read command that read every file in
// a directory operand, as in "diff -r DIR other".
var recursiveReads = map[string][]string{
	"diff": {"-r", "--recursive"},
	"zcat": {"-r", "--recursive"},
}

// shallowReads are the read commands that read the files directly in a
// directory operand without a recursive option.
var shallowReads = flagSet("diff")

// editCommands write every file operand. An option that writes a file,
// such as "vim -w", is not in the value table, so its file is an operand.
var editCommands = map[string]options{
	"vim": vimOptions, "vi": vimOptions, "nvim": vimOptions, "ex": vimOptions, "view": vimOptions,
	"nano": {},
}

var vimOptions = valueOptions("-c", "--cmd", "-S", "-u", "-U", "-t", "-T")

// copyFlags are the options that change where a copy writes.
type copyFlags struct {
	// dir options make the copy replace the destination itself.
	dir []string
	// recursive options copy a directory source with all its contents.
	recursive []string
	// relative options keep the whole source path under the destination.
	relative []string
	// slashContents is true when a source that ends in "/" copies the
	// contents of the directory, as in rsync. cp copies the directory.
	slashContents bool
}

// copyTool describes a local copy command.
type copyTool struct {
	opts          options
	flags         copyFlags
	readsSources  bool
	removeSources bool
	// linksHere is true for ln, which makes a link in the working
	// directory when it has one operand.
	linksHere bool
}

var noTargetDirectory = []string{"-T", "--no-target-directory"}

// nonReadCommands do not read the content of their operands. The walker
// does not guess reads for them.
var nonReadCommands = flagSet("ls", "stat", "du", "df", "echo", "printf", "test", "[", "[[",
	"mkdir", "touch", "which", "type", "basename", "dirname", "realpath", "readlink", "export",
	"unset", "set", "declare", "local", "alias", "true", "false", "sleep", "kill", "popd")

var (
	copyOptions = options{
		values: flagSet("-S", "--suffix", "-t", "--target-directory"),
		flags: flagSet("--recursive", "--archive", "--no-target-directory", "--no-dereference",
			"--parents"),
		abbrev: true,
	}
	installOptions = options{
		values: flagSet("-S", "--suffix", "-t", "--target-directory", "-g", "--group", "-m", "--mode",
			"-o", "--owner", "--strip-program"),
		flags:  flagSet("--no-target-directory", "--strip"),
		abbrev: true,
	}
	copyTools = map[string]copyTool{
		"cp": {opts: copyOptions, readsSources: true, flags: copyFlags{
			dir:       noTargetDirectory,
			recursive: []string{"-r", "-R", "-a", "--recursive", "--archive"},
			relative:  []string{"--parents"},
		}},
		"install": {opts: installOptions, readsSources: true, flags: copyFlags{dir: noTargetDirectory}},
		"mv":      {opts: copyOptions, readsSources: true, removeSources: true, flags: copyFlags{dir: noTargetDirectory}},
		"ln": {opts: copyOptions, linksHere: true, flags: copyFlags{
			dir: append([]string{"-n", "--no-dereference"}, noTargetDirectory...),
		}},
	}
	rsyncFlags = copyFlags{
		recursive:     []string{"-r", "-a", "--recursive", "--archive"},
		relative:      []string{"-R", "--relative"},
		slashContents: true,
	}
	scpFlags = copyFlags{recursive: []string{"-r"}}
)

var (
	grepOptions = valueOptions("-e", "-f", "-m", "-A", "-B", "-C", "-d", "-D", "-g", "-t", "-T",
		"--regexp", "--file", "--max-count", "--context", "--glob", "--type", "--type-not",
		"--include", "--exclude", "--exclude-dir")
	awkOptions = valueOptions("-F", "-v", "-f", "--file", "--assign", "--field-separator")
	jqOptions  = valueOptions("-f", "--from-file", "--arg", "--argjson", "--slurpfile",
		"--rawfile", "--indent")
	sortOptions = options{
		values: flagSet("-o", "-k", "-t", "-S", "-T", "--output", "--key", "--field-separator",
			"--buffer-size", "--temporary-directory", "--parallel", "--batch-size",
			"--compress-program", "--files0-from", "--random-source"),
		abbrev: true,
	}
	zipOptions = options{
		values: flagSet("-b", "-n", "-t", "-x", "-i", "-P", "-Z", "-s", "-O", "--temp-path",
			"--suffixes", "--from-date", "--before-date", "--exclude", "--include", "--password",
			"--compression-method", "--split-size", "--output-file", "--logfile-path",
			"--unzip-command", "--dot-size", "--unicode"),
		flags: flagSet("--move", "--must-match", "--more-help", "--recurse-paths",
			"--recurse-patterns", "--junk-paths", "--junk-sfx", "--quiet", "--verbose", "--update",
			"--freshen", "--delete", "--copy-entries", "--test", "--grow", "--encrypt", "--fix",
			"--fixfix", "--latest-time", "--log-append", "--log-info", "--no-extra",
			"--no-dir-entries", "--symlinks", "--archive-comment", "--entry-comments",
			"--adjust-sfx", "--ascii", "--to-crlf", "--from-crlf", "--paths", "--no-wild",
			"--wild-stop-dirs", "--help", "--license", "--version", "--filesync", "--fifo",
			"--difference-archive", "--preserve-case", "--regex", "--no-image", "--dos-names",
			"--archive-clear", "--archive-set", "--notes", "--display-bytes", "--display-counts",
			"--display-dots", "--display-globaldots", "--display-usize", "--display-volume",
			"--show-command", "--show-debug", "--show-files", "--show-options", "--show-unicode",
			"--split-bell", "--split-pause", "--split-verbose"),
		// Info-ZIP zip accepts a unique prefix of a long option.
		abbrev: true,
	}
	// zipShortOptions are the zip options with a short name of two letters
	// that take a value. getopt would read them as a group of flags.
	zipShortOptions = map[string]string{
		"-lf": "--logfile-path", "-TT": "--unzip-command", "-ds": "--dot-size",
		"-UN": "--unicode", "-tt": "--before-date",
	}
	rsyncOptions = valueOptions("-e", "--rsh", "-f", "--filter", "--exclude", "--include",
		"--files-from", "--exclude-from", "--include-from", "--password-file", "--log-file",
		"--port", "-T", "--temp-dir", "--backup-dir", "--suffix", "--chmod", "--chown", "-B",
		"--block-size", "--rsync-path", "-M", "--remote-option")
	scpOptions  = valueOptions("-P", "-i", "-o", "-F", "-l", "-S", "-c", "-J", "-D", "-X")
	sftpOptions = valueOptions("-B", "-b", "-c", "-D", "-F", "-i", "-J", "-l", "-o", "-P", "-R",
		"-S", "-s", "-X")
	sshOptions = valueOptions("-B", "-b", "-c", "-D", "-E", "-e", "-F", "-I", "-i", "-J", "-L",
		"-l", "-m", "-O", "-o", "-P", "-p", "-Q", "-R", "-S", "-W", "-w")
	telnetOptions = valueOptions("-X", "-b", "-e", "-k", "-l", "-n", "-S")
	ftpOptions    = valueOptions("-P", "-o", "-q", "-r", "-s", "-T", "-u", "-x", "-N")
	ncOptions     = valueOptions("-e", "-c", "-g", "-G", "-i", "-I", "-O", "-p", "-q", "-s", "-T",
		"-V", "-w", "-x", "-X")
	curlOptions = options{
		values: flagSet("-o", "--output", "-H", "--header", "-d", "--data", "--data-raw",
			"--data-binary", "--data-ascii", "--data-urlencode", "--json", "-X", "--request", "-u",
			"--user", "-A", "--user-agent", "-e", "--referer", "-b", "--cookie", "-c", "--cookie-jar",
			"-F", "--form", "--form-string", "-T", "--upload-file", "-x", "--proxy", "-m", "--max-time",
			"--connect-timeout", "-w", "--write-out", "-r", "--range", "-K", "--config", "--cert", "-E",
			"--key", "--cacert", "--resolve", "--retry", "-C", "--continue-at", "-U", "--proxy-user",
			"-t", "--telnet-option", "-P", "--ftp-port", "-z", "--time-cond", "-Y", "-y", "--url",
			"--connect-to", "--interface", "--dns-servers", "--output-dir", "-D", "--dump-header",
			"--trace", "--trace-ascii", "--stderr", "--hsts", "--etag-save", "--etag-compare",
			"--libcurl", "--alt-svc", "--ssl-sessions", "-Q", "--quote", "--limit-rate",
			"--max-filesize", "--max-redirs", "--retry-delay", "--retry-max-time", "--speed-limit",
			"--speed-time", "--cert-type", "--key-type", "--pass", "--ciphers", "--capath",
			"--crlfile", "--pinnedpubkey", "--noproxy", "--oauth2-bearer", "--unix-socket",
			"--abstract-unix-socket", "--socks4", "--socks4a", "--socks5", "--socks5-hostname",
			"--proxy-header", "--request-target", "--aws-sigv4", "--variable", "--netrc-file",
			"--proto", "--proto-redir", "--proto-default", "--doh-url", "--local-port",
			"--keepalive-time", "--expect100-timeout", "--tls-max", "--preproxy", "--mail-from",
			"--mail-rcpt", "--mail-auth", "--login-options", "--sasl-authzid", "--service-name",
			"--create-file-mode", "--parallel-max", "--happy-eyeballs-timeout-ms", "--ftp-method",
			"--ftp-account", "--tftp-blksize", "--url-query", "--proxy-cacert", "--proxy-cert",
			"--proxy-key", "--proxy-pass", "--proxy-ciphers", "--tls13-ciphers", "--curves",
			"--dns-interface", "--dns-ipv4-addr", "--dns-ipv6-addr", "--haproxy-clientip",
			"--trace-config"),
		flags: flagSet("--head", "--remote-name", "--remote-name-all", "--remote-header-name",
			"-0", "-1", "-2", "-3", "-4", "-6", "-a", "-B", "-f", "-g", "-G", "-h", "-i", "-I", "-j",
			"-J", "-k", "-l", "-L", "-M", "-n", "-N", "-O", "-p", "-q", "-R", "-s", "-S", "-v", "-V",
			"-Z", "-#", "-:", "--silent", "--show-error", "--location", "--location-trusted",
			"--include", "--insecure", "--proxy-insecure", "--fail", "--fail-with-body",
			"--fail-early", "--verbose", "--version", "--help", "--manual", "--globoff", "--get",
			"--buffer", "--append", "--use-ascii", "--list-only", "--netrc", "--netrc-optional",
			"--proxytunnel", "--remote-time", "--parallel", "--parallel-immediate", "--compressed",
			"--compressed-ssh", "--create-dirs", "--ftp-create-dirs", "--progress-bar",
			"--progress-meter", "--http1.0", "--http1.1", "--http2", "--http2-prior-knowledge",
			"--http3", "--http3-only", "--http0.9", "--ipv4", "--ipv6", "--tlsv1", "--tlsv1.0",
			"--tlsv1.1", "--tlsv1.2", "--tlsv1.3", "--sslv2", "--sslv3", "--ssl", "--ssl-reqd",
			"--raw", "--tr-encoding", "--path-as-is", "--tcp-nodelay", "--tcp-fastopen",
			"--anyauth", "--basic", "--digest", "--ntlm", "--negotiate", "--disable", "--xattr",
			"--retry-all-errors", "--retry-connrefused", "--junk-session-cookies", "--post301",
			"--post302", "--post303", "--crlf", "--ignore-content-length",
			"--suppress-connect-headers", "--doh-insecure", "--ca-native", "--cert-status",
			"--false-start", "--ftp-pasv", "--ftp-skip-pasv-ip", "--ftp-ssl-ccc", "--ftp-pret",
			"--styled-output", "--keepalive", "--sessionid", "--alpn", "--npn", "--epsv", "--eprt",
			"--clobber", "--remove-on-error", "--skip-existing", "--form-escape", "--mptcp",
			"--trace-time", "--trace-ids", "--show-headers", "--next"),
		// curl before 8.x accepts a unique prefix of a long option.
		abbrev: true,
		guess:  true,
	}
	wgetOptions = options{
		values: flagSet("-O", "--output-document", "-o", "--output-file", "-a",
			"--append-output", "-P", "--directory-prefix", "-U", "--user-agent", "-t", "--tries",
			"-T", "--timeout", "-e", "--execute", "-i", "--input-file", "-B", "--base", "-w", "--wait",
			"-Q", "--quota", "-l", "--level", "-A", "--accept", "-R", "--reject", "-D", "--domains",
			"-I", "-X", "-n", "--header", "--post-data", "--post-file", "--body-data", "--body-file",
			"--method", "--user", "--password", "--referer", "--load-cookies", "--save-cookies"),
		flags: flagSet("--recursive", "--mirror", "--page-requisites", "--content-disposition",
			"--trust-server-names", "--force-directories", "--spider"),
		abbrev: true,
	}
	gitOptions = valueOptions("-C", "-c", "--config-env", "--git-dir", "--work-tree", "--namespace",
		"--exec-path", "-b", "--branch", "-o", "--origin", "--depth", "-u", "--upload-pack",
		"--reference", "--template", "-j", "--jobs", "--filter", "--separate-git-dir", "--config",
		"--receive-pack", "--push-option", "-t", "--track", "-m", "--shallow-since",
		"--shallow-exclude")
	tarOptions = options{
		values: flagSet("-f", "--file", "-C", "--directory", "-T", "--files-from", "-X",
			"--exclude-from", "-b", "--blocking-factor", "-I", "--use-compress-program", "--exclude",
			"-N", "--newer", "--after-date", "--newer-mtime", "-V", "--label", "-L", "--tape-length",
			"-H", "--format", "-F", "--info-script", "--new-volume-script", "-g",
			"--listed-incremental", "-K", "--starting-file", "--owner", "--group", "--mode",
			"--mtime", "--transform", "--xform", "--to-command", "--suffix", "--record-size",
			"--volno-file", "--rsh-command", "--index-file", "--level", "--strip-components",
			"--exclude-tag", "--exclude-tag-all", "--exclude-tag-under", "--exclude-ignore",
			"--exclude-ignore-recursive", "--add-file", "--quoting-style", "--quote-chars",
			"--no-quote-chars", "--pax-option", "--sort", "--warning", "--checkpoint-action",
			"--owner-map", "--group-map", "--hole-detection"),
		flags: flagSet("--create", "--append", "--update", "--catenate", "--concatenate",
			"--delete", "--extract", "--get", "--list", "--diff", "--compare", "--to-stdout",
			"--remove-files", "--checkpoint", "--delay-directory-restore", "--absolute-names"),
		abbrev: true,
	}
)

// remoteShellOptions are the option tables of the tools whose first operand
// is the host.
var remoteShellOptions = map[string]options{
	"ssh": sshOptions, "sftp": sftpOptions, "telnet": telnetOptions, "ftp": ftpOptions,
}

// scriptTool records the file operands of a tool whose first operand is a
// program or pattern, such as grep or awk. The program comes from a file
// option or an expression option instead when either is set, and then every
// operand is a file.
func (w *walker) scriptTool(p parsedArgs, fileFlags, exprFlags []string, flat bool, cwds dirs) {
	w.addReads(p.value(fileFlags...), true, false, cwds)
	ops := p.operands
	if !p.has(fileFlags...) && !p.has(exprFlags...) && len(ops) > 0 {
		ops = ops[1:]
	}
	w.addReads(ops, flat, false, cwds)
}

// localCopy handles cp, mv, install, and ln. An ln with one operand makes
// a link with the base name of the operand in the working directory.
func (w *walker) localCopy(tool copyTool, args []string, cwds dirs) {
	p := parseArgs(args, tool.opts)
	dest, sources := copyOperands(p)
	if tool.linksHere && dest == "" && len(p.operands) == 1 {
		w.add(path.Base(p.operands[0]), destAccess(p, nil, tool.flags), cwds)
		return
	}
	if tool.readsSources {
		w.addReads(sources, !p.has(tool.flags.recursive...), false, cwds)
	}
	if tool.removeSources {
		w.addAll(sources, AccessRemove, cwds)
	}
	w.addDest(dest, sources, p, tool.flags, cwds)
}

// copyOperands splits the operands of a copy or move into the destination
// and the sources. The destination is the value of -t or the last operand.
func copyOperands(p parsedArgs) (string, []string) {
	if t := p.value("-t", "--target-directory"); len(t) > 0 {
		return t[len(t)-1], p.operands
	}
	if len(p.operands) < 2 {
		return "", nil
	}
	return p.operands[len(p.operands)-1], p.operands[:len(p.operands)-1]
}

// destAccess returns how a copy uses its destination. A copy onto the
// destination itself (-T), a copy of the contents of a directory, or a
// recursive copy of a source that can be a directory can write any path in
// the destination.
func destAccess(p parsedArgs, sources []string, flags copyFlags) Access {
	switch {
	case p.has(flags.dir...), slices.ContainsFunc(sources, copiesContents):
		return AccessWriteTree
	case p.has(flags.recursive...) && slices.ContainsFunc(sources, mayBeDirectory):
		return AccessWriteTree
	}
	return AccessWrite
}

func copiesContents(src string) bool {
	return strings.HasSuffix(src, "/") || strings.HasSuffix(src, "/.")
}

// mayBeDirectory reports whether a copy source can name a directory. A
// name with an extension, such as "notes.txt", counts as a file. A name
// without one, such as "nvim" or ".cc", can be a directory.
func mayBeDirectory(src string) bool {
	if _, p, remote := splitRemote(src); remote {
		src = p
	}
	name := path.Base(src)
	dot := strings.LastIndex(name, ".")
	return copiesContents(src) || dot <= 0 || dot == len(name)-1
}

// sort reads the operands and writes the -o file.
func (w *walker) sort(p parsedArgs, cwds dirs) {
	w.addAll(p.operands, AccessRead, cwds)
	w.addAll(p.value("-o", "--output"), AccessWrite, cwds)
}

// compressor describes gzip, bzip2, and xz.
type compressor struct {
	opts       options
	decompress bool
	// suffix is the suffix of a compressed file.
	suffix string
	// known are the suffixes that a decompress removes.
	known []string
	// tar are the suffixes that a decompress replaces with ".tar".
	tar []string
	// fallback is the suffix that a decompress adds to a file with an
	// unknown suffix. An empty fallback means the tool refuses the file.
	fallback string
}

func (c compressor) unpacks() compressor {
	c.decompress = true
	return c
}

var (
	gzipTool = compressor{
		opts: options{
			values: flagSet("-S", "--suffix"),
			flags: flagSet("--stdout", "--to-stdout", "--keep", "--decompress", "--uncompress",
				"--recursive", "--list", "--test", "--name", "--no-name"),
			abbrev: true,
		},
		suffix: ".gz",
		known:  []string{".gz", "-gz", ".z", "-z", "_z"},
		tar:    []string{".tgz", ".taz"},
	}
	bzip2Tool = compressor{
		suffix:   ".bz2",
		known:    []string{".bz2", ".bz"},
		tar:      []string{".tbz2", ".tbz"},
		fallback: ".out",
	}
	xzTool = compressor{
		opts: options{
			values: flagSet("-S", "--suffix", "-T", "--threads", "-F", "--format", "-C", "--check",
				"-M", "--memlimit", "--memory", "--memlimit-compress", "--memlimit-decompress",
				"--block-size", "--block-list", "--flush-timeout", "--filters"),
			flags: flagSet("--stdout", "--to-stdout", "--keep", "--decompress", "--uncompress",
				"--compress", "--list", "--test", "--files", "--files0", "--force", "--quiet",
				"--verbose", "--robot", "--extreme", "--fast", "--best", "--single-stream",
				"--no-sparse", "--ignore-check", "--no-adjust", "--info-memory", "--help",
				"--long-help", "--version"),
			abbrev: true,
		},
		suffix: ".xz",
		known:  []string{".xz", ".lzma"},
		tar:    []string{".txz", ".tlz"},
	}
	compressors = map[string]compressor{
		"gzip": gzipTool, "gunzip": gzipTool.unpacks(),
		"bzip2": bzip2Tool, "bunzip2": bzip2Tool.unpacks(),
		"xz": xzTool, "unxz": xzTool.unpacks(),
	}
)

// compress replaces each operand with the compressed or the decompressed
// file. With -c, -l, or -t it only reads the operands. With -k it keeps the
// operands. A recursive run changes files inside the operands. gzip -N
// takes the name of the output from the file header, so it can write any
// name in the directory of the operand. xz --files reads the names from a
// file, so the walker records only that file as a read.
func (w *walker) compress(c compressor, args []string, cwds dirs) {
	p := parseArgs(args, c.opts)
	if p.has("-c", "--stdout", "--to-stdout", "-l", "--list", "-t", "--test") {
		w.addAll(p.operands, AccessRead, cwds)
		return
	}
	w.addAll(p.value("--files", "--files0"), AccessRead, cwds)
	if p.has("-r", "--recursive") {
		w.addAll(p.operands, AccessRemove, cwds)
		return
	}
	decompress := c.decompress || p.has("-d", "--decompress", "--uncompress")
	headerName := decompress && p.has("-N", "--name")
	for _, op := range p.operands {
		if p.has("-k", "--keep") {
			w.add(op, AccessRead, cwds)
		} else {
			w.add(op, AccessRemove, cwds)
		}
		if headerName {
			w.add(path.Dir(op), AccessWriteTree, cwds)
		}
		w.add(c.output(op, p.value("-S", "--suffix"), decompress), AccessWrite, cwds)
	}
}

// output returns the file that the tool writes for file. It returns an
// empty string when the tool refuses the file.
func (c compressor) output(file string, suffixes []string, decompress bool) string {
	if !decompress {
		if len(suffixes) > 0 {
			return file + suffixes[len(suffixes)-1]
		}
		return file + c.suffix
	}
	lower := strings.ToLower(file)
	for _, s := range slices.Concat(suffixes, c.known) {
		if strings.HasSuffix(lower, strings.ToLower(s)) && len(file) > len(s) {
			return file[:len(file)-len(s)]
		}
	}
	for _, s := range c.tar {
		if strings.HasSuffix(lower, s) && len(file) > len(s) {
			return file[:len(file)-len(s)] + ".tar"
		}
	}
	if c.fallback != "" {
		return file + c.fallback
	}
	return ""
}

// zip writes the archive, the first operand, and reads the other operands.
// With -m it also removes them. -O writes the new archive to another file.
func (w *walker) zip(args []string, cwds dirs) {
	args = slices.Clone(args)
	for i, a := range args {
		if long, ok := zipShortOptions[a]; ok {
			args[i] = long
		}
	}
	p := parseArgs(args, zipOptions)
	w.addOutputs(p.value("-O", "--output-file", "--logfile-path"), cwds)
	if len(p.operands) == 0 {
		return
	}
	w.addOutputs(p.operands[:1], cwds)
	files := p.operands[1:]
	w.addAll(files, AccessRead, cwds)
	if p.has("-m", "--move") {
		w.addAll(files, AccessRemove, cwds)
	}
}

// sevenZip records the archive and the files of a 7z command. The first
// operand is the command. a, u, d, and rn write the archive. x extracts
// with paths into the -o directory or the working directory. e extracts
// without paths. The walker does not record the absolute member paths of
// -spf or the targets of an unknown command, because it cannot know them.
func (w *walker) sevenZip(args []string, cwds dirs) {
	var ops, outDirs []string
	var toStdout, deletes bool
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-o"):
			outDirs = append(outDirs, a[2:])
		case a == "-so":
			toStdout = true
		case a == "-sdel":
			deletes = true
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			ops = append(ops, a)
		}
	}
	if len(ops) == 0 {
		return
	}
	archive, files := ops[1:min(2, len(ops))], ops[min(2, len(ops)):]
	switch strings.ToLower(ops[0]) {
	case "a", "u":
		w.addOutputs(archive, cwds)
		w.addAll(files, AccessRead, cwds)
		if deletes {
			w.addAll(files, AccessRemove, cwds)
		}
	case "d", "rn":
		w.addOutputs(archive, cwds)
	case "x", "e":
		w.addAll(archive, AccessRead, cwds)
		if toStdout {
			return
		}
		w.extractInto(outDirs, cwds)
	case "l", "t", "h", "i", "b":
		w.addAll(ops[1:], AccessRead, cwds)
	}
}

// unzip reads the archive and extracts into the -d directory or the
// working directory. -l, -t, -v, -z, -Z, -c, and -p only read. -j drops
// the paths of the members. The walker does not record the paths that -:
// keeps, because it cannot know them.
func (w *walker) unzip(args []string, cwds dirs) {
	var ops, outDirs []string
	var readOnly bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			ops = append(ops, a)
			continue
		}
		flags, dir, hasDir := strings.Cut(a[1:], "d")
		readOnly = readOnly || strings.ContainsAny(flags, "ltvzZcp")
		if !hasDir {
			continue
		}
		if dir == "" && i+1 < len(args) {
			i++
			dir = args[i]
		}
		outDirs = append(outDirs, dir)
	}
	w.addAll(ops[:min(1, len(ops))], AccessRead, cwds)
	if !readOnly {
		w.extractInto(outDirs, cwds)
	}
}

// extractInto records the directories an extract writes into as tree
// writes. It uses the working directory when dirs is empty. An extract
// without member paths writes only names in the directory, but the names
// are not known, so a tree write is the closest target.
func (w *walker) extractInto(dirs []string, cwds dirs) {
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	w.addAll(dirs, AccessWriteTree, cwds)
}

// tar records the archive and the members. A create, append, update,
// concatenate, or delete writes the archive. An extract writes into each -C
// directory, or into the working directory. Each -C applies to the members
// after it, relative to the previous -C. The walker does not record the
// absolute member names of -P, because it cannot know them. Other modes read
// the archive.
func (w *walker) tar(args []string, cwds dirs) {
	p := parseArgs(tarOldStyle(args), tarOptions)
	adds := p.has("-c", "--create", "-r", "--append", "-u", "--update")
	concatenates := p.has("-A", "--catenate", "--concatenate")
	archive := AccessRead
	if adds || concatenates || p.has("--delete") {
		archive = AccessWrite
	}
	for _, f := range p.value("-f", "--file") {
		if f != "-" {
			w.add(f, archive, cwds)
		}
	}
	w.addAll(p.value("-T", "--files-from", "-X", "--exclude-from"), AccessRead, cwds)
	members := cwds
	extracts := p.has("-x", "--extract", "--get") && !p.has("-O", "--to-stdout")
	if extracts && !p.has("-C", "--directory") {
		w.add(".", AccessWriteTree, cwds)
	}
	for _, a := range p.seq {
		switch {
		case a.flag == "-C" || a.flag == "--directory":
			next := w.cd([]string{a.value}, members)
			switch {
			case !extracts:
			case strings.ContainsAny(a.value, "*?["):
				w.add(a.value, AccessWriteTree, members)
			default:
				w.add(".", AccessWriteTree, next)
			}
			members = next
		case a.flag == "" && (adds || concatenates):
			w.add(a.value, AccessRead, members)
			if adds && p.has("--remove-files") {
				w.add(a.value, AccessRemove, members)
			}
		}
	}
}

// tarOldStyle rewrites an old-style first word, as in "tar czf out.tgz
// dir", as separate options. GNU tar gives each letter that takes a value
// the next word, in order, so "tar xfC a.tar dir" reads a.tar with -C dir.
func tarOldStyle(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || !isLetters(args[0]) {
		return args
	}
	rest := args[1:]
	out := make([]string, 0, len(args)+len(args[0]))
	for _, c := range args[0] {
		flag := "-" + string(c)
		out = append(out, flag)
		if tarOptions.values[flag] && len(rest) > 0 {
			out = append(out, rest[0])
			rest = rest[1:]
		}
	}
	return append(out, rest...)
}

var sqliteOptions = valueOptions("-cmd", "-init", "-separator", "-newline", "-nullvalue",
	"-vfs", "-maxsize", "-mmap", "-pagecache", "-lookaside", "-heap")

// sqlite records the first operand of sqlite3 as a read of the database.
// The database can follow an option that sqlite3 parses with a single dash
// and that the table does not know, so each later operand is a guessed read.
// It also records the -init file, and each file that the SQL of an operand
// or of -cmd opens.
func (w *walker) sqlite(args []string, cwds dirs) {
	database := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		flag := "-" + strings.TrimLeft(a, "-")
		switch {
		case strings.HasPrefix(a, "-") && sqliteOptions.values[flag]:
			if i+1 < len(args) {
				i++
				w.sqliteValue(flag, args[i], cwds)
			}
		case strings.HasPrefix(a, "-"):
		case database:
			database = false
			w.add(filePath(a), AccessRead, cwds)
		default:
			w.guessReads([]string{filePath(a)}, cwds)
			w.sqliteText(a, cwds)
		}
	}
}

func (w *walker) sqliteValue(flag, value string, cwds dirs) {
	switch flag {
	case "-init":
		w.add(value, AccessRead, cwds)
	case "-cmd":
		w.sqliteText(value, cwds)
	}
}

var sqliteToken = regexp.MustCompile(`'(?:[^']|'')*'|"[^"]*"|[^\s;]+`)

// sqliteFileCommands are the dot commands of sqlite3 that read a file
// operand. sqliteShellCommands run the rest of the line as a shell command.
var (
	sqliteFileCommands  = []string{"open", "read", "import", "restore", "load"}
	sqliteShellCommands = []string{"shell", "system"}
)

// isSqliteDotCommand reports whether a word is one of the dot commands.
// sqlite3 accepts a short prefix of a dot command name.
func isSqliteDotCommand(word string, commands []string) bool {
	name, ok := strings.CutPrefix(word, ".")
	if !ok || len(name) < 2 {
		return false
	}
	return slices.ContainsFunc(commands, func(c string) bool { return strings.HasPrefix(c, name) })
}

// sqliteText records the files that SQL text or a dot command opens with
// .open, .read, .import, .restore, .load, ATTACH, readfile(), fsdir(), or
// load_extension(). It also analyzes the command of .shell and .system.
func (w *walker) sqliteText(text string, cwds dirs) {
	var sql []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		tokens := sqliteToken.FindAllString(line, -1)
		switch {
		case len(tokens) == 0:
		case isSqliteDotCommand(tokens[0], sqliteShellCommands):
			w.nested(strings.TrimPrefix(line, tokens[0]), cwds)
		case isSqliteDotCommand(tokens[0], sqliteFileCommands):
			for _, tok := range tokens[1:] {
				if !strings.HasPrefix(tok, "-") {
					w.add(filePath(sqliteUnquote(tok)), AccessRead, cwds)
				}
			}
		case !strings.HasPrefix(line, "."):
			sql = append(sql, line)
		}
	}
	w.addAll(sqlFiles(sqlTokens(strings.Join(sql, "\n"))), AccessRead, cwds)
}

// sqlToken is one SQL token. A string literal keeps its quotes. A quoted
// name is bare, so "readfile" and readfile are the same word.
type sqlToken struct {
	text    string
	literal bool
}

// sqlTokens splits SQL into tokens. It drops white space and comments.
func sqlTokens(sql string) []sqlToken {
	var out []sqlToken
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(sql[i:], "--"):
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				return out
			}
			i += end
		case strings.HasPrefix(sql[i:], "/*"):
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 4
		case c == '\'':
			end := quoteEnd(sql, i, '\'')
			out = append(out, sqlToken{text: sql[i:end], literal: true})
			i = end
		case c == '"' || c == '`' || c == '[':
			closer := map[byte]byte{'"': '"', '`': '`', '[': ']'}[c]
			end := quoteEnd(sql, i, closer)
			out = append(out, sqlToken{text: strings.Trim(sql[i:end], string(c)+string(closer))})
			i = end
		case strings.IndexByte("(),;", c) >= 0:
			out = append(out, sqlToken{text: sql[i : i+1]})
			i++
		default:
			end := i + 1
			for end < len(sql) && strings.IndexByte(" \t\n\r(),;'\"`[", sql[end]) < 0 && !strings.HasPrefix(sql[end:], "--") && !strings.HasPrefix(sql[end:], "/*") {
				end++
			}
			out = append(out, sqlToken{text: sql[i:end]})
			i = end
		}
	}
	return out
}

// quoteEnd returns the index after the quote that closes the quote at
// sql[start]. A doubled quote does not close it. An open quote runs to the
// end.
func quoteEnd(sql string, start int, closer byte) int {
	for i := start + 1; i < len(sql); i++ {
		if sql[i] != closer {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == closer && closer != ']' {
			i++
			continue
		}
		return i + 1
	}
	return len(sql)
}

// sqlFileFunctions are the SQL functions that open the file of their first
// argument.
var sqlFileFunctions = []string{"readfile", "fsdir", "load_extension"}

// sqlFiles returns the files that ATTACH at the start of a statement, or a
// call of a file function, names with one string literal. A file that an
// expression names is not known, so it adds nothing.
func sqlFiles(tokens []sqlToken) []string {
	var out []string
	start := true
	for i, tok := range tokens {
		word := strings.ToLower(tok.text)
		switch {
		case tok.literal:
		case start && word == "attach":
			arg := i + 1
			if arg < len(tokens) && !tokens[arg].literal && strings.EqualFold(tokens[arg].text, "database") {
				arg++
			}
			out = appendLiteralArg(out, tokens, arg, "as")
		case slices.Contains(sqlFileFunctions, word) && i+1 < len(tokens) && tokens[i+1].text == "(":
			out = appendLiteralArg(out, tokens, i+2, ",", ")")
		}
		start = !tok.literal && tok.text == ";"
	}
	return out
}

// appendLiteralArg appends the string literal at tokens[i] when the token
// after it is one of ends.
func appendLiteralArg(out []string, tokens []sqlToken, i int, ends ...string) []string {
	if i+1 >= len(tokens) || !tokens[i].literal || tokens[i+1].literal {
		return out
	}
	if !slices.ContainsFunc(ends, func(e string) bool { return strings.EqualFold(tokens[i+1].text, e) }) {
		return out
	}
	return append(out, filePath(sqliteUnquote(tokens[i].text)))
}

func sqliteUnquote(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		q := string(s[0])
		return strings.ReplaceAll(s[1:len(s)-1], q+q, q)
	}
	return s
}

// filePath returns the local path of a "file:" URI, as SQLite and curl read
// it: the percent-decoded path without the query, the fragment, and a
// "localhost" or empty host. It returns any other value as it is. The scheme
// is not case sensitive.
func filePath(value string) string {
	if len(value) < 5 || !strings.EqualFold(value[:5], "file:") {
		return value
	}
	u, err := url.Parse(value)
	if err != nil {
		p, _, _ := strings.Cut(value[5:], "?")
		return p
	}
	if u.Opaque == "" {
		return u.Path
	}
	if p, err := url.PathUnescape(u.Opaque); err == nil {
		return p
	}
	return u.Opaque
}

func isLetters(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
	}) < 0
}

// curl records the hosts, the files of "file://" URLs, the uploaded files,
// and the output files. -o and -O write into the --output-dir directory
// when it is set. The value of an option that the table does not know is a
// guessed write target.
func (w *walker) curl(p parsedArgs, cwds dirs) {
	urls := slices.Concat(p.operands, p.value("--url"))
	for _, u := range urls {
		if f := filePath(u); f != u {
			w.add(f, AccessRead, cwds)
			continue
		}
		w.addNetworkHost(u)
	}
	if p.has("-K", "--config", "--resolve") {
		// A config file can hold the URLs, and --resolve sends a host
		// name to any address.
		w.addHost(UnknownHost)
	}
	for _, v := range p.value("-x", "--proxy", "--preproxy", "--socks4", "--socks4a", "--socks5",
		"--socks5-hostname", "--doh-url") {
		w.addNetworkHost(v)
	}
	for _, v := range p.value("--connect-to") {
		// HOST1:PORT1:HOST2:PORT2 sends HOST1 to HOST2. An empty HOST2
		// keeps HOST1.
		parts := strings.Split(v, ":")
		if len(parts) != 4 || strings.Contains(v, "[") {
			w.addHost(UnknownHost)
		} else if parts[2] != "" {
			w.addNetworkHost(parts[2])
		}
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
	for _, f := range p.value("-T", "--upload-file", "-K", "--config", "--etag-compare") {
		w.addUpload(f, cwds)
	}
	w.addOutputs(p.value("-c", "--cookie-jar", "-D", "--dump-header", "--trace", "--trace-ascii",
		"--stderr", "--hsts", "--etag-save", "--libcurl", "--alt-svc", "--ssl-sessions"), cwds)
	w.addOutputs(p.guesses, cwds)
	for _, format := range p.value("-w", "--write-out") {
		w.writeOutFiles(format, cwds)
	}

	dir := "."
	if dirs := p.value("--output-dir"); len(dirs) > 0 {
		dir = dirs[len(dirs)-1]
	}
	for _, o := range p.value("-o", "--output") {
		switch {
		case o == "-":
		case dir == ".":
			w.add(o, AccessWrite, cwds)
		default:
			w.add(dir+"/"+o, AccessWrite, cwds)
		}
	}
	if !p.has("-O", "--remote-name", "--remote-name-all") {
		return
	}
	if p.has("-J", "--remote-header-name") {
		w.add(dir, AccessWriteTree, cwds)
		return
	}
	for _, u := range urls {
		if name := remoteFileName(u); name != "" {
			w.add(dir+"/"+name, AccessWrite, cwds)
		}
	}
}

// writeOutFiles records the files of "%output{FILE}" in a curl --write-out
// format. "%output{>>FILE}" appends. A format read from a file with "@" is a
// read of that file. The walker does not read the file, so it does not know
// the files that the format names.
func (w *walker) writeOutFiles(format string, cwds dirs) {
	if file, ok := strings.CutPrefix(format, "@"); ok {
		w.addUpload(file, cwds)
		return
	}
	const marker = "%output{"
	for {
		_, after, ok := strings.Cut(format, marker)
		if !ok {
			return
		}
		file, rest, _ := strings.Cut(after, "}")
		w.add(strings.TrimPrefix(file, ">>"), AccessWrite, cwds)
		format = rest
	}
}

// wgetrcCommands maps the "-e" commands that set an output path or a
// download mode to their options. wgetrc ignores case, "_", and "-" in a
// command name.
var wgetrcCommands = map[string]string{
	"outputdocument": "--output-document", "dirprefix": "--directory-prefix",
	"logfile": "--output-file", "savecookies": "--save-cookies", "input": "--input-file",
	"recursive": "--recursive", "mirror": "--mirror", "pagerequisites": "--page-requisites",
	"contentdisposition": "--content-disposition", "trustservernames": "--trust-server-names",
	"dirstruct": "--force-directories",
}

// withWgetrc adds the options that the "-e" commands of p set.
func withWgetrc(p parsedArgs) parsedArgs {
	for _, cmd := range p.value("-e", "--execute") {
		name, v, ok := strings.Cut(cmd, "=")
		if !ok {
			continue
		}
		name = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(name)))
		opt, known := wgetrcCommands[name]
		v = strings.TrimSpace(v)
		switch {
		case !known:
		case wgetOptions.values[opt]:
			p.seen[opt] = true
			p.addValue(opt, v)
		case !strings.EqualFold(v, "off"):
			p.seen[opt] = true
		}
	}
	return p
}

// wget records the hosts, the uploaded files, and the output files.
// Without -O, wget saves each URL under its file name in the -P directory
// or the working directory. A download that takes names from the server or
// an input file can write any name in that directory. A recursive
// download can write any path in it.
func (w *walker) wget(p parsedArgs, cwds dirs) {
	p = withWgetrc(p)
	for _, u := range p.operands {
		w.addNetworkHost(u)
	}
	if p.has("-i", "--input-file") {
		// An input file holds the URLs.
		w.addHost(UnknownHost)
	}
	for _, e := range p.value("-e", "--execute") {
		if strings.Contains(strings.ToLower(e), "proxy") {
			w.addHost(UnknownHost)
		}
	}
	w.addAll(p.value("--post-file", "--body-file", "-i", "--input-file"), AccessRead, cwds)
	w.addOutputs(p.value("-O", "--output-document", "-o", "--output-file", "-a", "--append-output",
		"--save-cookies"), cwds)
	if p.has("-O", "--output-document", "--spider") {
		return
	}
	dirs := p.value("-P", "--directory-prefix")
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	switch {
	case p.has("-r", "--recursive", "-m", "--mirror", "-p", "--page-requisites", "-x",
		"--force-directories"):
		w.addAll(dirs, AccessWriteTree, cwds)
	case p.has("--content-disposition", "--trust-server-names", "-i", "--input-file"):
		w.addAll(dirs, AccessWriteTree, cwds)
	default:
		for _, dir := range dirs {
			for _, u := range p.operands {
				name := remoteFileName(u)
				if name == "" {
					name = "index.html"
				}
				w.add(dir+"/"+name, AccessWrite, cwds)
			}
		}
	}
}

// remoteFileName returns the last path segment of a URL, or an empty string
// when the path has none.
func remoteFileName(u string) string {
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "/" || name == "." {
		return ""
	}
	return name
}

// addOutputs records the files a command writes. The value "-" is stdout.
func (w *walker) addOutputs(files []string, cwds dirs) {
	for _, f := range files {
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

// sshRoute records the hosts that ssh, scp or sftp connects through: the
// jump hosts of -J and ProxyJump, the host of the HostName option, and the
// targets of the forward options. A config file, a ProxyCommand, a
// LocalCommand, or a dynamic forward can connect to any host.
func (w *walker) sshRoute(p parsedArgs) {
	if p.has("-F") {
		w.addHost(UnknownHost)
	}
	jumps := p.value("-J")
	for _, o := range p.value("-o") {
		key, value, _ := strings.Cut(strings.TrimSpace(o), "=")
		if k, v, ok := strings.Cut(key, " "); ok {
			key, value = k, v
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "proxyjump":
			jumps = append(jumps, value)
		case "hostname":
			w.addNetworkHost(value)
		case "localforward", "remoteforward":
			w.forwardOption(value)
		case "proxycommand", "localcommand", "dynamicforward", "include", "":
			w.addHost(UnknownHost)
		}
	}
	for _, j := range jumps {
		for _, h := range strings.Split(j, ",") {
			w.addNetworkHost(strings.TrimSpace(h))
		}
	}
}

// sshForwards records the targets of the ssh options -W, -L, and -R. A
// dynamic forward, -D or -R with only a port, can connect to any host.
func (w *walker) sshForwards(p parsedArgs) {
	for _, v := range p.value("-W") {
		w.addNetworkHost(v)
	}
	for _, v := range p.value("-L") {
		w.forwardHost(v, false)
	}
	for _, v := range p.value("-R") {
		w.forwardHost(v, true)
	}
	if p.has("-D") {
		w.addHost(UnknownHost)
	}
}

// forwardHost records the target host of a forward spec such as
// "[bind:]port:host:hostport". A spec with two fields forwards to a socket
// path, or, for a remote forward, is a dynamic forward.
func (w *walker) forwardHost(spec string, remote bool) {
	fields := strings.Split(spec, ":")
	switch {
	case strings.Contains(spec, "["):
		w.addHost(UnknownHost)
	case len(fields) >= 3:
		w.addNetworkHost(fields[len(fields)-2])
	case remote && (len(fields) == 1 || !strings.HasPrefix(fields[1], "/")):
		w.addHost(UnknownHost)
	}
}

// forwardOption records the target of a LocalForward or RemoteForward
// value, "[bind:]port host:hostport". A value with one field is a dynamic
// forward. A target that starts with "/" is a socket path.
func (w *walker) forwardOption(value string) {
	fields := strings.Fields(value)
	switch {
	case len(fields) < 2:
		w.addHost(UnknownHost)
	case !strings.HasPrefix(fields[len(fields)-1], "/"):
		w.addNetworkHost(fields[len(fields)-1])
	}
}

// sshCommand records the hosts of an ssh command line, such as the value
// of GIT_SSH_COMMAND or core.sshCommand. git adds the host of the remote
// after it. A value that does not parse, or a program other than ssh, can
// connect to any host.
func (w *walker) sshCommand(value string) {
	args, ok := w.commandWords(value)
	if !ok || len(args) == 0 || path.Base(args[0]) != "ssh" {
		w.addHost(UnknownHost)
		return
	}
	p := parseArgs(args[1:], sshOptions)
	w.sshRoute(p)
	w.sshForwards(p)
	w.remoteShell(p)
}

// commandWords parses one simple command and returns its words. It reports
// false when the text is not one simple command, or when a word does not
// resolve.
func (w *walker) commandWords(src string) ([]string, bool) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil || len(f.Stmts) != 1 || len(f.Stmts[0].Redirs) > 0 {
		return nil, false
	}
	call, ok := f.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 {
		return nil, false
	}
	words := make([]string, 0, len(call.Args))
	for _, a := range call.Args {
		v, ok := w.word(a, nil)
		if !ok {
			return nil, false
		}
		words = append(words, v)
	}
	return words, true
}

func (w *walker) remoteShell(p parsedArgs) {
	if len(p.operands) > 0 {
		w.addNetworkHost(p.operands[0])
	}
}

// lookupTool describes the options and operands of a DNS or reachability
// tool. A value flag that a tool does not have would hide the host that
// follows it, so each table lists only the options that take a value on
// every common build of the tool.
type lookupTool struct {
	opts options
	// hostFlags take a host, or a list of hosts split by commas.
	hostFlags []string
	// fileFlags read the names from a file.
	fileFlags []string
	// skip reports an operand that names no host.
	skip func(op string) bool
	// firstOnly is true when only the first operand names a host.
	firstOnly bool
	// stdin is true when the tool reads the names from stdin when it has
	// no operand or the operand "-".
	stdin bool
}

var lookupTools = func() map[string]lookupTool {
	ping := lookupTool{opts: valueOptions("-c", "-e", "-F", "-i", "-I", "-l", "-m", "-M", "-N",
		"-p", "-s", "-S", "-t", "-T", "-w", "-W")}
	return map[string]lookupTool{
		"dig": {
			opts:      valueOptions("-b", "-c", "-f", "-k", "-p", "-q", "-t", "-x", "-y"),
			hostFlags: []string{"-q", "-x"},
			fileFlags: []string{"-f"},
			skip:      isDigArg,
		},
		"host":     {opts: valueOptions("-c", "-m", "-N", "-p", "-R", "-t", "-W")},
		"nslookup": {skip: func(op string) bool { return op == "-" }, stdin: true},
		"ping":     ping,
		"ping6":    ping,
		"traceroute": {
			opts: valueOptions("-f", "-g", "-i", "-l", "-m", "-M", "-N", "-O", "-p", "-P", "-q", "-s",
				"-t", "-w", "-z"),
			hostFlags: []string{"-g"},
			firstOnly: true,
		},
		"tracepath": {opts: valueOptions("-l", "-m", "-p"), firstOnly: true},
		"whois": {
			opts:      valueOptions("-h", "--host", "-p", "--port", "-g", "-i", "-q", "-s", "-t", "-T", "-v"),
			hostFlags: []string{"-h", "--host"},
		},
	}
}()

// dnsKeywords are the record types and classes that dig takes as operands.
var dnsKeywords = flagSet("a", "aaaa", "afsdb", "any", "axfr", "caa", "cdnskey", "cds", "cert",
	"ch", "chaos", "cname", "csync", "dname", "dnskey", "ds", "hinfo", "hs", "hesiod", "https",
	"in", "ixfr", "key", "loc", "mx", "naptr", "none", "ns", "nsec", "nsec3", "nsec3param",
	"openpgpkey", "ptr", "rp", "rrsig", "sig", "smimea", "soa", "spf", "srv", "sshfp", "svcb",
	"tlsa", "txt", "uri", "zonemd")

// isDigArg reports a dig operand that is a query option such as "+short", a
// record type, or a class.
func isDigArg(op string) bool {
	op = strings.ToLower(op)
	if strings.HasPrefix(op, "+") || dnsKeywords[op] {
		return true
	}
	for _, prefix := range []string{"type", "class"} {
		if n, ok := strings.CutPrefix(op, prefix); ok && n != "" && strings.Trim(n, "0123456789") == "" {
			return true
		}
	}
	return false
}

// lookup records the hosts of a DNS or reachability tool such as dig or
// ping. An operand that starts with "@" names the DNS server.
func (w *walker) lookup(tool lookupTool, args []string) {
	p := parseArgs(args, tool.opts)
	if p.has(tool.fileFlags...) || tool.stdin && (len(p.operands) == 0 || p.operands[0] == "-") {
		w.addHost(UnknownHost)
	}
	for _, v := range p.value(tool.hostFlags...) {
		for _, h := range strings.Split(v, ",") {
			w.addNetworkHost(h)
		}
	}
	names := 0
	for _, op := range p.operands {
		if tool.skip != nil && tool.skip(op) {
			continue
		}
		if server, ok := strings.CutPrefix(op, "@"); ok {
			w.addNetworkHost(server)
			continue
		}
		if tool.firstOnly && names > 0 {
			return
		}
		names++
		w.addNetworkHost(op)
	}
}

// netcat records the host a client connects to. A listener has no host.
func (w *walker) netcat(args []string) {
	p := parseArgs(args, ncOptions)
	if !p.has("-l", "--listen") {
		w.remoteShell(p)
	}
}

// remoteCopy handles rsync and scp. The last operand is the destination.
// Each remote operand gives a host. A local source is a read, and a local
// destination is a write of the destination and of each source name in it.
// rsync --remove-source-files removes each local source.
func (w *walker) remoteCopy(p parsedArgs, flags copyFlags, cwds dirs) {
	w.addAll(p.value("--files-from", "--exclude-from", "--include-from", "--password-file"), AccessRead, cwds)
	if len(p.operands) < 2 {
		return
	}
	dest := p.operands[len(p.operands)-1]
	sources := p.operands[:len(p.operands)-1]
	removesSources := p.has("--remove-source-files", "--remove-sent-files")
	for _, src := range sources {
		if host, _, remote := splitRemote(src); remote {
			w.addHost(host)
			continue
		}
		w.add(src, AccessRead, cwds)
		if removesSources {
			w.add(src, AccessRemove, cwds)
		}
	}
	if host, _, remote := splitRemote(dest); remote {
		w.addHost(host)
		return
	}
	w.addDest(dest, sources, p, flags, cwds)
}

// gitRemoteArg gives, for each git subcommand that names a remote, the
// position of the remote among the subcommand's operands.
var gitRemoteArg = map[string]int{
	"clone": 1, "fetch": 1, "pull": 1, "push": 1, "ls-remote": 1,
}

// git records the host of the remote that a git command names. git can
// read any file under its working tree, so each -C directory, --git-dir,
// and --work-tree is a read of that tree. Each word that can name a file is
// a guessed read relative to the last -C directory.
func (w *walker) git(args []string, cwds dirs) {
	p := parseArgs(args, gitOptions)
	global := true
	for _, a := range p.seq {
		switch a.flag {
		case "":
			global = false
		case "-c", "--config-env":
			if global {
				key, value, _ := strings.Cut(a.value, "=")
				w.gitConfig(key, value, a.flag == "-c")
			}
		case "-C":
			cwds = w.cd([]string{"--", a.value}, cwds)
			w.add(".", AccessRead, cwds)
		case "--git-dir", "--work-tree":
			w.add(a.value, AccessRead, cwds)
		}
	}
	w.guessReads(guessWords(args), cwds)
	ops := p.operands
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
	if !ok {
		return
	}
	if i >= len(ops) {
		// "git push" with no remote uses a configured remote.
		if _, fetches := gitRemoteArg[ops[0]]; fetches && ops[0] != "clone" {
			w.addHost(UnknownHost)
		}
		return
	}
	if host, _, remote := splitRemote(ops[i]); remote {
		w.addHost(host)
		return
	}
	if !isLocalPath(ops[i]) {
		// A named remote such as "origin" points at a host in the git
		// config.
		w.addHost(UnknownHost)
	}
}

// gitConfig records the hosts of a "git -c key=value" setting that sends
// git through another host: a proxy, an ssh command, or a URL rewrite. The
// value of --config-env comes from a variable, so it is not known. Such a
// value, an include, or a proxy command can name any host.
func (w *walker) gitConfig(key, value string, known bool) {
	lower := strings.ToLower(key)
	section, _, _ := strings.Cut(lower, ".")
	switch {
	case lower == "core.gitproxy" || section == "include" || section == "includeif":
		w.addHost(UnknownHost)
	case lower == "core.sshcommand" && !known:
		w.addHost(UnknownHost)
	case lower == "core.sshcommand":
		w.sshCommand(value)
	case strings.HasSuffix(lower, ".proxy") && (section == "http" || section == "https" || section == "remote"):
		if !known || value != "" {
			w.addNetworkHost(value)
		}
	case section == "url" && (strings.HasSuffix(lower, ".insteadof") || strings.HasSuffix(lower, ".pushinsteadof")):
		base := key[len("url."):strings.LastIndex(key, ".")]
		w.addNetworkHost(base)
	}
}

func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
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
	w.addHostName(hostOf(value))
}

func (w *walker) addHostName(host string) {
	if host != "" && !slices.Contains(w.hosts, host) {
		w.hosts = append(w.hosts, host)
	}
}

// addNetworkHost records the host of an operand of a network tool. An
// operand that names no host, such as an unresolved "$URL", gives
// UnknownHost, because the tool can contact any host.
func (w *walker) addNetworkHost(value string) {
	if hostOf(value) == "" {
		w.addHost(UnknownHost)
		return
	}
	w.addHost(value)
}

// HostOf returns the lower-case host of a URL, of "user@host:path", or of a
// bare host name. It returns "" when value names no host that it can read.
func HostOf(value string) string {
	return hostOf(strings.TrimSpace(value))
}

func hostOf(value string) string {
	if value == UnknownHost {
		return UnknownHost
	}
	if scheme, rest, ok := strings.Cut(value, ":"); ok && isScheme(scheme) && strings.HasPrefix(rest, "/") {
		// A URL with a scheme. A URL that does not parse, such as
		// "https:/host" or one with a backslash in the authority, names
		// no host that Gryph can trust.
		u, err := url.Parse(value)
		if err != nil {
			return ""
		}
		return normalizeHost(u.Hostname())
	}
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

// isScheme reports whether s is a URL scheme. A single letter is a Windows
// drive, not a scheme.
func isScheme(s string) bool {
	if first := s[0] | 0x20; len(s) < 2 || first < 'a' || first > 'z' {
		return false
	}
	for _, r := range strings.ToLower(s) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if h == "" || strings.ContainsAny(h, " \t\"'") {
		return ""
	}
	return h
}
