import { uid, providerOf } from '../lib/helpers';
import { MODELS } from '../lib/constants';
import type { Workspace } from './types';


export function seedAcme() {
  return {
    id: 'acme', name: 'Acme Corp', sub: 'acme', tz: 'America/Los_Angeles',
    defaultModel: 'claude-sonnet-5', retention: '90 days',
    agents: [
      { id: 'a-atlas', name: 'Atlas', model: 'claude-sonnet-5', temp: 0.3, autonomy: 'approval', channelPost: true,
        role: 'Ops coordinator — triages alerts, runs playbooks, writes postmortems',
        status: 'running', tools: ['web', 'shell', 'api', 'db'], lastActive: '2m ago',
        skills: ['research', 'summarize', 'postmortem'],
        prompt: 'You are Atlas, on-call ops coordinator for Acme. Triage alerts, correlate with deploys, page only when SLO burn demands it. Always end with a proposed next action.' },
      { id: 'a-beacon', name: 'Beacon', model: 'claude-opus-5', temp: 0.5, autonomy: 'approval', channelPost: false,
        role: 'Research analyst — deep briefs with citations',
        status: 'idle', tools: ['web', 'files'], lastActive: '1h ago',
        skills: ['research', 'writing', 'summarize'],
        prompt: 'You are Beacon. Produce research briefs with sources, clear tradeoffs, and a recommendation. Never pad.' },
      { id: 'a-warden', name: 'Warden', model: 'claude-sonnet-5', temp: 0.2, autonomy: 'approval', channelPost: true,
        role: 'SRE — incident response and error budgets',
        status: 'error', tools: ['shell', 'api', 'db'], lastActive: '3h ago',
        skills: ['postmortem', 'summarize'],
        prompt: 'You are Warden. Own incident timeline hygiene, error budgets, and on-call handoffs. Escalate stalled acks.' },
      { id: 'a-quill', name: 'Quill', model: 'claude-haiku-4-5', temp: 0.6, autonomy: 'approval', channelPost: true,
        role: 'Writer — release notes, changelogs, docs polish',
        status: 'idle', tools: ['files', 'web'], lastActive: 'Yesterday',
        skills: ['writing', 'summarize'],
        prompt: 'You are Quill. Turn commit history into human release notes. Flag breaking changes first.' },
      { id: 'a-ledger', name: 'Ledger', model: 'llama-4-maverick', temp: 0.1, autonomy: 'suggest', channelPost: false,
        role: 'Finance analyst — spend summaries, invoice extraction',
        status: 'idle', tools: ['files', 'db'], lastActive: '2d ago',
        skills: ['data', 'summarize'],
        prompt: 'You are Ledger. Extract structure from invoices, reconcile against the ledger, surface variance over 2%.' },
      { id: 'a-scout', name: 'Scout', model: 'claude-haiku-4-5', temp: 0.3, autonomy: 'full', channelPost: true,
        role: 'Support triage — classifies tickets, drafts replies',
        status: 'running', tools: ['api', 'web'], lastActive: 'just now',
        skills: ['summarize'],
        prompt: 'You are Scout. Classify inbound tickets, auto-reply to known issues, escalate angry or billing-related ones to a human.' }
    ],
    // Channels are server-only (integrate-agent-channels): they hydrate from
    // the API via loadChannels and are never seeded. Room rendering code
    // stays; an empty real feed is the correct state.
    channels: [],
    people: [
      { id: 'p-dana', name: 'Dana Kwan', presence: 'online' },
      { id: 'p-milo', name: 'Milo Park', presence: 'online' },
      { id: 'p-sasha', name: 'Sasha Reyes', presence: 'away' }
    ],
    threads: {
      'a-atlas': [
        { id: 'm1', author: 'agent', ts: '7:00 AM', scheduler: 'morning-digest', text: 'Morning digest — 14 alerts overnight (13 auto-resolved, 1 paged), 3 deploys (1 rolled back), 22 tickets closed. The full report is pinned in #ops.' },
        { id: 'm2', author: 'you', ts: '9:12 AM', text: 'Atlas, summarize anything that happened overnight in #incidents' },
        { id: 'm3', author: 'agent', ts: '9:12 AM',
          tools: [
            { name: 'grafana.query', args: 'alert: rate_5xx > 1%', ms: 820, res: 'series api-gateway 5xx: 1 firing (02:14–02:51), p99 latency nominal' },
            { name: 'pagerduty.incidents', args: 'window: 12h', ms: 410, res: '1 incident: INC-2291 api-gateway-5xx, auto-resolved, no pages issued' }
          ],
          text: 'One incident: api-gateway-5xx fired at 02:14 and auto-resolved by 02:51. The trigger lines up with the deploys in #release at 01:58 — the rollback landed at 02:40, which likely resolved it. No pages were issued. Want me to draft a postmortem stub in #incidents?' },
        { id: 'm4', author: 'you', ts: '9:14 AM', text: 'Yes — draft it and ask Dana to review' },
        { id: 'm5', author: 'agent', ts: '9:14 AM',
          tools: [{ name: 'files.write', args: 'postmortems/2026-08-25-api-5xx.md', ms: 350, res: 'wrote 2.4 KB — postmortems/2026-08-25-api-5xx.md' }],
          text: 'Done. Stub posted to #incidents with the timeline pre-filled from logs, and Dana was asked to review. I\'ll nudge her again in 4 hours if it\'s still untouched.' }
      ],
      'a-beacon': [
        { id: 'm6', author: 'you', ts: 'Yesterday 4:20 PM', text: 'Brief me on embedded vector DBs for under 50 GB with strong metadata filtering' },
        { id: 'm7', author: 'agent', ts: 'Yesterday 4:23 PM',
          tools: [
            { name: 'web.search', args: 'q: filtered ANN benchmark 2026', ms: 1240, res: '8 results — top: ann-benchmarks/filtered-2026, Qdrant engineering blog' },
            { name: 'web.search', args: 'q: pgvector vs qdrant metadata filters', ms: 980, res: '6 results — top: pgvector HNSW filtering notes, Supabase comparison' }
          ],
          text: 'Short version: pgvector is the pragmatic default at this scale — filtering stays in Postgres where the data already lives. Qdrant wins the filtered-ANN benchmarks; LanceDB is the lean local option. Weaviate and Milvus pay off closer to the 100 GB+ range. I have benchmarks and pricing in a 6-page brief — want it posted to #engineering?' }
      ],
      'a-warden': [
        { id: 'm8', author: 'agent', ts: '3:02 AM',
          tools: [{ name: 'pagerduty.ack', args: 'INC-2291', ms: 0, error: '401 — token expired' }],
          text: 'Failed to acknowledge INC-2291: the PagerDuty token expired. I\'ve paused the on-call handoff job until the credential is rotated in Settings → Integrations.' }
      ],
      'a-quill': [
        { id: 'm9', author: 'you', ts: 'Friday 5:31 PM', text: 'Sweep the week and cut v2.14 release notes' },
        { id: 'm10', author: 'agent', ts: 'Friday 5:33 PM',
          tools: [{ name: 'github.commits', args: 'repo: acme/platform since: 7d', ms: 640, res: '41 commits across 19 PRs — 3 features, 9 fixes, 2 breaking' }],
          text: 'v2.14 notes drafted — 41 commits: 3 features, 9 fixes, 2 breaking (both flagged at the top). Posted to #release and tagged for review.' }
      ],
      'a-ledger': [],
      'a-scout': [
        { id: 'm11', author: 'agent', ts: '9:26 AM', scheduler: 'inbox-triage', text: 'Triage pass: 31 new tickets — 18 routed to Billing (auto-replied), 9 to Bugs, 4 escalated to a human. Three replies are drafted and waiting in the approval queue.' }
      ],
      'p-dana': [
        { id: 'm15', author: 'other', name: 'Dana Kwan', ts: '9:15 AM', text: 'Saw the postmortem stub — nice. I\'ll review after standup.' }
      ],
      'p-milo': [],
      'p-sasha': []
    },
    // Schedules and runs are server-only (integrate-scheduler): they hydrate
    // from the live API (loadSchedules / the runs screen) and are never
    // seeded — an empty real list is the correct state, same as channels.
    schedules: [],
    runs: [],
    members: [
      { id: 'me', name: 'You', email: 'you@acme.dev', role: 'Owner' },
      { id: 'p-dana', name: 'Dana Kwan', email: 'dana@acme.dev', role: 'Admin' },
      { id: 'p-milo', name: 'Milo Park', email: 'milo@acme.dev', role: 'Member' },
      { id: 'p-sasha', name: 'Sasha Reyes', email: 'sasha@acme.dev', role: 'Member' }
    ],
    integrations: [
      { id: 'slack', name: 'Slack', detail: '#ops · 4 channels', connected: true },
      { id: 'github', name: 'GitHub', detail: 'acme/platform · 3 repos', connected: true },
      { id: 'pagerduty', name: 'PagerDuty', detail: 'Escalation: Platform — token expired', connected: true },
      { id: 'postgres', name: 'Postgres', detail: 'prod read replica', connected: true },
      { id: 'linear', name: 'Linear', detail: 'Sync issues to agent tasks', connected: false },
      { id: 'notion', name: 'Notion', detail: 'Publish briefs to docs', connected: false }
    ],
    skillLib: [
      { id: 'research', name: 'web-research', version: '2.4.1', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'code', name: 'code-execution', version: '1.9.0', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'data-analysis', version: '2.1.3', enabled: true, tier: 'workspace', source: 'upload',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.',
        dependencies: { tools: ['execute'] } },
      { id: 'writing', name: 'writing-editing', version: '3.0.0', enabled: true, tier: 'workspace', source: 'authored',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'summarize', name: 'summarization', version: '1.4.2', enabled: true, tier: 'workspace', source: 'fork',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'vision', name: 'vision', version: '1.2.0', enabled: false, tier: 'workspace', source: 'upload',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.',
        dependencies: { binaries: ['pdftotext'] } },
      { id: 'postmortem', name: 'postmortem-writer', version: '0.3.1', enabled: true, tier: 'workspace', source: 'authored',
        desc: 'Turns an incident timeline into a review-ready postmortem with contributing factors.' }
    ],
    keys: [
      { id: 'k1', name: 'production-gateway', masked: 'oc_live_••••••••7f3a', full: 'oc_live_9t2mKc7QwZr4LpHx7f3a', created: 'Mar 2026' },
      { id: 'k2', name: 'ci-deploy', masked: 'oc_live_••••••••a21b', full: 'oc_live_3bVn8sYqTfE2mJdRa21b', created: 'Jun 2026' }
    ],
    providers: [
      {
        id: 'prov_acme_anthropic',
        workspace_id: 'acme',
        type: 'anthropic',
        name: 'Anthropic Production',
        base_url: '',
        key_set: true,
        key_hint: '7f3a',
        enabled: true,
        created_at: '2026-08-01T00:00:00Z',
        updated_at: '2026-08-01T00:00:00Z',
      },
      {
        id: 'prov_acme_openai',
        workspace_id: 'acme',
        type: 'openai',
        name: 'OpenAI Primary',
        base_url: '',
        key_set: true,
        key_hint: '9k2b',
        enabled: true,
        created_at: '2026-08-01T00:00:00Z',
        updated_at: '2026-08-01T00:00:00Z',
      }
    ]
  };
}


