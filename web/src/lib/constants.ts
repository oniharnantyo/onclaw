export const PROVIDER_TYPES = [
  { id: 'openai', label: 'OpenAI' },
  { id: 'anthropic', label: 'Anthropic' },
  { id: 'gemini', label: 'Gemini' },
  { id: 'openrouter', label: 'OpenRouter' },
  { id: 'openai-compatible', label: 'OpenAI-compatible' },
  { id: 'anthropic-compatible', label: 'Anthropic-compatible' },
] as const;

export const PROVIDER_MODELS: Record<string, string[]> = {
  openai: ['gpt-4o', 'gpt-4o-mini', 'o1', 'o3-mini', 'gpt-4-turbo'],
  anthropic: ['claude-sonnet-5', 'claude-opus-5', 'claude-haiku-4-5', 'claude-3-5-sonnet-20241022', 'claude-3-5-haiku-20241022'],
  gemini: ['gemini-2.0-flash', 'gemini-1.5-pro', 'gemini-1.5-flash'],
  openrouter: [
    'anthropic/claude-3.5-sonnet',
    'openai/gpt-4o',
    'meta-llama/llama-3.3-70b-instruct',
    'google/gemini-2.0-flash-001',
    'deepseek/deepseek-r1',
  ],
  'openai-compatible': [],
  'anthropic-compatible': [],
};

export const MODELS = [
  'claude-sonnet-5',
  'claude-opus-5',
  'claude-haiku-4-5',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-2.0-flash',
  'llama-4-maverick',
  'llama-4-scout',
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
  { cmd: '/compact', desc: "Compact this conversation's context" }
];
