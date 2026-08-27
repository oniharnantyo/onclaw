export const MODELS = ['claude-sonnet-5', 'claude-opus-5', 'claude-haiku-4-5', 'llama-4-maverick', 'llama-4-scout'];
export const PROVIDERS = [
  { id: 'anthropic', label: 'Anthropic', models: ['claude-opus-5', 'claude-sonnet-5', 'claude-haiku-4-5'] },
  { id: 'meta', label: 'Meta', models: ['llama-4-maverick', 'llama-4-scout'] }
];
export const SKILLS = [
  { id: 'research', label: 'Deep research' },
  { id: 'code', label: 'Code execution' },
  { id: 'data', label: 'Data analysis' },
  { id: 'writing', label: 'Writing & editing' },
  { id: 'vision', label: 'Vision' },
  { id: 'summarize', label: 'Summarization' },
  { id: 'postmortem', label: 'Postmortem writer' }
];
export const MCP_SERVERS = [
  { id: 'github', label: 'GitHub MCP', detail: 'repos, issues, pull requests' },
  { id: 'postgres', label: 'Postgres MCP', detail: 'read-only SQL' },
  { id: 'slack', label: 'Slack MCP', detail: 'channels & messages' },
  { id: 'filesystem', label: 'Filesystem MCP', detail: 'workspace files' },
  { id: 'memory', label: 'Memory MCP', detail: 'persistent notes' },
  { id: 'browser', label: 'Browser MCP', detail: 'headless browsing' }
];
export const TOOLS = [
  { id: 'web', label: 'Web search' },
  { id: 'files', label: 'Files' },
  { id: 'shell', label: 'Shell' },
  { id: 'api', label: 'HTTP APIs' },
  { id: 'db', label: 'Database' }
];
export const STATUS = {
  running: { dot: 'bg-success', label: 'Running', live: true },
  idle: { dot: 'bg-muted', label: 'Idle' },
  error: { dot: 'bg-danger', label: 'Needs attention' }
};
export const REPLY_TEMPLATES = [
  'Working on it. I\'ll pull the freshest data first and report back in this thread — anything that needs a decision gets flagged to you.',
  'Got it. Scoping it against my playbook now. If it stays under the autonomy threshold I\'ll just do it and summarize the diff.',
  'Queued. Runs like this usually land in under a minute. I\'ll post the result here and mirror a digest to the bound channel.'
];
export const MENTION_REPLIES = [
  'On it — pulling the relevant data now and I\'ll report back in this thread.',
  'Taking this. I\'ll correlate with the latest runs and post findings here.',
  'Got the ping — digging in now. If anything needs a decision I\'ll flag it with context.'
];
export const COMMANDS = [
  { cmd: '/tools', desc: 'List this agent’s tools' },
  { cmd: '/model', desc: 'Show the model this agent runs on' },
  { cmd: '/schedule', desc: 'Open the cron editor' },
  { cmd: '/reset', desc: 'Clear this thread' },
  { cmd: '/help', desc: 'Show commands' }
];