export function seedGlobex() {
  return {
    id: 'globex', name: 'Globex Inc', sub: 'globex', tz: 'Europe/Berlin',
    defaultModel: 'claude-haiku-4-5', retention: '30 days',
    agents: [
      { id: 'a-herald', name: 'Herald', model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: true,
        role: 'Comms coordinator — standup notes and announcements',
        status: 'idle', tools: ['files', 'api'], lastActive: '1d ago',
        skills: ['writing', 'summarize'],
        prompt: 'You are Herald. Turn ticket activity into crisp standup notes. Never invent status.' },
      { id: 'a-tally', name: 'Tally', model: 'llama-4-scout', temp: 0.1, autonomy: 'full', channelPost: true,
        role: 'Data analyst — ETL checks and weekly metrics',
        status: 'running', tools: ['db', 'api'], lastActive: 'just now',
        skills: ['data', 'summarize'],
        prompt: 'You are Tally. Verify pipeline freshness and row counts against a 7-day baseline. Alert on drift over 5%.' },
      { id: 'a-forge', name: 'Forge', model: 'claude-sonnet-5', temp: 0.3, autonomy: 'approval', channelPost: false,
        role: 'Dev agent — PR review and test triage',
        status: 'idle', tools: ['shell', 'api'], lastActive: '6h ago',
        skills: ['code', 'summarize'],
        prompt: 'You are Forge. Review PRs for correctness first, style second. Run the test suite before commenting.' }
    ],
    // Channels are server-only (integrate-agent-channels) — never seeded.
    channels: [],
    people: [{ id: 'p-ravi', name: 'Ravi Shah', presence: 'online' }],
    threads: {
      'a-herald': [],
      'a-tally': [
        { id: 'g1', author: 'agent', ts: '2:00 AM', scheduler: 'nightly-etl', text: 'ETL check: 12/12 pipelines green. Row counts within 2% of the 7-day average. No action needed.' }
      ],
      'a-forge': [
        { id: 'g2', author: 'you', ts: 'Yesterday', text: 'Review the auth refactor PR when tests go green' },
        { id: 'g3', author: 'agent', ts: 'Yesterday', tools: [{ name: 'shell.run', args: 'pnpm test auth/', ms: 15800, res: '142 passed, 2 failed — token-expiry and refresh race' }], text: 'Reviewed. Two real findings: a missing token-expiry test and a race in refresh. Left comments on the diff — rest looks clean.' }
      ],
      'p-ravi': []
    },
    schedules: [],
    runs: [],
    members: [
      { id: 'me', name: 'You', email: 'you@globex.io', role: 'Owner' },
      { id: 'p-ravi', name: 'Ravi Shah', email: 'ravi@globex.io', role: 'Member' }
    ],
    integrations: [
      { id: 'slack', name: 'Slack', detail: '#general · 1 channel', connected: true },
      { id: 'github', name: 'GitHub', detail: 'globex/etl', connected: true },
      { id: 'linear', name: 'Linear', detail: 'Sync issues to agent tasks', connected: false },
      { id: 'postgres', name: 'Postgres', detail: 'warehouse read replica', connected: false }
    ],
    skillLib: [
      { id: 'research', name: 'web-research', version: '2.4.1', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'code', name: 'code-execution', version: '1.9.0', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'data-analysis', version: '2.1.3', enabled: true, tier: 'workspace', source: 'git',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.',
        dependencies: { tools: ['execute'] } },
      { id: 'writing', name: 'writing-editing', version: '3.0.0', enabled: true, tier: 'workspace', source: 'authored',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'summarize', name: 'summarization', version: '1.4.2', enabled: true, tier: 'workspace', source: 'fork',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'vision', name: 'vision', version: '1.2.0', enabled: false, tier: 'workspace', source: 'upload',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.',
        dependencies: { binaries: ['pdftotext'] } }
    ],
    keys: [
      { id: 'gk1', name: 'default', masked: 'oc_live_••••••••e5c9', full: 'oc_live_7dXk2pQmZn8vLtEe5c9', created: 'Jul 2026' }
    ],
    providers: [
      {
        id: 'prov_globex_anthropic',
        workspace_id: 'globex',
        type: 'anthropic',
        name: 'Anthropic Main',
        base_url: '',
        key_set: true,
        key_hint: 'e5c9',
        enabled: true,
        created_at: '2026-08-01T00:00:00Z',
        updated_at: '2026-08-01T00:00:00Z',
      }
    ]
  };
}

