package shellcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTargets(t *testing.T) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}

	cases := []struct {
		name    string
		command string
		want    []Target
	}{
		{"redirect", `printf '{}' > ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", false}}},
		{"append redirect", `echo x >> "$HOME/.cc/settings.json"`, []Target{{"/home/u/.cc/settings.json", false}}},
		{"redirect all", `cmd &> out.log`, []Target{{"/work/out.log", false}}},
		{"rm", `rm -rf ~/.cc`, []Target{{"/home/u/.cc", true}}},
		{"rm quoted braces", `rm -rf "${HOME}/.cc/"`, []Target{{"/home/u/.cc", true}}},
		{"rm glob", `rm -rf ~/.cc/*`, []Target{{"/home/u/.cc", true}}},
		{"unlink", `unlink ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"mv", `mv ~/.cc /tmp/x`, []Target{{"/home/u/.cc", true}, {"/tmp/x", false}, {"/tmp/x/.cc", false}}},
		{"mv into home", `mv notes.txt ~/`, []Target{{"/work/notes.txt", true}, {"/home/u", false}, {"/home/u/notes.txt", false}}},
		{"cp dest", `cp /tmp/s.json ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", false}, {"/home/u/.cc/settings.json/s.json", false}}},
		{"cp into dir", `cp settings.json ~/.cc/`, []Target{{"/home/u/.cc", false}, {"/home/u/.cc/settings.json", false}}},
		{"cp target dir flag", `cp -t ~/.cc settings.json`, []Target{{"/home/u/.cc", false}, {"/home/u/.cc/settings.json", false}}},
		{"tee", `echo {} | tee -a ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", false}}},
		{"sed in place", `sed -i 's/a/b/' ~/.cc/settings.json`, []Target{{"/work/s/a/b", false}, {"/home/u/.cc/settings.json", false}}},
		{"sed read only", `sed 's/a/b/' ~/.cc/settings.json`, nil},
		{"dd", `dd if=/dev/zero of=~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", false}}},
		{"chmod", `chmod 000 ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"find delete", `find ~/.cc -name '*.json' -delete`, []Target{{"/home/u/.cc", true}}},
		{"relative path", `rm settings.json`, []Target{{"/work/settings.json", true}}},
		{"cd then rm", `cd ~/.cc && rm settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"sudo wrapper", `sudo -E rm -f ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"env wrapper", `env A=1 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"bash -c", `bash -c "rm -rf ~/.cc"`, []Target{{"/home/u/.cc", true}}},
		{"eval", `eval rm -rf ~/.cc`, []Target{{"/home/u/.cc", true}}},
		{"subshell", `(cd /tmp; rm -rf ~/.cc)`, []Target{{"/home/u/.cc", true}}},
		{"second command only", `ls ~/.cc && rm -rf /tmp/build`, []Target{{"/tmp/build", true}}},
		{"read only", `cat ~/.cc/settings.json`, nil},
		{"sudo option value", `sudo -u root rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"env unset option", `env -u NAME rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"nice option value", `nice -n 10 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"stdbuf option value", `stdbuf -o L tee ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", false}}},
		{"timeout duration", `timeout -s KILL 5 rm ~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"env chdir", `env -C ~/.cc rm settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"bash -lc", `bash -lc 'rm ~/.cc/settings.json'`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"sh -ec", `sh -ec "rm -rf ~/.cc"`, []Target{{"/home/u/.cc", true}}},
		{"bash -o option -c", `bash -o pipefail -c 'rm -rf ~/.cc'`, []Target{{"/home/u/.cc", true}}},
		{"bash script file", `bash deploy.sh`, nil},
		// The shell does not expand a quoted or escaped "~". The walker expands
		// it anyway, so it can over-block but never miss the home path.
		{"escaped tilde expands", `rm \~/.cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"escaped space", `rm ~/.cc/my\ settings.json`, []Target{{"/home/u/.cc/my settings.json", true}}},
		{"escaped quote in double quotes", `tee "$HOME/.cc/a\"b"`, []Target{{"/home/u/.cc/a\"b", false}}},
		{"find exec rm", `find ~/.cc -name '*.json' -exec rm {} \;`, []Target{{"/home/u/.cc", true}}},
		{"find exec sed in place", `find ~/.cc -exec sed -i s/a/b/ {} +`, []Target{{"/work/s/a/b", false}, {"/home/u/.cc", true}}},
		{"find exec read only", `find ~/.cc -exec cat {} \;`, nil},
		{"find exec grep", `find . -name '*.go' -exec grep -l TODO {} +`, nil},
		{"find -H root", `find -H ~/.cc -delete`, []Target{{"/home/u/.cc", true}}},
		{"find -L root", `find -L ~/.cc -name x -delete`, []Target{{"/home/u/.cc", true}}},
		{"find -P root", `find -P ~/.cc -delete`, []Target{{"/home/u/.cc", true}}},
		{"find -D and -O roots", `find -D exec -O3 ~/.cc -delete`, []Target{{"/home/u/.cc", true}}},
		{"find no root", `find -name x -delete`, []Target{{"/work", true}}},
		{"find execdir rm", `find ~/.cc -execdir rm settings.json \;`, []Target{{"/home/u/.cc", true}}},
		{"find execdir rm file", `find ~/.cc -name '*.json' -execdir rm {} +`, []Target{{"/home/u/.cc", true}}},
		{"find execdir reads the root", `find ~/.cc -execdir cp {} /tmp/backup \;`, []Target{{"/tmp/backup", false}, {"/tmp/backup", true}}},
		{"find execdir read only", `find ~/.cc -execdir cat {} \;`, nil},
		{"find okdir", `find ~/.cc -okdir rm {} \;`, []Target{{"/home/u/.cc", true}}},
		{"unknown variable", `rm -rf "$DIR/.cc"`, nil},
		{"command substitution", `rm -rf $(echo ~/.cc)`, nil},
		{"parse error fails closed", `rm ~/.cc/settings.json ) (`, []Target{
			{"/work/rm", true}, {"/home/u/.cc/settings.json", true}, {"/work/)", true}, {"/work/(", true},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Targets(tc.command, env))
		})
	}
}

func TestTargets_WorkingDirectoryScope(t *testing.T) {
	env := Env{WorkingDir: "/home/u", Home: "/home/u"}

	cases := []struct {
		name    string
		command string
		want    []Target
	}{
		{"cd in subshell does not leak", `(cd /tmp); rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"cd in pipe does not leak", `cd /tmp | true; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"cd in substitution does not leak", `echo $(cd /tmp); rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"cd in background does not leak", `cd /tmp & rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"skipped cd keeps both", `false && cd /tmp; rm .cc/settings.json`, []Target{
			{"/home/u/.cc/settings.json", true}, {"/tmp/.cc/settings.json", true},
		}},
		{"cd in if body keeps both", `if true; then cd /tmp; fi; rm .cc/settings.json`, []Target{
			{"/home/u/.cc/settings.json", true}, {"/tmp/.cc/settings.json", true},
		}},
		{"cd in if body applies inside", `if true; then cd ~/.cc; rm settings.json; fi`, []Target{
			{"/home/u/settings.json", true}, {"/home/u/.cc/settings.json", true},
		}},
		{"cd chain over-approximates", `cd /tmp && cd ~/.cc && rm settings.json`, []Target{
			{"/tmp/settings.json", true}, {"/home/u/.cc/settings.json", true},
		}},
		{"cd with no argument goes home", `cd /tmp; cd; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"cd in eval applies", `cd /tmp; eval 'cd /home/u'; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"command cd applies", `cd /tmp; command cd /home/u; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"builtin cd applies", `cd /tmp; builtin cd /home/u; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
		{"cd in bash -c does not leak", `bash -c 'cd /tmp'; rm .cc/settings.json`, []Target{{"/home/u/.cc/settings.json", true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, Targets(tc.command, env))
		})
	}
}

func TestTargets_NoWorkingDirKeepsRelativePath(t *testing.T) {
	assert.Equal(t, []Target{{".cc/settings.json", true}}, Targets("rm .cc/settings.json", Env{}))
}
