export interface InstallMethod {
  key: string
  label: string
  cmd: string
}

export interface TrackStep {
  label: string
  expected: string
  cmd?: string
  methods?: InstallMethod[]
}

export interface QuizOption {
  text: string
  correct: boolean
}

export interface TrackQuiz {
  q: string
  opts: QuizOption[]
  explain: string
}

export interface TrackCheckpoint {
  cmd: string
  prompt: string
}

export interface Track {
  title: string
  time: string
  docs: string
  goal: string
  steps: TrackStep[]
  checkpoint: TrackCheckpoint
  quiz: TrackQuiz
  takeaway: string
}

export const TRACKS: Track[] = [
  {
    title: 'Install and hook your agent',
    time: '10 min',
    docs: 'cli-reference.md',
    goal: 'Install Gryph. Add hooks to your agent. Check the setup.',
    steps: [
      {
        label: 'Install the program. Select one method.',
        expected: 'Gryph is installed and available in your PATH.',
        methods: [
          {
            key: 'curl',
            label: 'curl | sh',
            cmd: 'curl -fsSL https://raw.githubusercontent.com/safedep/gryph/main/install.sh | sh',
          },
          { key: 'brew', label: 'homebrew', cmd: 'brew install safedep/tap/gryph' },
          { key: 'npm', label: 'npm', cmd: 'npm install -g @safedep/gryph' },
          { key: 'go', label: 'go', cmd: 'go install github.com/safedep/gryph/cmd/gryph@latest' },
        ],
      },
      {
        label: 'Preview the installation. It shows which files will change.',
        cmd: 'gryph install --dry-run',
        expected: 'Gryph lists the agents it found and the files it will change.',
      },
      {
        label: 'Install the hooks. Gryph saves a backup of your configuration first.',
        cmd: 'gryph install',
        expected: 'Gryph writes the hooks and saves a backup.',
      },
      {
        label: 'Check the version, agents, database, configuration, and schema.',
        cmd: 'gryph status && gryph doctor',
        expected: 'Gryph shows a registered agent. All checks pass.',
      },
    ],
    checkpoint: {
      cmd: 'gryph self-log',
      prompt: 'You see an install record. Gryph also records its own actions.',
    },
    quiz: {
      q: 'Does install change the agent program?',
      opts: [
        { text: 'Yes. It changes the program file.', correct: false },
        { text: 'No. It changes only the hook configuration.', correct: true },
      ],
      explain:
        'Gryph adds small hooks to the agent configuration. It does not change the program file.',
    },
    takeaway: 'Gryph adds small hooks. It does not change the agent program file.',
  },
  {
    title: 'Capture your first session',
    time: '15 min',
    docs: 'cli-reference.md',
    goal: 'Record the actions of an agent. Then view them with logs and sessions.',
    steps: [
      {
        label: 'Make a temporary test project.',
        cmd: 'mkdir -p /tmp/gryph-sandbox && cd /tmp/gryph-sandbox',
        expected: 'Use a clean directory for practice. Do not use a real project.',
      },
      {
        label: 'Start your agent in that directory. Give it a small task.',
        cmd: 'echo "hello gryph" > readme.txt',
        expected: 'The agent reads, writes, and runs commands. Gryph records each action.',
      },
      {
        label: 'View recent actions by session.',
        cmd: 'gryph logs --today',
        expected: 'Gryph shows read, write, and command actions for each session.',
      },
      {
        label: 'Open one session. Then watch new actions as they happen.',
        cmd: 'gryph sessions && gryph logs --follow',
        expected:
          'The command shows the full session. Use --follow to see new actions. Use --live for a full screen view.',
      },
    ],
    checkpoint: {
      cmd: 'gryph query --action file_write --today --count',
      prompt:
        'The count is more than zero. This shows that the hooks record actions after they run.',
    },
    quiz: {
      q: 'When do the hooks record an action?',
      opts: [
        { text: 'Only before it runs.', correct: false },
        { text: 'Before it runs and after it runs.', correct: true },
      ],
      explain: 'Gryph records each action before it runs and after it runs.',
    },
    takeaway: 'The hooks record each action before it runs and after it runs.',
  },
  {
    title: 'Query & export the audit trail',
    time: '20 min',
    docs: 'cli-automation.md',
    goal: 'Use filters, file differences, and exports to see what an agent did.',
    steps: [
      {
        label: 'Filter by action type.',
        cmd: 'gryph query --action file_write --today',
        expected: 'Types include file_read, file_write, command_exec, and network_request.',
      },
      {
        label: 'Filter by file name or command pattern.',
        cmd: 'gryph query --file "**/*.env" --today',
        expected: 'The --file and --command options accept patterns.',
      },
      {
        label: 'View one write action. See the file before and after.',
        cmd: 'gryph diff <event-id>',
        expected: 'Set the full logging level first. Use gryph cat for more detail.',
      },
      {
        label: 'Export the events to a JSONL file. Send the file to jq.',
        cmd: "gryph export --since 1w | jq -r '.action_type' | sort | uniq -c | sort -rn",
        expected:
          'You get a total for each action type. Gryph does not include sensitive events by default.',
      },
    ],
    checkpoint: {
      cmd: "gryph export --since 1w | jq -r '[.agent_name,.action_type]|@tsv' | sort | uniq -c",
      prompt: 'You can say which agent did which type of action.',
    },
    quiz: {
      q: 'Which command leaves out sensitive events by default?',
      opts: [
        { text: 'gryph export', correct: true },
        { text: 'gryph diff', correct: false },
      ],
      explain:
        'The export command does not include sensitive events unless you add --sensitive. Gryph never stores the content.',
    },
    takeaway: 'The export command and jq make most audit reports. You do not need to write code.',
  },
  {
    title: 'Privacy & configuration',
    time: '10 min',
    docs: 'security-policy.md',
    goal: 'Find sensitive files. Control how much detail Gryph records.',
    steps: [
      {
        label: 'Ask your agent to read the .env file. Gryph marks it as sensitive.',
        cmd: 'gryph export --since 1w --sensitive',
        expected: 'Gryph records the action. Gryph does not store the content.',
      },
      {
        label: 'Read the current logging level.',
        cmd: 'gryph config get logging.level',
        expected: 'The levels are minimal, standard, and full. Standard is the default.',
      },
      {
        label: 'Set the level to full to record file differences and context.',
        cmd: 'gryph config set logging.level full',
        expected: 'Gryph adds file differences, raw events, and conversation context.',
      },
      {
        label: 'Set the level back to standard when you finish.',
        cmd: 'gryph config set logging.level standard',
        expected: 'Gryph records difference counts, exit codes, and short output.',
      },
    ],
    checkpoint: {
      cmd: 'gryph config show --format json',
      prompt:
        'You can read the current configuration. Check that the privacy defaults are correct.',
    },
    quiz: {
      q: 'Does Gryph store the content of a sensitive file?',
      opts: [
        { text: 'Yes. Gryph encrypts it.', correct: false },
        { text: 'No. Gryph never stores it.', correct: true },
      ],
      explain:
        'Gryph protects sensitive files in the logging and in the policy. It never stores the content.',
    },
    takeaway: 'Gryph protects sensitive files. It never stores their content.',
  },
  {
    title: 'Enforce a security policy',
    time: '30 min',
    docs: 'security-policy.md',
    goal: 'Write a YAML policy. Test it. Then apply it.',
    steps: [
      {
        label: 'Write a candidate rule file in your sandbox directory.',
        cmd: 'gryph policy init /tmp/gryph-sandbox/candidate.yml',
        expected: 'Gryph writes the example policy to that path. The file is a candidate. It is not active yet.',
      },
      {
        label: 'Check one rule file. Do not merge it yet.',
        cmd: 'gryph policy validate --file /tmp/gryph-sandbox/candidate.yml',
        expected: 'Gryph checks the one file and reports errors.',
      },
      {
        label: 'Test the rule before you install it.',
        cmd: 'gryph policy test --file /tmp/gryph-sandbox/candidate.yml --action command_exec --command "rm -rf /"',
        expected: 'The result is block. Gryph shows the message.',
      },
      {
        label: 'Install the rule. Then turn on enforcement.',
        cmd: 'gryph policy install /tmp/gryph-sandbox/candidate.yml && gryph config set policy.enabled true',
        expected: 'Now every agent action passes through the policy.',
      },
    ],
    checkpoint: {
      cmd: 'gryph policy receipts --decision block',
      prompt: 'You see one block receipt or more. Gryph keeps the decision record.',
    },
    quiz: {
      q: 'Which effect is the safe way to start?',
      opts: [
        { text: 'Block every action at once.', correct: false },
        { text: 'Warn first. Change to block after you trust the rule.', correct: true },
      ],
      explain:
        'Rules can allow, warn, guide, block, escalate, or defer. Start with warn. Change to block later.',
    },
    takeaway: 'Start with warn. Change to block after you trust the rule.',
  },
  {
    title: 'Audit integrity and receipts',
    time: '15 min',
    docs: 'security-policy.md',
    goal: 'Sign the receipts. Check the chain. Export the receipts for an audit on another machine.',
    steps: [
      {
        label: 'Make an Ed25519 key pair for signing.',
        cmd: 'gryph policy keys generate',
        expected: 'Gryph stores the keys in the keys directory. Receipts now include signatures.',
      },
      {
        label: 'Check the chain for all sessions.',
        cmd: 'gryph policy receipts --verify --all-sessions',
        expected: 'The exit code is not zero if the chain is broken.',
      },
      {
        label: 'Export signed receipts. Check them on another machine.',
        cmd: 'gryph policy receipts export --include-signatures | gryph policy receipts verify-log --input -',
        expected: 'The signatures protect the export from changes during transfer.',
      },
      {
        label: 'View the context counts for each session.',
        cmd: 'gryph policy context --session <id>',
        expected:
          'These are the same values that CEL conditions read, such as total_actions and files_written.',
      },
    ],
    checkpoint: {
      cmd: 'gryph policy receipts --format json --limit 1',
      prompt: 'You can show one signed receipt. You can say what the chain proves.',
    },
    quiz: {
      q: 'What does the chain alone prove?',
      opts: [
        { text: 'The data is encrypted.', correct: false },
        { text: 'A change to the data in the database is found.', correct: true },
      ],
      explain:
        'The chain finds a change to the data in the database. The signatures also protect the export during transfer.',
    },
    takeaway:
      'Receipts form a chain for each session. Signatures protect the export during transfer.',
  },
  {
    title: 'Cost, stats and agent comparison',
    time: '10 min',
    docs: 'cost.md',
    goal: 'Track token use. Compare agents. Open the statistics dashboard.',
    steps: [
      {
        label: 'View the cost for today.',
        cmd: 'gryph cost --today',
        expected: 'Gryph prices the token use from models.dev.',
      },
      {
        label: 'Group the cost by model, day, or agent.',
        cmd: 'gryph cost --since 1w --by model',
        expected: 'Use --sync to add older sessions. Use --force to compute the cost again.',
      },
      {
        label: 'Open the statistics dashboard for one agent.',
        cmd: 'gryph stats --since 7d --agent claude-code',
        expected: 'Gryph shows sessions, actions, and cost for the time range.',
      },
    ],
    checkpoint: {
      cmd: 'gryph cost --by agent',
      prompt: 'You can say which agent costs more this week.',
    },
    quiz: {
      q: 'Where do the model prices come from?',
      opts: [
        { text: 'A Gryph cloud service.', correct: false },
        { text: 'The models.dev source.', correct: true },
      ],
      explain:
        'Gryph reads the session records. It prices the token use with models.dev at the end of the session.',
    },
    takeaway:
      'Gryph reports token use with prices from models.dev. The cost is ready at the end of the session.',
  },
  {
    title: 'Housekeeping and advanced',
    time: '15 min',
    docs: 'cli-reference.md',
    goal: 'Control data retention. View the Gryph audit trail. See the advanced functions.',
    steps: [
      {
        label: 'Preview the retention cleanup. No data changes.',
        cmd: 'gryph retention cleanup --dry-run',
        expected: 'Gryph reports the old rows it would remove. Each data type has its own retention period.',
      },
      {
        label: 'Run the cleanup to remove old rows.',
        cmd: 'gryph retention cleanup',
        expected: 'Gryph removes old events. Receipts follow their own retention period, 365 days by default.',
      },
      {
        label: 'Review every action that Gryph made on itself.',
        cmd: 'gryph self-log',
        expected: 'You see install, uninstall, config set, and cleanup records.',
      },
      {
        label: 'See the advanced functions: escalate, defer, and SOC export.',
        cmd: 'gryph policy deferrals',
        expected: 'Escalate stops the agent. Defer holds the action for an operator to review.',
      },
    ],
    checkpoint: {
      cmd: 'gryph uninstall --dry-run',
      prompt: 'You can say how to remove the hooks. You can say how to delete the data with --purge.',
    },
    quiz: {
      q: 'What does the retention cleanup keep?',
      opts: [
        { text: 'Every receipt forever.', correct: false },
        { text: 'Receipts for their own retention period, 365 days by default.', correct: true },
      ],
      explain:
        'Events, receipts, and deferred actions each have their own retention period. Gryph always keeps the self-audit trail.',
    },
    takeaway:
      'Retention removes old rows per data type. Receipts use their own period, 365 days by default.',
  },
]