/* ---- session model: threads[cid] = { active, list: [{ id, title, updated, messages }] } ---- */
export const EXTRA_SESSIONS = {
  'a-atlas': [
    { title: 'Deploy correlation — 5xx spike', updated: 'Yesterday', messages: [
      { id: 'x1', author: 'you', ts: 'Yesterday 11:20 AM', text: 'Correlate this week\'s 5xx spikes with deploys' },
      { id: 'x2', author: 'agent', ts: 'Yesterday 11:22 AM',
        tools: [{ name: 'grafana.query', args: 'alert: rate_5xx window 7d', ms: 760, res: '2 firing windows this week, both auto-resolved under 40m' }],
        text: 'Two spikes this week. Tuesday\'s lines up with the v2.12 rollout (reverted 20:41); today\'s with the v2.14 rollback. Both resolved within 40 minutes of the revert.' }
    ] }
  ],
  'a-beacon': [
    { title: 'Filtered-ANN benchmark sources', updated: 'Monday', messages: [
      { id: 'x3', author: 'you', ts: 'Monday 10:05 AM', text: 'Pull the benchmark papers cited in your vector DB brief' },
      { id: 'x4', author: 'agent', ts: 'Monday 10:09 AM',
        tools: [{ name: 'web.search', args: 'q: filtered ANN benchmark 2026', ms: 1100, res: '8 results — benchmark summary extracted to run notes' }],
        text: 'Three primary sources: the Qdrant filtered-search benchmark (Feb 2026), pgvector HNSW filter tests, and the LanceDB TPC-H annex — linked in the brief appendix with repro notes.' }
    ] }
  ],
  'a-ledger': Array.from({ length: 100 }, (_, i) => ({
    title: 'Reconciliation run #' + String(100 - i).padStart(3, '0'),
    updated: i === 0 ? 'Today' : (i === 1 ? 'Yesterday' : i + 'd ago'),
    messages: [
      { id: 'lgu' + i, author: 'you', ts: i === 0 ? 'Today 6:00 AM' : (i === 1 ? 'Yesterday 6:00 AM' : i + 'd ago'), text: 'Run the daily reconciliation sweep' },
      { id: 'lga' + i, author: 'agent', ts: i === 0 ? 'Today 6:00 AM' : (i === 1 ? 'Yesterday 6:00 AM' : i + 'd ago'),
        tools: [{ name: 'db.query', args: 'ledger unreconciled > $50', ms: 380 + (i % 7) * 25, res: '3 rows — oldest entry 6 days, total $412.80' }],
        text: 'Sweep complete — ' + (i % 3) + ' exceptions flagged, all under the $50 threshold. Variance ' + (0.1 + (i % 5) * 0.1).toFixed(1) + '%, within tolerance.' }
    ]
  }))
};

