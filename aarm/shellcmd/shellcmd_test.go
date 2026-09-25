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

func TestTargets_NoWorkingDirKeepsRelativePath(t *testing.T) {
	assert.Equal(t, []Target{{".cc/settings.json", true}}, Targets("rm .cc/settings.json", Env{}))
}
