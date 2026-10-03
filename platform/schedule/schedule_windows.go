package schedule

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const taskFolder = `SafeDep\`

// usersGroupSID is the well-known SID of the local Users group. A task
// whose principal is this group runs for every member who logs on, as that
// member, with the interactive token.
const usersGroupSID = "S-1-5-32-545"

// installSystemWide registers one task that runs for every member of the
// Users group at logon and then on the interval, as the member. The task
// comes from an XML definition, because the schtasks flags cannot name a
// group principal.
func installSystemWide(ctx context.Context, job Job) (*Result, error) {
	task := taskFolder + job.Name
	file, err := os.CreateTemp("", "gryph-task-*.xml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.WriteString(renderTaskXML(job)); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	res := &Result{Changed: true, Next: "schtasks /Create /F /TN " + task + " /XML <task.xml>"}
	if err := runCommand(ctx, "schtasks", "/Create", "/F", "/TN", task, "/XML", file.Name()); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func removeSystemWide(ctx context.Context, name string) (*Result, error) {
	return remove(ctx, name)
}

// renderTaskXML renders the task definition: a logon trigger for every
// member of the Users group that repeats on the interval, and the command
// run as the logged-on member.
func renderTaskXML(job Job) string {
	var args strings.Builder
	for _, arg := range job.Command[1:] {
		if strings.ContainsAny(arg, " \t") {
			arg = `"` + arg + `"`
		}
		if args.Len() > 0 {
			args.WriteString(" ")
		}
		args.WriteString(arg)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>%s</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <Repetition>
        <Interval>PT%dM</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <GroupId>%s</GroupId>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <StartWhenAvailable>true</StartWhenAvailable>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT10M</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`, xmlText(job.Description), job.minutes(), usersGroupSID, xmlText(job.Command[0]), xmlText(args.String()))
}

func xmlText(s string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(s))
	return out.String()
}

func install(ctx context.Context, job Job) (*Result, error) {
	task := taskFolder + job.Name
	res := &Result{Changed: true, Next: "schtasks /Create /F /SC MINUTE /MO " + strconv.Itoa(job.minutes()) + " /TN " + task + " /TR " + taskCommand(job.Command)}
	if err := runCommand(ctx, "schtasks", "/Create", "/F", "/SC", "MINUTE", "/MO", strconv.Itoa(job.minutes()),
		"/TN", task, "/TR", taskCommand(job.Command)); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func remove(ctx context.Context, name string) (*Result, error) {
	task := taskFolder + name
	res := &Result{Enabled: true, Changed: true}
	if err := runCommand(ctx, "schtasks", "/Delete", "/F", "/TN", task); err != nil {
		res.Changed = false
		// A task that does not exist is not an error for a remove.
		if !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "cannot find") {
			res.Enabled = false
			res.Next = "schtasks /Delete /F /TN " + task
		}
	}
	return res, nil
}

// taskCommand renders the /TR value: the program in double quotes, then
// the arguments.
func taskCommand(command []string) string {
	parts := []string{`"` + command[0] + `"`}
	for _, arg := range command[1:] {
		if strings.ContainsAny(arg, " \t") {
			arg = `"` + arg + `"`
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}