export function withSessions(t: any) {
  const threads = {};
  Object.entries((t as any).threads || {}).forEach(([cid, msgs]: any) => {
    if (!msgs || msgs.length === 0) {
      const extras0 = (EXTRA_SESSIONS[cid] || []).map((s: any) => ({ ...s, id: uid('s'), messages: s.messages.slice() }));
      threads[cid] = { active: extras0.length ? extras0[0].id : null, list: extras0 };
      return;
    }
    const firstYou = msgs.find((m: any) => m.author === 'you');
    const title = firstYou
      ? (firstYou.text.length > 42 ? firstYou.text.slice(0, 42) + '…' : firstYou.text)
      : ('Scheduled · ' + (msgs[0].scheduler || 'digest')).slice(0, 48);
    const main = { id: uid('s'), title, updated: (msgs[msgs.length - 1] || {}).ts || '', messages: msgs };
    const extras = (EXTRA_SESSIONS[cid] || []).map((s: any) => ({ ...s, id: uid('s'), messages: s.messages.slice() }));
    threads[cid] = { active: main.id, list: [main].concat(extras) };
  });
  return {
    ...t,
    agents: (t.agents || []).map((a: any) => ({
      ...a,
      provider: a.provider || providerOf(a.model),
      skills: a.skills || ['research', 'summarize']
    })),
    providers: t.providers || [],
    threads
  };
}

