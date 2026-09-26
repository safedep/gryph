package shellcmd

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyze_Changes(t *testing.T) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}

	cases := []struct {
		name    string
		command string
		want    []Target
	}{
		{"redirect", `printf '{}' > ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"append redirect", `echo x >> "$HOME/.cc/settings.json"`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"redirect all", `cmd &> out.log`, []Target{{Path: "/work/out.log", Access: AccessWrite}}},
		{"rm", `rm -rf ~/.cc`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"rm quoted braces", `rm -rf "${HOME}/.cc/"`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"rm glob", `rm -rf ~/.cc/*`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"unlink", `unlink ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"mv", `mv ~/.cc /tmp/x`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}, {Path: "/tmp/x", Access: AccessWrite}, {Path: "/tmp/x/.cc", Access: AccessWrite}}},
		{"mv into home", `mv notes.txt ~/`, []Target{{Path: "/work/notes.txt", Access: AccessRemove}, {Path: "/home/u", Access: AccessWrite}, {Path: "/home/u/notes.txt", Access: AccessWrite}}},
		{"cp dest", `cp /tmp/s.json ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}, {Path: "/home/u/.cc/settings.json/s.json", Access: AccessWrite}}},
		{"cp into dir", `cp settings.json ~/.cc/`, []Target{{Path: "/home/u/.cc", Access: AccessWrite}, {Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"cp target dir flag", `cp -t ~/.cc settings.json`, []Target{{Path: "/home/u/.cc", Access: AccessWrite}, {Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"tee", `echo {} | tee -a ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"sed in place", `sed -i 's/a/b/' ~/.cc/settings.json`, []Target{{Path: "/work/s/a/b", Access: AccessWrite}, {Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"sed read only", `sed 's/a/b/' ~/.cc/settings.json`, nil},
		{"dd", `dd if=/dev/zero of=~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"chmod", `chmod 000 ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"find delete", `find ~/.cc -name '*.json' -delete`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"relative path", `rm settings.json`, []Target{{Path: "/work/settings.json", Access: AccessRemove}}},
		{"cd then rm", `cd ~/.cc && rm settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"sudo wrapper", `sudo -E rm -f ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"env wrapper", `env A=1 rm ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"bash -c", `bash -c "rm -rf ~/.cc"`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"eval", `eval rm -rf ~/.cc`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"subshell", `(cd /tmp; rm -rf ~/.cc)`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"second command only", `ls ~/.cc && rm -rf /tmp/build`, []Target{{Path: "/tmp/build", Access: AccessRemove}}},
		{"read only", `cat ~/.cc/settings.json`, nil},
		{"sudo option value", `sudo -u root rm ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"env unset option", `env -u NAME rm ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"nice option value", `nice -n 10 rm ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"stdbuf option value", `stdbuf -o L tee ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessWrite}}},
		{"timeout duration", `timeout -s KILL 5 rm ~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"env chdir", `env -C ~/.cc rm settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"bash -lc", `bash -lc 'rm ~/.cc/settings.json'`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"sh -ec", `sh -ec "rm -rf ~/.cc"`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"bash -o option -c", `bash -o pipefail -c 'rm -rf ~/.cc'`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"bash script file", `bash deploy.sh`, nil},
		// The shell does not expand a quoted or escaped "~". The walker expands
		// it anyway, so it can over-block but never miss the home path.
		{"escaped tilde expands", `rm \~/.cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"escaped space", `rm ~/.cc/my\ settings.json`, []Target{{Path: "/home/u/.cc/my settings.json", Access: AccessRemove}}},
		{"escaped quote in double quotes", `tee "$HOME/.cc/a\"b"`, []Target{{Path: "/home/u/.cc/a\"b", Access: AccessWrite}}},
		{"find exec rm", `find ~/.cc -name '*.json' -exec rm {} \;`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find exec sed in place", `find ~/.cc -exec sed -i s/a/b/ {} +`, []Target{{Path: "/work/s/a/b", Access: AccessWrite}, {Path: "/home/u/.cc", Access: AccessWriteTree}}},
		{"find exec read only", `find ~/.cc -exec cat {} \;`, nil},
		{"find exec grep", `find . -name '*.go' -exec grep -l TODO {} +`, nil},
		{"find -H root", `find -H ~/.cc -delete`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find -L root", `find -L ~/.cc -name x -delete`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find -P root", `find -P ~/.cc -delete`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find -D and -O roots", `find -D exec -O3 ~/.cc -delete`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find no root", `find -name x -delete`, []Target{{Path: "/work", Access: AccessRemove}}},
		{"find execdir rm", `find ~/.cc -execdir rm settings.json \;`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find execdir rm file", `find ~/.cc -name '*.json' -execdir rm {} +`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"find execdir reads the root", `find ~/.cc -execdir cp {} /tmp/backup \;`, []Target{{Path: "/tmp/backup", Access: AccessWrite}, {Path: "/tmp/backup", Access: AccessWriteTree}}},
		{"find execdir read only", `find ~/.cc -execdir cat {} \;`, nil},
		{"find okdir", `find ~/.cc -okdir rm {} \;`, []Target{{Path: "/home/u/.cc", Access: AccessRemove}}},
		{"unknown variable", `rm -rf "$DIR/.cc"`, nil},
		{"command substitution", `rm -rf $(echo ~/.cc)`, nil},
		{"parse error records nothing", `rm ~/.cc/settings.json ) (`, nil},
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
		{"cd in subshell does not leak", `(cd /tmp); rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"cd in pipe does not leak", `cd /tmp | true; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"cd in substitution does not leak", `echo $(cd /tmp); rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"cd in background does not leak", `cd /tmp & rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"skipped cd keeps both", `false && cd /tmp; rm .cc/settings.json`, []Target{
			{Path: "/home/u/.cc/settings.json", Access: AccessRemove}, {Path: "/tmp/.cc/settings.json", Access: AccessRemove},
		}},
		{"cd in if body keeps both", `if true; then cd /tmp; fi; rm .cc/settings.json`, []Target{
			{Path: "/home/u/.cc/settings.json", Access: AccessRemove}, {Path: "/tmp/.cc/settings.json", Access: AccessRemove},
		}},
		{"cd in if body applies inside", `if true; then cd ~/.cc; rm settings.json; fi`, []Target{
			{Path: "/home/u/settings.json", Access: AccessRemove}, {Path: "/home/u/.cc/settings.json", Access: AccessRemove},
		}},
		{"cd chain over-approximates", `cd /tmp && cd ~/.cc && rm settings.json`, []Target{
			{Path: "/tmp/settings.json", Access: AccessRemove}, {Path: "/home/u/.cc/settings.json", Access: AccessRemove},
		}},
		{"cd with no argument goes home", `cd /tmp; cd; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"cd in eval applies", `cd /tmp; eval 'cd /home/u'; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"command cd applies", `cd /tmp; command cd /home/u; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"builtin cd applies", `cd /tmp; builtin cd /home/u; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
		{"cd in bash -c does not leak", `bash -c 'cd /tmp'; rm .cc/settings.json`, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, Analyze(tc.command, env).Changes())
		})
	}
}

func TestAnalyze_NoWorkingDirKeepsRelativePath(t *testing.T) {
	assert.Equal(t, []Target{{Path: ".cc/settings.json", Access: AccessRemove}}, Analyze("rm .cc/settings.json", Env{}).Changes())
}

func TestAnalyze_Wrappers(t *testing.T) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}
	settings := []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}

	cases := []struct {
		command string
		want    []Target
	}{
		{`sudo --user=root rm ~/.cc/settings.json`, settings},
		{`sudo -u rm cat ~/.cc/settings.json`, nil},
		{`sudo -D ~/.cc rm settings.json`, settings},
		{`sudo A=1 rm ~/.cc/settings.json`, settings},
		{`doas -u root rm ~/.cc/settings.json`, settings},
		{`env -i A=1 B=2 rm ~/.cc/settings.json`, settings},
		{`env - rm ~/.cc/settings.json`, settings},
		{`env --chdir=/home/u/.cc rm settings.json`, settings},
		{`env A=1`, nil},
		{`nohup rm ~/.cc/settings.json`, settings},
		{`exec -a name rm ~/.cc/settings.json`, settings},
		{`time -p rm ~/.cc/settings.json`, settings},
		{`nice -10 rm ~/.cc/settings.json`, settings},
		{`ionice -c 2 -n 7 rm ~/.cc/settings.json`, settings},
		{`stdbuf -oL rm ~/.cc/settings.json`, settings},
		{`timeout --signal=KILL 5 rm ~/.cc/settings.json`, settings},
		{`timeout 5 rm ~/.cc/settings.json`, settings},
		{`timeout 5`, nil},
		{`xargs -I {} rm ~/.cc/settings.json`, settings},
		{`chrt -f 10 rm ~/.cc/settings.json`, settings},
		{`taskset -c 0 rm ~/.cc/settings.json`, settings},
		{`sudo nice -n 5 timeout 1 env A=1 rm ~/.cc/settings.json`, settings},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			assert.Equal(t, tc.want, Analyze(tc.command, env).Changes())
		})
	}
}

func wrapperChain(wrapper string, n int) string {
	return strings.Repeat(wrapper+" ", n) + "rm ~/.cc/settings.json"
}

var chainWrappers = []string{"nice", "sudo", "env", "nohup", "timeout 1", "command", "sudo -u root", "xargs"}

func TestAnalyze_WrapperChainIsFast(t *testing.T) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}
	for _, wrapper := range chainWrappers {
		t.Run(wrapper, func(t *testing.T) {
			start := time.Now()
			a := Analyze(wrapperChain(wrapper, 60), env)
			assert.Less(t, time.Since(start), 50*time.Millisecond)
			assert.Equal(t, []Target{{Path: "/home/u/.cc/settings.json", Access: AccessRemove}}, a.Changes())
		})
	}
}

func TestAnalyze_CallBudgetStopsTheWalk(t *testing.T) {
	command := strings.Repeat("find a b c d -exec ", 8) + "rm {} +"
	w := &walker{env: Env{WorkingDir: "/work"}}
	_, err := w.script(command, dirs{"/work"})
	require.NoError(t, err)
	assert.Equal(t, maxCalls, w.calls)
	assert.True(t, w.failed, "a walk that stops at the budget is not a full analysis")
	assert.False(t, Analyze(command, Env{WorkingDir: "/work"}).Parsed)
}

func BenchmarkAnalyze_WrapperChain(b *testing.B) {
	env := Env{WorkingDir: "/work", Home: "/home/u"}
	for _, wrapper := range chainWrappers {
		command := wrapperChain(wrapper, 60)
		b.Run(wrapper, func(b *testing.B) {
			for b.Loop() {
				Analyze(command, env)
			}
		})
	}
}
