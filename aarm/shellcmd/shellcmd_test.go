package shellcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalyze_Changes(t *testing.T) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}

	cases := []struct {
		name    string
		command string
		want    []Target
	}{
		{"redirect", `printf '{}' > ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessWrite}}},
		{"append redirect", `echo x >> "$HOME/.cc/settings.json"`, []Target{{"/home/u/.cc/settings.json", AccessWrite}}},
		{"redirect all", `cmd &> out.log`, []Target{{"/work/out.log", AccessWrite}}},
		{"rm", `rm -rf ~/.cc`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"rm quoted braces", `rm -rf "${HOME}/.cc/"`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"rm glob", `rm -rf ~/.cc/*`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"unlink", `unlink ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"mv", `mv ~/.cc /tmp/x`, []Target{{"/home/u/.cc", AccessRemove}, {"/tmp/x", AccessWrite}, {"/tmp/x/.cc", AccessWrite}}},
		{"mv into home", `mv notes.txt ~/`, []Target{{"/work/notes.txt", AccessRemove}, {"/home/u", AccessWrite}, {"/home/u/notes.txt", AccessWrite}}},
		{"cp dest", `cp /tmp/s.json ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessWrite}, {"/home/u/.cc/settings.json/s.json", AccessWrite}}},
		{"cp into dir", `cp settings.json ~/.cc/`, []Target{{"/home/u/.cc", AccessWrite}, {"/home/u/.cc/settings.json", AccessWrite}}},
		{"cp target dir flag", `cp -t ~/.cc settings.json`, []Target{{"/home/u/.cc", AccessWrite}, {"/home/u/.cc/settings.json", AccessWrite}}},
		{"tee", `echo {} | tee -a ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessWrite}}},
		{"sed in place", `sed -i 's/a/b/' ~/.cc/settings.json`, []Target{{"/work/s/a/b", AccessWrite}, {"/home/u/.cc/settings.json", AccessWrite}}},
		{"sed read only", `sed 's/a/b/' ~/.cc/settings.json`, nil},
		{"dd", `dd if=/dev/zero of=~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessWrite}}},
		{"chmod", `chmod 000 ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"find delete", `find ~/.cc -name '*.json' -delete`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"relative path", `rm settings.json`, []Target{{"/work/settings.json", AccessRemove}}},
		{"cd then rm", `cd ~/.cc && rm settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"sudo wrapper", `sudo -E rm -f ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"env wrapper", `env A=1 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"bash -c", `bash -c "rm -rf ~/.cc"`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"eval", `eval rm -rf ~/.cc`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"subshell", `(cd /tmp; rm -rf ~/.cc)`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"second command only", `ls ~/.cc && rm -rf /tmp/build`, []Target{{"/tmp/build", AccessRemove}}},
		{"read only", `cat ~/.cc/settings.json`, nil},
		{"sudo option value", `sudo -u root rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"env unset option", `env -u NAME rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"nice option value", `nice -n 10 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"stdbuf option value", `stdbuf -o L tee ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessWrite}}},
		{"timeout duration", `timeout -s KILL 5 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"env chdir", `env -C ~/.cc rm settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"bash -lc", `bash -lc 'rm ~/.cc/settings.json'`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"sh -ec", `sh -ec "rm -rf ~/.cc"`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"bash -o option -c", `bash -o pipefail -c 'rm -rf ~/.cc'`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"bash script file", `bash deploy.sh`, nil},
		// The shell does not expand a quoted or escaped "~". The walker expands
		// it anyway, so it can over-block but never miss the home path.
		{"escaped tilde expands", `rm \~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"escaped space", `rm ~/.cc/my\ settings.json`, []Target{{"/home/u/.cc/my settings.json", AccessRemove}}},
		{"escaped quote in double quotes", `tee "$HOME/.cc/a\"b"`, []Target{{"/home/u/.cc/a\"b", AccessWrite}}},
		{"find exec rm", `find ~/.cc -name '*.json' -exec rm {} \;`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find exec sed in place", `find ~/.cc -exec sed -i s/a/b/ {} +`, []Target{{"/work/s/a/b", AccessWrite}, {"/home/u/.cc", AccessRemove}}},
		{"find exec read only", `find ~/.cc -exec cat {} \;`, nil},
		{"find exec grep", `find . -name '*.go' -exec grep -l TODO {} +`, nil},
		{"find -H root", `find -H ~/.cc -delete`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find -L root", `find -L ~/.cc -name x -delete`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find -P root", `find -P ~/.cc -delete`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find -D and -O roots", `find -D exec -O3 ~/.cc -delete`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find no root", `find -name x -delete`, []Target{{"/work", AccessRemove}}},
		{"find execdir rm", `find ~/.cc -execdir rm settings.json \;`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find execdir rm file", `find ~/.cc -name '*.json' -execdir rm {} +`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"find execdir reads the root", `find ~/.cc -execdir cp {} /tmp/backup \;`, []Target{{"/tmp/backup", AccessWrite}, {"/tmp/backup", AccessRemove}}},
		{"find execdir read only", `find ~/.cc -execdir cat {} \;`, nil},
		{"find okdir", `find ~/.cc -okdir rm {} \;`, []Target{{"/home/u/.cc", AccessRemove}}},
		{"unknown variable", `rm -rf "$DIR/.cc"`, nil},
		{"command substitution", `rm -rf $(echo ~/.cc)`, nil},
		{"parse error fails closed", `rm ~/.cc/settings.json ) (`, []Target{
			{"/work/rm", AccessRemove}, {"/home/u/.cc/settings.json", AccessRemove}, {"/work/)", AccessRemove}, {"/work/(", AccessRemove},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Analyze(tc.command, env).Changes())
		})
	}
}

func TestAnalyze_ChangesWorkingDirectoryScope(t *testing.T) {
	env := Env{WorkingDir: "/home/u", Home: "/home/u"}

	cases := []struct {
		name    string
		command string
		want    []Target
	}{
		{"cd in subshell does not leak", `(cd /tmp); rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"cd in pipe does not leak", `cd /tmp | true; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"cd in substitution does not leak", `echo $(cd /tmp); rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"cd in background does not leak", `cd /tmp & rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"skipped cd keeps both", `false && cd /tmp; rm .cc/settings.json`, []Target{
			{"/home/u/.cc/settings.json", AccessRemove}, {"/tmp/.cc/settings.json", AccessRemove},
		}},
		{"cd in if body keeps both", `if true; then cd /tmp; fi; rm .cc/settings.json`, []Target{
			{"/home/u/.cc/settings.json", AccessRemove}, {"/tmp/.cc/settings.json", AccessRemove},
		}},
		{"cd in if body applies inside", `if true; then cd ~/.cc; rm settings.json; fi`, []Target{
			{"/home/u/settings.json", AccessRemove}, {"/home/u/.cc/settings.json", AccessRemove},
		}},
		{"cd chain over-approximates", `cd /tmp && cd ~/.cc && rm settings.json`, []Target{
			{"/tmp/settings.json", AccessRemove}, {"/home/u/.cc/settings.json", AccessRemove},
		}},
		{"cd with no argument goes home", `cd /tmp; cd; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"cd in eval applies", `cd /tmp; eval 'cd /home/u'; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"command cd applies", `cd /tmp; command cd /home/u; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"builtin cd applies", `cd /tmp; builtin cd /home/u; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
		{"cd in bash -c does not leak", `bash -c 'cd /tmp'; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", AccessRemove}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, Analyze(tc.command, env).Changes())
		})
	}
}

func TestAnalyze_NoWorkingDirKeepsRelativePath(t *testing.T) {
	assert.Equal(t, []Target{{".cc/settings.json", AccessRemove}}, Analyze("rm .cc/settings.json", Env{}).Changes())
}
