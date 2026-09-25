export interface Task {
  text: string
  install?: boolean
  cmd?: string
  ask?: string
  see?: string
}

// answers[0] is the right answer. The page mixes the order.
export interface Milestone {
  id: string
  title: string
  lede: string
  tasks: Task[]
  question: string
  answers: [string, string]
  why: string
}

const SB = '/tmp/gryph-sandbox'

export const MILESTONES: Milestone[] = [
  { id: 'install', title: 'Install Gryph and add hooks',
    lede: 'Install Gryph. Add hooks to your agent. Then check the installation.',
    tasks: [
      { text: 'Install Gryph. Use one of these methods.', install: true, see: 'This puts the gryph command on your PATH.' },
      { text: 'Preview the hooks before you install them.', cmd: 'gryph install --dry-run', see: 'Gryph lists the agents it found and the files it will change.' },
      { text: 'Install the hooks.', cmd: 'gryph install', see: 'Gryph makes a backup of each agent configuration first.' },
      { text: 'Check the version, the agents, the database, the configuration and the schema.', cmd: 'gryph status && gryph doctor', see: 'A healthy setup shows one or more agents and all checks passing.' },
      { text: 'Show the actions that Gryph did on itself.', cmd: 'gryph self-log', see: 'You see a record of this installation.' } ],
    question: 'Does the installation change the agent program?',
    answers: ['No. It changes only the hook configuration.', 'Yes. It changes the program file.'],
    why: 'Gryph adds hooks to the agent configuration. It does not change the program file.' },

  { id: 'session', title: 'Record your first session',
    lede: 'See the actions of an agent as they occur. Then examine the records.',
    tasks: [
      { text: 'Make an empty test directory.', cmd: `mkdir -p ${SB} && cd ${SB}`, see: 'Do not use a real project for this test.' },
      { text: 'Start the live monitor.', cmd: 'gryph logs --live', see: 'A monitor opens in the full terminal. To get plain text output, use --follow.' },
      { text: 'Start your agent in a second terminal, in the test directory.', ask: 'Create readme.txt with the text "hello gryph". Then list the files in this directory.',
        see: 'Each read, write and command shows in the monitor. Gryph records the actions of the agent. It does not record your own commands.' },
      { text: 'Close the monitor, then show the session records.', cmd: 'gryph sessions && gryph logs --today', see: 'You see the read, write and command actions of each session.' },
      { text: 'Count the write actions.', cmd: 'gryph query --action file_write --today --count', see: 'A count above zero confirms the hooks are recording.' } ],
    question: 'When do the hooks record an action?',
    answers: ['Before the action runs and after it runs.', 'Only before the action runs.'],
    why: 'Gryph records each action before it runs and after it runs.' },

  { id: 'query', title: 'Find and export events',
    lede: 'Use filters, diffs and exports to find what an agent did.',
    tasks: [
      { text: 'Show one type of action.', cmd: 'gryph query --action file_write --today', see: 'Types include file_read, file_write, command_exec and network_request.' },
      { text: 'Show the actions for a file name or a command pattern.', cmd: 'gryph query --file "**/*.env" --today', see: 'Both --file and --command accept patterns.' },
      { text: 'Show one write action, with the file before and after the change.', cmd: 'gryph diff <event-id>', see: 'Before you use this command, set the logging level to full. For more detail, use gryph cat.' },
      { text: 'Export a week of events as JSON Lines. Send the output to jq.', cmd: "gryph export --since 1w | jq -r '.action_type' | sort | uniq -c | sort -rn", see: 'You see a total for each action type. The export does not include sensitive events.' },
      { text: 'Show the action types for each agent.', cmd: "gryph export --since 1w | jq -r '[.agent_name,.action_type]|@tsv' | sort | uniq -c" } ],
    question: 'Which command does not include sensitive events?',
    answers: ['gryph export', 'gryph diff'],
    why: 'gryph export does not include sensitive events. To include them, add --sensitive. For most audit reports, you need only gryph export and jq.' },

  { id: 'privacy', title: 'Control privacy and detail',
    lede: 'Learn how Gryph records sensitive files. Set the quantity of detail that Gryph records.',
    tasks: [
      { text: 'Tell your agent to read a secret file.', ask: 'Read the .env file in this directory.', see: 'Gryph records the action and marks it as sensitive. Gryph does not keep the content of the file.' },
      { text: 'Show the current logging level.', cmd: 'gryph config get logging.level', see: 'The levels are minimal, standard and full. The default level is standard.' },
      { text: 'Set the level to full.', cmd: 'gryph config set logging.level full', see: 'This adds file diffs, raw events and conversation context.' },
      { text: 'When you finish, set the level to standard again.', cmd: 'gryph config set logging.level standard', see: 'Gryph records the diff counts, the exit codes and a short part of the output.' },
      { text: 'Show the configuration.', cmd: 'gryph config show --format json', see: 'Make sure the privacy settings are correct.' } ],
    question: 'Does Gryph keep the content of a sensitive file?',
    answers: ['No. Gryph never keeps the content.', 'Yes. Gryph encrypts the content.'],
    why: 'Gryph records that the agent used a sensitive file. It never keeps the content of that file.' },

  { id: 'policy', title: 'Enforce a security policy',
    lede: 'Write a YAML policy. Test a CEL condition. Then enable the policy.',
    tasks: [
      { text: 'Write a sample policy file in your test directory.', cmd: `gryph policy init ${SB}/candidate.yml`, see: 'This file is not active yet.' },
      { text: 'Validate this file alone.', cmd: `gryph policy validate --file ${SB}/candidate.yml`, see: 'Do not merge it with other policy files yet.' },
      { text: 'Test a rule before you install it.', cmd: `gryph policy test --file ${SB}/candidate.yml --action command_exec --command "rm -rf /"`, see: "The result is block, with the rule's message." },
      { text: 'Test a CEL rule.', cmd: `gryph policy test --file ${SB}/candidate.yml --action file_write --path src/app.go --context-files-written 30`,
        see: 'The sample policy warns at 25 or more files written. The result is warn, from the rule warn-session-write-volume.' },
      { text: 'Install the policy file, then enable it.', cmd: `gryph policy install ${SB}/candidate.yml && gryph config set policy.enabled true`, see: 'Gryph now checks every action before it runs.' },
      { text: 'Show the decisions that blocked an action.', cmd: 'gryph policy receipts --decision block', see: 'Gryph keeps a record of each one.' } ],
    question: 'What can a CEL condition read?',
    answers: ['The fields of the action and the counters of the session.', 'Only the fields of the action.'],
    why: 'A condition can read an action field, for example action.injection_score. It can also read a session counter, for example context.files_written. Use warn first. When you trust the rule, change it to block.' },

  { id: 'authoring', title: 'Write a policy with your agent',
    lede: 'Install the skill for policy authoring. Tell your agent to write a rule. Examine the rule. Then install it yourself.',
    tasks: [
      { text: 'Install the skill for policy authoring.', cmd: 'npx skills add safedep/gryph --skill gryph-policy-authoring', see: 'It works with Claude Code, Cursor and other agents.' },
      { text: 'Tell your agent to write a rule.', ask: 'Write a gryph policy that blocks git push --force',
        see: 'The agent writes the draft in a directory such as ./gryph-policy/. Gryph does not let agents write to the Gryph configuration directory.' },
      { text: 'Test the draft yourself.', cmd: 'gryph policy test --file ./gryph-policy/no-force-push.yaml --action command_exec --command "git push --force"',
        see: 'Use the file name your agent selected. The result is block. Before it hands you the draft, the agent tests a match, a non-match and a boundary case.' },
      { text: 'Examine the draft, then install it yourself.', cmd: 'gryph policy install ./gryph-policy/no-force-push.yaml', see: 'The agent never installs policies. To remove the rule, delete the file from the policies directory.' },
      { text: 'Show the active policy sources.', cmd: 'gryph policy list', see: 'Your new file appears as an active source.' } ],
    question: 'Who installs the policy?',
    answers: ['You install it. The agent writes the draft and tests it.', 'The agent installs it after the tests pass.'],
    why: 'The skill writes the draft and gives you the install command. Gryph does not let agents write to its configuration directory.' },

  { id: 'receipts', title: 'Sign and verify receipts',
    lede: 'Sign the receipts. Verify the chain. Export the receipts for an audit on a different computer.',
    tasks: [
      { text: 'Make an Ed25519 key pair to sign the receipts.', cmd: 'gryph policy keys generate', see: 'Receipts now include a signature.' },
      { text: 'Verify the chain for all sessions.', cmd: 'gryph policy receipts --verify --all-sessions', see: 'Gryph reports "Chain verification: OK". If the chain is broken, the exit code is not zero.' },
      { text: 'Export the signed receipts, then verify them as a different computer does.', cmd: 'gryph policy receipts export --include-signatures | gryph policy receipts verify-log --input -', see: 'Gryph reports "SIGNATURES OK".' },
      { text: 'Show the context counters for one session.', cmd: 'gryph policy context --session <id>', see: 'These are the same values a CEL condition reads, such as total_actions and files_written.' } ],
    question: 'What does the chain alone prove?',
    answers: ['It finds a change to the data in the database.', 'It encrypts the data.'],
    why: 'The chain finds a change to the data in the database. The signatures protect the export when you move it to a different computer.' },

  { id: 'cost', title: 'Track cost and compare agents',
    lede: 'Track the token use. Compare the agents. Open the statistics dashboard.',
    tasks: [
      { text: 'Show the cost for today.', cmd: 'gryph cost --today', see: 'Gryph calculates it with prices from models.dev.' },
      { text: 'Show the cost for each model.', cmd: 'gryph cost --since 1w --by model', see: 'You can also group by day or by agent. To add older sessions, use --sync. To calculate the cost again, use --force.' },
      { text: 'Open the statistics dashboard for one agent.', cmd: 'gryph stats --since 7d --agent claude-code', see: 'It shows the sessions, actions and cost for the time range.' },
      { text: 'Find the agent with the highest cost this week.', cmd: 'gryph cost --by agent' } ],
    question: 'Where do the model prices come from?',
    answers: ['From models.dev', 'From a Gryph cloud service'],
    why: 'At the end of a session, Gryph calculates the cost with prices from models.dev. Gryph does not have a cloud service.' },

  { id: 'housekeeping', title: 'Maintain and remove Gryph',
    lede: 'Keep the database small. Examine the actions that Gryph did on itself. Learn how to remove Gryph.',
    tasks: [
      { text: 'Preview the retention cleanup before you run it.', cmd: 'gryph retention cleanup --dry-run', see: 'Gryph lists the old rows it would remove.' },
      { text: 'Run the cleanup.', cmd: 'gryph retention cleanup', see: 'Gryph removes the old events. Receipts keep their own period, 365 days by default.' },
      { text: 'Show every action that Gryph did on itself.', cmd: 'gryph self-log', see: 'For example install, uninstall, config set and cleanup.' },
      { text: 'Show the actions that a policy stopped.', cmd: 'gryph policy deferrals', see: 'Escalate stops the agent. Defer keeps the action until an operator examines it.' },
      { text: 'Show what the uninstall command removes.', cmd: 'gryph uninstall --dry-run', see: 'To delete the data too, add --purge.' } ],
    question: 'Which data does the cleanup keep?',
    answers: ['Receipts, for their own retention period. The default is 365 days.', 'All receipts, with no time limit.'],
    why: 'Events, receipts and deferred actions each have their own retention period. Gryph always keeps the records of its own actions.' },
];