export const seedDb = () => ({ acme: withSessions(seedAcme()), globex: withSessions(seedGlobex()) });

export function blankTenant({ name, sub, tz, starter  }: any): Workspace {
  const aid = uid('a');
  return {
    id: 'ws_' + sub.replace(/-/g, '_'),
    name, sub, tz, defaultModel: MODELS[0], retention: '90 days',
    agents: starter ? [{
      id: aid, name: 'Guide', model: 'claude-haiku-4-5', temp: 0.4, autonomy: 'suggest', channelPost: true,
      role: 'Starter agent — answers questions, searches the web, shows the ropes',
      status: 'idle', tools: ['web'], lastActive: 'just now', skills: [],
      prompt: 'You are Guide, the starter agent for a brand-new OnClaw workspace. Answer questions about the workspace, demonstrate tool use, and suggest what to deploy next.'
    }] : [],
    // Server-only (integrate-agent-channels) — fresh workspaces start with no
    // local channel rows; the real list hydrates via loadChannels.
    channels: [],
    people: [],
    threads: starter ? {
      [aid]: { active: 's0', list: [{ id: 's0', title: 'Chat', updated: 'just now', messages: [
        { id: uid('m'), author: 'agent', ts: 'just now', text: 'Welcome to ' + name + '. I\'m Guide, your starter agent — ask me anything or put me to work with a web search. When you\'re ready, deploy specialists from the Agents view; everything about me lives in Settings → Agents.' }
      ] }] }
    } : {},
    schedules: [], runs: [],
    members: [{ id: 'me', name: 'You', email: 'you@' + sub + '.dev', role: 'Owner' }],
    integrations: [
      { id: 'slack', name: 'Slack', detail: 'Route digests to a channel', connected: false },
      { id: 'github', name: 'GitHub', detail: 'Watch repos and review PRs', connected: false },
      { id: 'pagerduty', name: 'PagerDuty', detail: 'Page on SLO burn', connected: false },
      { id: 'linear', name: 'Linear', detail: 'Sync issues to agent tasks', connected: false },
      { id: 'notion', name: 'Notion', detail: 'Publish briefs to docs', connected: false },
      { id: 'postgres', name: 'Postgres', detail: 'Query a read replica', connected: false }
    ],
    skillLib: [
      { id: 'research', name: 'web-research', version: '2.4.1', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'summarize', name: 'summarization', version: '1.4.2', enabled: true, tier: 'workspace', source: 'authored',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'code', name: 'code-execution', version: '1.9.0', enabled: true, tier: 'system', source: 'system', locked: true,
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'data-analysis', version: '2.1.3', enabled: false, tier: 'workspace', source: 'git',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.',
        dependencies: { tools: ['execute'] } },
      { id: 'writing', name: 'writing-editing', version: '3.0.0', enabled: false, tier: 'workspace', source: 'authored',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'vision', name: 'vision', version: '1.2.0', enabled: false, tier: 'workspace', source: 'upload',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.',
        dependencies: { binaries: ['pdftotext'] } }
    ],
    keys: [],
    providers: []
  };
}


