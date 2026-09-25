package shellcmd

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
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
}

func valueOptions(names ...string) options {
	return options{values: flagSet(names...)}
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
			p.operands = append(p.operands, args[i+1:]...)
			return p
		case strings.HasPrefix(a, "--"):
			name, v, hasValue := strings.Cut(a, "=")
			name = o.longName(name)
			p.seen[name] = true
			if hasValue {
				p.values[name] = append(p.values[name], v)
			} else if o.values[name] && i+1 < len(args) {
				i++
				p.values[name] = append(p.values[name], args[i])
			}
		case strings.HasPrefix(a, "-") && a != "-":
			for j := 1; j < len(a); j++ {
				flag := "-" + a[j:j+1]
				p.seen[flag] = true
				if !o.values[flag] {
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

// readCommands read every file operand.
var readCommands = map[string]options{
	"cat": {}, "tac": {}, "less": {}, "more": {}, "strings": valueOptions("-n", "-t"),
	"xxd": valueOptions("-c", "-g", "-l", "-s", "-o"), "od": valueOptions("-A", "-j", "-N", "-t", "-w"),
	"hexdump": valueOptions("-e", "-f", "-n", "-s"), "base64": valueOptions("-w"), "base32": valueOptions("-w"),
	"nl": valueOptions("-b", "-s", "-v", "-w"), "wc": {}, "uniq": valueOptions("-f", "-s", "-w"),
	"cut": valueOptions("-b", "-c", "-d", "-f"), "diff": {}, "cmp": {}, "md5sum": {},
	"sha1sum": {}, "sha256sum": {}, "sha512sum": {}, "zcat": {}, "bat": {},
	"head": valueOptions("-n", "-c", "--lines", "--bytes"),
	"tail": valueOptions("-n", "-c", "--lines", "--bytes", "-s", "--pid"),
}

// editCommands write every file operand. An option that writes a file,
// such as "vim -w", is not in the value table, so its file is an operand.
var editCommands = map[string]options{
	"vim":  valueOptions("-c", "--cmd", "-S", "-u", "-U", "-t", "-T"),
	"vi":   valueOptions("-c", "--cmd", "-S", "-u", "-U", "-t", "-T"),
	"nano": {},
}

// copyTool describes a local copy command. A destination option in
// dirFlags makes the copy replace paths inside the destination.
type copyTool struct {
	opts          options
	readsSources  bool
	removeSources bool
	dirFlags      []string
}

var noTargetDirectory = []string{"-T", "--no-target-directory"}

var (
	copyOptions = options{
		values: flagSet("-S", "--suffix", "-t", "--target-directory"),
		flags:  flagSet("--recursive", "--archive", "--no-target-directory"),
		abbrev: true,
	}
	installOptions = options{
		values: flagSet("-S", "--suffix", "-t", "--target-directory", "-g", "--group", "-m", "--mode",
			"-o", "--owner", "--strip-program"),
		flags:  flagSet("--no-target-directory", "--strip"),
		abbrev: true,
	}
	copyTools = map[string]copyTool{
		"cp": {opts: copyOptions, readsSources: true,
			dirFlags: append([]string{"-r", "-R", "-a", "--recursive", "--archive"}, noTargetDirectory...)},
		"install": {opts: installOptions, readsSources: true, dirFlags: noTargetDirectory},
		"mv":      {opts: copyOptions, readsSources: true, removeSources: true, dirFlags: noTargetDirectory},
		"ln":      {opts: copyOptions, dirFlags: noTargetDirectory},
	}
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
	gzipOptions = options{
		values: flagSet("-S", "--suffix"),
		flags: flagSet("--stdout", "--to-stdout", "--keep", "--decompress", "--uncompress",
			"--recursive", "--list", "--test"),
		abbrev: true,
	}
	zipOptions = valueOptions("-b", "-n", "-t", "-x", "-i", "-P", "-Z", "-s", "--temp-path",
		"--suffixes", "--from-date", "--before-date", "--exclude", "--include", "--password",
		"--compression-method", "--split-size")
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
			"--trace", "--trace-ascii", "--stderr"),
		flags: flagSet("--head", "--remote-name", "--remote-name-all", "--remote-header-name"),
		// curl before 8.x accepts a unique prefix of a long option.
		abbrev: true,
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
	gitOptions = valueOptions("-C", "-c", "--git-dir", "--work-tree", "--namespace",
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
			"--remove-files", "--checkpoint", "--delay-directory-restore"),
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
func (w *walker) scriptTool(p parsedArgs, fileFlags, exprFlags []string, cwds dirs) {
	w.addAll(p.value(fileFlags...), AccessRead, cwds)
	ops := p.operands
	if !p.has(fileFlags...) && !p.has(exprFlags...) && len(ops) > 0 {
		ops = ops[1:]
	}
	w.addAll(ops, AccessRead, cwds)
}

// localCopy handles cp, mv, install, and ln.
func (w *walker) localCopy(tool copyTool, args []string, cwds dirs) {
	p := parseArgs(args, tool.opts)
	dest, sources := copyOperands(p)
	if tool.readsSources {
		w.addAll(sources, AccessRead, cwds)
	}
	if tool.removeSources {
		w.addAll(sources, AccessRemove, cwds)
	}
	w.addDest(dest, sources, destAccess(p, sources, tool.dirFlags), cwds)
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

// destAccess returns how a copy uses its destination. A copy of the
// contents of a directory, a recursive copy, or a copy onto the
// destination itself (-T) can write any path in the destination, so it is
// a removal of the destination.
func destAccess(p parsedArgs, sources, dirFlags []string) Access {
	if p.has(dirFlags...) || slices.ContainsFunc(sources, copiesContents) {
		return AccessRemove
	}
	return AccessWrite
}

func copiesContents(src string) bool {
	return strings.HasSuffix(src, "/") || strings.HasSuffix(src, "/.")
}

// sort reads the operands and writes the -o file.
func (w *walker) sort(p parsedArgs, cwds dirs) {
	w.addAll(p.operands, AccessRead, cwds)
	w.addAll(p.value("-o", "--output"), AccessWrite, cwds)
}

// gzip replaces each operand with the compressed or the decompressed file.
// With -c, -l, or -t it only reads the operands. With -k it keeps the
// operands. A recursive run changes files inside the operands.
func (w *walker) gzip(p parsedArgs, decompress bool, cwds dirs) {
	if p.has("-c", "--stdout", "--to-stdout", "-l", "--list", "-t", "--test") {
		w.addAll(p.operands, AccessRead, cwds)
		return
	}
	if p.has("-r", "--recursive") {
		w.addAll(p.operands, AccessRemove, cwds)
		return
	}
	decompress = decompress || p.has("-d", "--decompress", "--uncompress")
	for _, op := range p.operands {
		if p.has("-k", "--keep") {
			w.add(op, AccessRead, cwds)
		} else {
			w.add(op, AccessRemove, cwds)
		}
		w.add(gzipOutput(op, p.value("-S", "--suffix"), decompress), AccessWrite, cwds)
	}
}

// gzipOutput returns the file that gzip writes for file. It returns an
// empty string when gzip does not know the suffix of a compressed file.
func gzipOutput(file string, suffixes []string, decompress bool) string {
	if !decompress {
		if len(suffixes) > 0 {
			return file + suffixes[len(suffixes)-1]
		}
		return file + ".gz"
	}
	lower := strings.ToLower(file)
	for _, s := range append(suffixes, ".gz", "-gz", ".z", "-z", "_z") {
		if strings.HasSuffix(lower, strings.ToLower(s)) && len(file) > len(s) {
			return file[:len(file)-len(s)]
		}
	}
	for _, s := range []string{".tgz", ".taz"} {
		if strings.HasSuffix(lower, s) && len(file) > len(s) {
			return file[:len(file)-len(s)] + ".tar"
		}
	}
	return ""
}

// zip writes the archive, the first operand, and reads the other operands.
// With -m it also removes them.
func (w *walker) zip(p parsedArgs, cwds dirs) {
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

// tar records the archive and the members. The first word can be a bundle
// of flags without a dash, as in "tar czf out.tgz dir". A create, append,
// update, concatenate, or delete writes the archive. An extract writes
// into the -C directory, or into the working directory. Other modes read
// the archive.
func (w *walker) tar(args []string, cwds dirs) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && isLetters(args[0]) {
		args = append([]string{"-" + args[0]}, args[1:]...)
	}
	p := parseArgs(args, tarOptions)
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
	switch {
	case adds && p.has("--remove-files"):
		w.addAll(p.operands, AccessRemove, cwds)
	case adds || concatenates:
		w.addAll(p.operands, AccessRead, cwds)
	case p.has("-x", "--extract", "--get") && !p.has("-O", "--to-stdout"):
		targets := p.value("-C", "--directory")
		if len(targets) == 0 {
			targets = []string{"."}
		}
		w.addAll(targets, AccessRemove, cwds)
	}
}

func isLetters(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
	}) < 0
}

// curl records the hosts, the uploaded files, and the output files.
func (w *walker) curl(p parsedArgs, cwds dirs) {
	urls := slices.Concat(p.operands, p.value("--url"))
	for _, u := range urls {
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
	w.addOutputs(p.value("-o", "--output", "-c", "--cookie-jar", "-D", "--dump-header",
		"--trace", "--trace-ascii", "--stderr"), cwds)
	w.addAll(p.value("--output-dir"), AccessRemove, cwds)
	if !p.has("-O", "--remote-name", "--remote-name-all") {
		return
	}
	if p.has("-J", "--remote-header-name") {
		w.add(".", AccessRemove, cwds)
		return
	}
	for _, u := range urls {
		w.add(remoteFileName(u), AccessWrite, cwds)
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
			p.values[opt] = append(p.values[opt], v)
		case !strings.EqualFold(v, "off"):
			p.seen[opt] = true
		}
	}
	return p
}

// wget records the hosts, the uploaded files, and the output files.
// Without -O, wget saves each URL under its file name in the -P directory
// or the working directory. A recursive download, or a download that takes
// names from the server or an input file, can write any path in that
// directory.
func (w *walker) wget(p parsedArgs, cwds dirs) {
	p = withWgetrc(p)
	for _, u := range p.operands {
		w.addHost(u)
	}
	w.addAll(p.value("--post-file", "--body-file", "-i", "--input-file"), AccessRead, cwds)
	w.addOutputs(p.value("-O", "--output-document", "-o", "--output-file", "-a", "--append-output",
		"--save-cookies"), cwds)
	prefixes := p.value("-P", "--directory-prefix")
	w.addAll(prefixes, AccessRemove, cwds)
	if len(prefixes) > 0 || p.has("-O", "--output-document", "--spider") {
		return
	}
	if p.has("-r", "--recursive", "-m", "--mirror", "-p", "--page-requisites", "-x",
		"--force-directories", "--content-disposition", "--trust-server-names", "-i", "--input-file") {
		w.add(".", AccessRemove, cwds)
		return
	}
	for _, u := range p.operands {
		name := remoteFileName(u)
		if name == "" {
			name = "index.html"
		}
		w.add(name, AccessWrite, cwds)
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

func (w *walker) remoteShell(p parsedArgs) {
	if len(p.operands) > 0 {
		w.addHost(p.operands[0])
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
func (w *walker) remoteCopy(p parsedArgs, dirFlags []string, cwds dirs) {
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
	w.addDest(dest, sources, destAccess(p, sources, dirFlags), cwds)
}

// gitRemoteArg gives, for each git subcommand that names a remote, the
// position of the remote among the subcommand's operands.
var gitRemoteArg = map[string]int{
	"clone": 1, "fetch": 1, "pull": 1, "push": 1, "ls-remote": 1,
}

// git records the host of the remote that a git command names.
func (w *walker) git(args []string) {
	ops := parseArgs(args, gitOptions).operands
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
	w.addHostName(hostOf(value))
}

func (w *walker) addHostName(host string) {
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
