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
        skills: ['research', 'summarize', 'postmortem'], mcp: ['github', 'postgres', 'slack'],
        prompt: 'You are Atlas, on-call ops coordinator for Acme. Triage alerts, correlate with deploys, page only when SLO burn demands it. Always end with a proposed next action.' },
      { id: 'a-beacon', name: 'Beacon', model: 'claude-opus-5', temp: 0.5, autonomy: 'approval', channelPost: false,
        role: 'Research analyst — deep briefs with citations',
        status: 'idle', tools: ['web', 'files'], lastActive: '1h ago',
        skills: ['research', 'writing', 'summarize'], mcp: ['browser'],
        prompt: 'You are Beacon. Produce research briefs with sources, clear tradeoffs, and a recommendation. Never pad.' },
      { id: 'a-warden', name: 'Warden', model: 'claude-sonnet-5', temp: 0.2, autonomy: 'approval', channelPost: true,
        role: 'SRE — incident response and error budgets',
        status: 'error', tools: ['shell', 'api', 'db'], lastActive: '3h ago',
        skills: ['postmortem', 'summarize'], mcp: ['postgres'],
        prompt: 'You are Warden. Own incident timeline hygiene, error budgets, and on-call handoffs. Escalate stalled acks.' },
      { id: 'a-quill', name: 'Quill', model: 'claude-haiku-4-5', temp: 0.6, autonomy: 'approval', channelPost: true,
        role: 'Writer — release notes, changelogs, docs polish',
        status: 'idle', tools: ['files', 'web'], lastActive: 'Yesterday',
        skills: ['writing', 'summarize'], mcp: ['github'],
        prompt: 'You are Quill. Turn commit history into human release notes. Flag breaking changes first.' },
      { id: 'a-ledger', name: 'Ledger', model: 'llama-4-maverick', temp: 0.1, autonomy: 'suggest', channelPost: false,
        role: 'Finance analyst — spend summaries, invoice extraction',
        status: 'idle', tools: ['files', 'db'], lastActive: '2d ago',
        skills: ['data', 'summarize'], mcp: ['postgres'],
        prompt: 'You are Ledger. Extract structure from invoices, reconcile against the ledger, surface variance over 2%.' },
      { id: 'a-scout', name: 'Scout', model: 'claude-haiku-4-5', temp: 0.3, autonomy: 'full', channelPost: true,
        role: 'Support triage — classifies tickets, drafts replies',
        status: 'running', tools: ['api', 'web'], lastActive: 'just now',
        skills: ['summarize'], mcp: ['slack'],
        prompt: 'You are Scout. Classify inbound tickets, auto-reply to known issues, escalate angry or billing-related ones to a human.' }
    ],
    channels: [
      { id: 'c-ops', name: 'ops', purpose: 'Production ops & alerting', agentId: 'a-atlas', unread: 3, members: ['a-atlas', 'a-warden', 'p-dana'] },
      { id: 'c-incidents', name: 'incidents', purpose: 'Incident channels & postmortems', agentId: 'a-warden', unread: 0, members: ['a-warden', 'a-atlas', 'p-milo'] },
      { id: 'c-engineering', name: 'engineering', purpose: 'Platform engineering', agentId: 'a-beacon', unread: 0, members: ['a-beacon', 'p-milo', 'p-sasha'] },
      { id: 'c-release', name: 'release', purpose: 'Deploys & release notes', agentId: 'a-quill', unread: 0, members: ['a-quill', 'p-dana'] },
      { id: 'c-general', name: 'general', purpose: 'Company-wide', agentId: 'a-scout', unread: 12, members: ['a-scout', 'a-atlas', 'a-beacon', 'p-dana', 'p-milo', 'p-sasha'] }
    ],
    people: [
      { id: 'p-dana', name: 'Dana Kwan', presence: 'online' },
      { id: 'p-milo', name: 'Milo Park', presence: 'online' },
      { id: 'p-sasha', name: 'Sasha Reyes', presence: 'away' }
    ],
    threads: {
      'a-atlas': [
        { id: 'm1', author: 'agent', ts: '7:00 AM', cron: 'morning-digest', text: 'Morning digest — 14 alerts overnight (13 auto-resolved, 1 paged), 3 deploys (1 rolled back), 22 tickets closed. The full report is pinned in #ops.' },
        { id: 'm2', author: 'you', ts: '9:12 AM', text: 'Atlas, summarize anything that happened overnight in #incidents' },
        { id: 'm3', author: 'agent', ts: '9:12 AM',
          tools: [
            { name: 'grafana.query', args: 'alert: rate_5xx > 1%', ms: 820 },
            { name: 'pagerduty.incidents', args: 'window: 12h', ms: 410 }
          ],
          text: 'One incident: api-gateway-5xx fired at 02:14 and auto-resolved by 02:51. The trigger lines up with the deploys in #release at 01:58 — the rollback landed at 02:40, which likely resolved it. No pages were issued. Want me to draft a postmortem stub in #incidents?' },
        { id: 'm4', author: 'you', ts: '9:14 AM', text: 'Yes — draft it and ask Dana to review' },
        { id: 'm5', author: 'agent', ts: '9:14 AM',
          tools: [{ name: 'files.write', args: 'postmortems/2026-08-25-api-5xx.md', ms: 350 }],
          text: 'Done. Stub posted to #incidents with the timeline pre-filled from logs, and Dana was asked to review. I\'ll nudge her again in 4 hours if it\'s still untouched.' }
      ],
      'a-beacon': [
        { id: 'm6', author: 'you', ts: 'Yesterday 4:20 PM', text: 'Brief me on embedded vector DBs for under 50 GB with strong metadata filtering' },
        { id: 'm7', author: 'agent', ts: 'Yesterday 4:23 PM',
          tools: [
            { name: 'web.search', args: 'q: filtered ANN benchmark 2026', ms: 1240 },
            { name: 'web.search', args: 'q: pgvector vs qdrant metadata filters', ms: 980 }
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
          tools: [{ name: 'github.commits', args: 'repo: acme/platform since: 7d', ms: 640 }],
          text: 'v2.14 notes drafted — 41 commits: 3 features, 9 fixes, 2 breaking (both flagged at the top). Posted to #release and tagged for review.' }
      ],
      'a-ledger': [],
      'a-scout': [
        { id: 'm11', author: 'agent', ts: '9:26 AM', cron: 'inbox-triage', text: 'Triage pass: 31 new tickets — 18 routed to Billing (auto-replied), 9 to Bugs, 4 escalated to a human. Three replies are drafted and waiting in the approval queue.' }
      ],
      'c-ops': [
        { id: 'm12', author: 'agent', agentId: 'a-atlas', ts: '7:00 AM', text: 'Morning digest pinned. Highlight: deploys 3 / rollback 1 — timeline in #incidents.' },
        { id: 'm13', author: 'you', ts: '9:10 AM', text: '@Warden can you take the 5xx spike? Atlas is already on the deploy correlation' },
        { id: 'm14', author: 'agent', agentId: 'a-warden', ts: '9:11 AM',
          tools: [{ name: 'grafana.query', args: 'alert: rate_5xx window 3h', ms: 640 }],
          text: 'On it — pulling the 5xx timeline now. @Atlas flag me if the rollback window shifts and I\'ll re-check the burn rate.' }
      ],
      'c-general': [],
      'c-incidents': [],
      'c-engineering': [],
      'c-release': [],
      'p-dana': [
        { id: 'm15', author: 'other', name: 'Dana Kwan', ts: '9:15 AM', text: 'Saw the postmortem stub — nice. I\'ll review after standup.' }
      ],
      'p-milo': [],
      'p-sasha': []
    },
    cron: [
      { id: 'morning-digest', name: 'Morning ops digest', agentId: 'a-atlas', expr: '0 7 * * 1-5', human: 'Weekdays · 7:00 AM', next: 'Tue 7:00 AM', enabled: true, last: { status: 'success', when: '2h ago', dur: '42s' } },
      { id: 'inbox-triage', name: 'Inbox triage', agentId: 'a-scout', expr: '*/30 * * * *', human: 'Every 30 minutes', next: '9:30 AM', enabled: true, last: { status: 'success', when: '12m ago', dur: '8s' } },
      { id: 'spend-report', name: 'Weekly spend report', agentId: 'a-ledger', expr: '0 8 * * 1', human: 'Mondays · 8:00 AM', next: 'Mon 8:00 AM', enabled: true, last: { status: 'success', when: '6d ago', dur: '51s' } },
      { id: 'postmortem-reminder', name: 'Postmortem follow-ups', agentId: 'a-warden', expr: '0 9 * * 1-5', human: 'Weekdays · 9:00 AM', next: 'Tue 9:00 AM', enabled: false, last: { status: 'skipped', when: '3d ago', dur: '—' } },
      { id: 'changelog-sweep', name: 'Changelog sweep', agentId: 'a-quill', expr: '30 17 * * 5', human: 'Fridays · 5:30 PM', next: 'Fri 5:30 PM', enabled: true, last: { status: 'success', when: '4d ago', dur: '2m 10s' } }
    ],
    runs: [
      { id: 'run_9f27', agentId: 'a-atlas', trigger: 'cron', when: '7:00 AM', dur: '42s', tokens: '18.2k', status: 'success' },
      { id: 'run_9f26', agentId: 'a-scout', trigger: 'chat', when: '9:12 AM', dur: '8s', tokens: '2.1k', status: 'success' },
      { id: 'run_9f25', agentId: 'a-beacon', trigger: 'chat', when: 'Yesterday', dur: '3m 12s', tokens: '96.4k', status: 'success' },
      { id: 'run_9f24', agentId: 'a-warden', trigger: 'api', when: 'Yesterday', dur: '21s', tokens: '9.4k', status: 'failed' },
      { id: 'run_9f23', agentId: 'a-atlas', trigger: 'cron', when: 'Yesterday', dur: '39s', tokens: '17.8k', status: 'success' },
      { id: 'run_9f22', agentId: 'a-quill', trigger: 'cron', when: 'Friday', dur: '2m 10s', tokens: '44.0k', status: 'success' }
    ],
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
    mcpServers: [
      { id: 'github', name: 'GitHub', transport: 'stdio · gh-mcp serve --read-only', auth: 'OAuth · acme/platform', tools: 24, status: 'connected',
        sample: ['repo.list', 'repo.read_file', 'issues.search', 'issues.create', 'pulls.list', 'pulls.review', 'commits.range', 'actions.runs'] },
      { id: 'postgres', name: 'Postgres', transport: 'stdio · postgres-mcp --replica prod-ro', auth: 'Service account · read-only', tools: 6, status: 'connected',
        sample: ['db.schema', 'db.tables', 'db.query_ro', 'db.explain'] },
      { id: 'slack', name: 'Slack', transport: 'http · slack-mcp.internal:8787/sse', auth: 'Bot token · 4 channels', tools: 12, status: 'connected',
        sample: ['channels.list', 'history.read', 'message.post', 'users.lookup'] },
      { id: 'browser', name: 'Browser', transport: 'stdio · browser-mcp --headless', auth: 'No auth · sandboxed profile', tools: 8, status: 'error',
        error: 'Handshake failed — headless Chromium missing on runner-2', sample: ['page.open', 'page.click', 'page.extract', 'screenshot.capture'] },
      { id: 'filesystem', name: 'Filesystem', transport: 'stdio · fs-mcp --root /docs', auth: 'Workspace scope · /docs', tools: 5, status: 'disabled',
        sample: ['fs.list', 'fs.read', 'fs.write', 'fs.glob'] },
      { id: 'memory', name: 'Memory', transport: 'stdio · memory-mcp', auth: 'Workspace scope · shared', tools: 4, status: 'disabled',
        sample: ['memory.save', 'memory.recall', 'memory.forget'] }
    ],
    skillLib: [
      { id: 'research', name: 'Deep research', version: '2.4.1', uses: 312, enabled: true, source: 'registry',
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'code', name: 'Code execution', version: '1.9.0', uses: 87, enabled: true, source: 'registry',
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'Data analysis', version: '2.1.3', uses: 214, enabled: true, source: 'registry',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.' },
      { id: 'writing', name: 'Writing & editing', version: '3.0.0', uses: 458, enabled: true, source: 'registry',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'summarize', name: 'Summarization', version: '1.4.2', uses: 1204, enabled: true, source: 'registry',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'vision', name: 'Vision', version: '1.2.0', uses: 0, enabled: false, source: 'registry',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.' },
      { id: 'postmortem', name: 'Postmortem writer', version: '0.3.1', uses: 23, enabled: true, source: 'workspace',
        desc: 'Turns an incident timeline into a review-ready postmortem with contributing factors.' }
    ],
    keys: [
      { id: 'k1', name: 'production-gateway', masked: 'oc_live_••••••••7f3a', full: 'oc_live_9t2mKc7QwZr4LpHx7f3a', created: 'Mar 2026' },
      { id: 'k2', name: 'ci-deploy', masked: 'oc_live_••••••••a21b', full: 'oc_live_3bVn8sYqTfE2mJdRa21b', created: 'Jun 2026' }
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
        skills: ['writing', 'summarize'], mcp: ['slack'],
        prompt: 'You are Herald. Turn ticket activity into crisp standup notes. Never invent status.' },
      { id: 'a-tally', name: 'Tally', model: 'llama-4-scout', temp: 0.1, autonomy: 'full', channelPost: true,
        role: 'Data analyst — ETL checks and weekly metrics',
        status: 'running', tools: ['db', 'api'], lastActive: 'just now',
        skills: ['data', 'summarize'], mcp: ['postgres'],
        prompt: 'You are Tally. Verify pipeline freshness and row counts against a 7-day baseline. Alert on drift over 5%.' },
      { id: 'a-forge', name: 'Forge', model: 'claude-sonnet-5', temp: 0.3, autonomy: 'approval', channelPost: false,
        role: 'Dev agent — PR review and test triage',
        status: 'idle', tools: ['shell', 'api'], lastActive: '6h ago',
        skills: ['code', 'summarize'], mcp: ['github'],
        prompt: 'You are Forge. Review PRs for correctness first, style second. Run the test suite before commenting.' }
    ],
    channels: [
      { id: 'g-general', name: 'general', purpose: 'Company-wide', agentId: 'a-herald', unread: 4, members: ['a-herald', 'a-tally', 'p-ravi'] },
      { id: 'g-data', name: 'data', purpose: 'Pipelines & metrics', agentId: 'a-tally', unread: 0, members: ['a-tally', 'a-forge', 'p-ravi'] }
    ],
    people: [{ id: 'p-ravi', name: 'Ravi Shah', presence: 'online' }],
    threads: {
      'a-herald': [],
      'a-tally': [
        { id: 'g1', author: 'agent', ts: '2:00 AM', cron: 'nightly-etl', text: 'ETL check: 12/12 pipelines green. Row counts within 2% of the 7-day average. No action needed.' }
      ],
      'a-forge': [
        { id: 'g2', author: 'you', ts: 'Yesterday', text: 'Review the auth refactor PR when tests go green' },
        { id: 'g3', author: 'agent', ts: 'Yesterday', tools: [{ name: 'shell.run', args: 'pnpm test auth/', ms: 15800 }], text: 'Reviewed. Two real findings: a missing token-expiry test and a race in refresh. Left comments on the diff — rest looks clean.' }
      ],
      'g-general': [],
      'g-data': [],
      'p-ravi': []
    },
    cron: [
      { id: 'nightly-etl', name: 'Nightly ETL check', agentId: 'a-tally', expr: '0 2 * * *', human: 'Daily · 2:00 AM', next: '2:00 AM', enabled: true, last: { status: 'success', when: '7h ago', dur: '26s' } },
      { id: 'standup-notes', name: 'Standup notes', agentId: 'a-herald', expr: '0 9 * * 1-5', human: 'Weekdays · 9:00 AM', next: 'Tue 9:00 AM', enabled: false, last: { status: 'skipped', when: '5d ago', dur: '—' } }
    ],
    runs: [
      { id: 'run_g07', agentId: 'a-tally', trigger: 'cron', when: '2:00 AM', dur: '26s', tokens: '6.8k', status: 'success' },
      { id: 'run_g06', agentId: 'a-forge', trigger: 'chat', when: 'Yesterday', dur: '19m 04s', tokens: '210k', status: 'success' },
      { id: 'run_g05', agentId: 'a-herald', trigger: 'cron', when: 'Yesterday', dur: '12s', tokens: '3.2k', status: 'success' }
    ],
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
    mcpServers: [
      { id: 'github', name: 'GitHub', transport: 'stdio · gh-mcp serve --read-only', auth: 'OAuth · globex/etl', tools: 24, status: 'connected',
        sample: ['repo.list', 'issues.search', 'pulls.list', 'pulls.review', 'commits.range'] },
      { id: 'slack', name: 'Slack', transport: 'http · slack-mcp.internal:8787/sse', auth: 'Bot token · 2 channels', tools: 12, status: 'connected',
        sample: ['channels.list', 'history.read', 'message.post'] },
      { id: 'postgres', name: 'Postgres', transport: 'stdio · postgres-mcp --replica warehouse-ro', auth: 'Service account · read-only', tools: 6, status: 'error',
        error: 'Connect failed — warehouse replica unreachable', sample: ['db.schema', 'db.query_ro'] },
      { id: 'filesystem', name: 'Filesystem', transport: 'stdio · fs-mcp --root /docs', auth: 'Workspace scope · /docs', tools: 5, status: 'disabled',
        sample: ['fs.list', 'fs.read'] },
      { id: 'browser', name: 'Browser', transport: 'stdio · browser-mcp --headless', auth: 'No auth · sandboxed profile', tools: 8, status: 'disabled',
        sample: ['page.open', 'page.extract'] },
      { id: 'memory', name: 'Memory', transport: 'stdio · memory-mcp', auth: 'Workspace scope · shared', tools: 4, status: 'disabled',
        sample: ['memory.save', 'memory.recall'] }
    ],
    skillLib: [
      { id: 'research', name: 'Deep research', version: '2.4.1', uses: 12, enabled: true, source: 'registry',
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'code', name: 'Code execution', version: '1.9.0', uses: 141, enabled: true, source: 'registry',
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'Data analysis', version: '2.1.3', uses: 96, enabled: true, source: 'registry',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.' },
      { id: 'writing', name: 'Writing & editing', version: '3.0.0', uses: 64, enabled: true, source: 'registry',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'summarize', name: 'Summarization', version: '1.4.2', uses: 388, enabled: true, source: 'registry',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'vision', name: 'Vision', version: '1.2.0', uses: 0, enabled: false, source: 'registry',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.' }
    ],
    keys: [
      { id: 'gk1', name: 'default', masked: 'oc_live_••••••••e5c9', full: 'oc_live_7dXk2pQmZn8vLtEe5c9', created: 'Jul 2026' }
    ]
  };
}

/* ---- session model: threads[cid] = { active, list: [{ id, title, updated, messages }] } ---- */
export const EXTRA_SESSIONS = {
  'a-atlas': [
    { title: 'Deploy correlation — 5xx spike', updated: 'Yesterday', messages: [
      { id: 'x1', author: 'you', ts: 'Yesterday 11:20 AM', text: 'Correlate this week\'s 5xx spikes with deploys' },
      { id: 'x2', author: 'agent', ts: 'Yesterday 11:22 AM',
        tools: [{ name: 'grafana.query', args: 'alert: rate_5xx window 7d', ms: 760 }],
        text: 'Two spikes this week. Tuesday\'s lines up with the v2.12 rollout (reverted 20:41); today\'s with the v2.14 rollback. Both resolved within 40 minutes of the revert.' }
    ] }
  ],
  'a-beacon': [
    { title: 'Filtered-ANN benchmark sources', updated: 'Monday', messages: [
      { id: 'x3', author: 'you', ts: 'Monday 10:05 AM', text: 'Pull the benchmark papers cited in your vector DB brief' },
      { id: 'x4', author: 'agent', ts: 'Monday 10:09 AM',
        tools: [{ name: 'web.search', args: 'q: filtered ANN benchmark 2026', ms: 1100 }],
        text: 'Three primary sources: the Qdrant filtered-search benchmark (Feb 2026), pgvector HNSW filter tests, and the LanceDB TPC-H annex — linked in the brief appendix with repro notes.' }
    ] }
  ],
  'a-ledger': Array.from({ length: 100 }, (_, i) => ({
    title: 'Reconciliation run #' + String(100 - i).padStart(3, '0'),
    updated: i === 0 ? 'Today' : (i === 1 ? 'Yesterday' : i + 'd ago'),
    messages: [
      { id: 'lgu' + i, author: 'you', ts: i === 0 ? 'Today 6:00 AM' : (i === 1 ? 'Yesterday 6:00 AM' : i + 'd ago'), text: 'Run the daily reconciliation sweep' },
      { id: 'lga' + i, author: 'agent', ts: i === 0 ? 'Today 6:00 AM' : (i === 1 ? 'Yesterday 6:00 AM' : i + 'd ago'),
        tools: [{ name: 'db.query', args: 'ledger unreconciled > $50', ms: 380 + (i % 7) * 25 }],
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
      : ('Scheduled · ' + (msgs[0].cron || 'digest')).slice(0, 48);
    const main = { id: uid('s'), title, updated: (msgs[msgs.length - 1] || {}).ts || '', messages: msgs };
    const extras = (EXTRA_SESSIONS[cid] || []).map((s: any) => ({ ...s, id: uid('s'), messages: s.messages.slice() }));
    threads[cid] = { active: main.id, list: [main].concat(extras) };
  });
  return {
    ...t,
    agents: (t.agents || []).map((a: any) => ({
      ...a,
      provider: a.provider || providerOf(a.model),
      skills: a.skills || ['research', 'summarize'],
      mcp: a.mcp || []
    })),
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
      status: 'idle', tools: ['web'], lastActive: 'just now', skills: [], mcp: [],
      prompt: 'You are Guide, the starter agent for a brand-new OnClaw workspace. Answer questions about the workspace, demonstrate tool use, and suggest what to deploy next.'
    }] : [],
    channels: starter ? [{ id: uid('c'), name: 'general', purpose: 'Company-wide', agentId: aid, unread: 0, members: [aid] }] : [],
    people: [],
    threads: starter ? {
      [aid]: { active: 's0', list: [{ id: 's0', title: 'Chat', updated: 'just now', messages: [
        { id: uid('m'), author: 'agent', ts: 'just now', text: 'Welcome to ' + name + '. I\'m Guide, your starter agent — ask me anything or put me to work with a web search. When you\'re ready, deploy specialists from the Agents view; everything about me lives in Settings → Agents.' }
      ] }] }
    } : {},
    cron: [], runs: [],
    members: [{ id: 'me', name: 'You', email: 'you@' + sub + '.dev', role: 'Owner' }],
    integrations: [
      { id: 'slack', name: 'Slack', detail: 'Route digests to a channel', connected: false },
      { id: 'github', name: 'GitHub', detail: 'Watch repos and review PRs', connected: false },
      { id: 'pagerduty', name: 'PagerDuty', detail: 'Page on SLO burn', connected: false },
      { id: 'linear', name: 'Linear', detail: 'Sync issues to agent tasks', connected: false },
      { id: 'notion', name: 'Notion', detail: 'Publish briefs to docs', connected: false },
      { id: 'postgres', name: 'Postgres', detail: 'Query a read replica', connected: false }
    ],
    mcpServers: [
      { id: 'github', name: 'GitHub', transport: 'stdio · gh-mcp serve --read-only', auth: 'Not configured', tools: 24, status: 'disabled', sample: ['repo.list', 'issues.search', 'pulls.list', 'commits.range'] },
      { id: 'postgres', name: 'Postgres', transport: 'stdio · postgres-mcp --replica', auth: 'Not configured', tools: 6, status: 'disabled', sample: ['db.schema', 'db.query_ro'] },
      { id: 'slack', name: 'Slack', transport: 'http · slack-mcp.internal:8787/sse', auth: 'Not configured', tools: 12, status: 'disabled', sample: ['channels.list', 'message.post'] },
      { id: 'filesystem', name: 'Filesystem', transport: 'stdio · fs-mcp --root /docs', auth: 'Not configured', tools: 5, status: 'disabled', sample: ['fs.list', 'fs.read'] },
      { id: 'memory', name: 'Memory', transport: 'stdio · memory-mcp', auth: 'Not configured', tools: 4, status: 'disabled', sample: ['memory.save', 'memory.recall'] },
      { id: 'browser', name: 'Browser', transport: 'stdio · browser-mcp --headless', auth: 'Not configured', tools: 8, status: 'disabled', sample: ['page.open', 'page.extract'] }
    ],
    skillLib: [
      { id: 'research', name: 'Deep research', version: '2.4.1', uses: 0, enabled: true, source: 'registry',
        desc: 'Multi-source research briefs with citation tracking and a structured summary template.' },
      { id: 'summarize', name: 'Summarization', version: '1.4.2', uses: 0, enabled: true, source: 'registry',
        desc: 'Thread and document summaries with action-item extraction.' },
      { id: 'code', name: 'Code execution', version: '1.9.0', uses: 0, enabled: false, source: 'registry',
        desc: 'Sandboxed Python for transforms, one-off scripts and quick calculations.' },
      { id: 'data', name: 'Data analysis', version: '2.1.3', uses: 0, enabled: false, source: 'registry',
        desc: 'DataFrame workflows over CSV and query results — joins, rollups, drift checks.' },
      { id: 'writing', name: 'Writing & editing', version: '3.0.0', uses: 0, enabled: false, source: 'registry',
        desc: 'Drafting and line-editing with house style rules applied on top of the base model.' },
      { id: 'vision', name: 'Vision', version: '1.2.0', uses: 0, enabled: false, source: 'registry',
        desc: 'Chart, screenshot and diagram reading for agents that handle images.' }
    ],
    keys: []
  };
}

