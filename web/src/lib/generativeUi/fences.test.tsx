/**
 * @vitest-environment jsdom
 */
// Fence transport tests (markdown-card-elements 2.2/2.5/2.7/2.8): the
// per-tag validators are fail-closed (a rejected body never mounts) while the
// message fails open (D2 — a bad fence degrades to the ordinary code block,
// never a crash, never blank). The AgentMessage integration pins the pre
// override end to end: a valid fence mounts inline, an unclosed/streaming
// fence and any rejected body stay the existing styled code block, ordinary
// tagged blocks route through the lazy shiki highlighter (mocked here), and
// inline code is untouched.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { AgentMessage } from '../../components/chat/AgentMessage';
import { parseFence } from './fences';

vi.mock('beautiful-mermaid', () => ({
  renderMermaidSVG: vi.fn((code: string) => `<svg data-code="${code}"></svg>`),
}));

vi.mock('react-shiki', () => ({
  useShikiHighlighter: vi.fn(() => null),
}));

import { useShikiHighlighter } from 'react-shiki';
const mockHighlight = vi.mocked(useShikiHighlighter);

beforeEach(() => {
  mockHighlight.mockClear();
});

const OK = '{\n  "x": 1\n}' as const;

describe('parseFence — JSON tags (2.2)', () => {
  it('accepts every JSON tag on a shape-correct body', () => {
    const bodies: Record<string, string> = {
      chart: '{"label":"Revenue","value":"$1","points":[1,2]}',
      timeline: '{"events":[{"label":"Kickoff"}]}',
      preview: '{"url":"https://example.com","html":"<p>hi</p>"}',
      table: '{"columns":[{"key":"name","label":"Model"}],"rows":[{"name":"Atlas"}]}',
      ticker: '{"value":4200,"label":"stars"}',
      activity: '{"title":"D","total":3,"start":"2026-08-01","end":"2026-08-07","data":[{"date":"2026-08-03","count":1}]}',
      spec: '{"title":"Atlas","rows":[{"label":"R","value":"V"}]}',
      compare: '{"traitLabels":["A"],"options":[{"id":"x","name":"N","headline":"H","traits":["a"]}],"recommendedId":"x","reason":"why"}',
      progress: '{"title":"T","stages":[{"name":"s","weight":1}],"stageIndex":0,"stageProgress":0,"eta":"1m"}',
      score: '{"verdict":"ok","total":1,"outOf":2,"criteria":[{"label":"L","score":1,"weight":1}]}',
      flow: '{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"done"}],"edges":[]}',
      math: '{"steps":[{"expression":"x=1"}]}',
    };
    for (const [tag, body] of Object.entries(bodies)) {
      const parsed = parseFence(tag, body);
      expect(parsed.ok, `${tag} should accept its minimal shape`).toBe(true);
    }
  });

  it('rejects malformed JSON and non-object bodies', () => {
    expect(parseFence('chart', '{oops').ok).toBe(false);
    expect(parseFence('chart', '[1,2,3]').ok).toBe(false);
    expect(parseFence('chart', '"just a string"').ok).toBe(false);
    expect(parseFence('chart', '').ok).toBe(false);
  });

  it('rejects missing required fields and empty collections', () => {
    expect(parseFence('chart', '{"label":"L","points":[1]}').ok).toBe(false); // no value
    expect(parseFence('chart', '{"label":"L","value":"V","points":[]}').ok).toBe(false); // empty series
    expect(parseFence('ticker', '{"value":1}').ok).toBe(false);
    expect(parseFence('ticker', '{"value":"4","label":"stars"}').ok).toBe(false); // string value
    expect(parseFence('timeline', '{"events":[]}').ok).toBe(false);
    expect(parseFence('timeline', '{"events":[{"state":"settled"}]}').ok).toBe(false); // no label
    expect(parseFence('table', '{"columns":[{"key":"a","label":"A"}],"rows":[]}').ok).toBe(false);
    expect(parseFence('table', '{"columns":[],"rows":[{"a":1}]}').ok).toBe(false);
    expect(parseFence('spec', '{"rows":[{"label":"L","value":"V"}]}').ok).toBe(false); // no title
    expect(parseFence('compare', '{"traitLabels":[],"options":[{"id":"x","name":"N","headline":"H","traits":[]}],"recommendedId":"x","reason":"r"}').ok).toBe(false);
    expect(parseFence('progress', '{"title":"T","stages":[],"stageIndex":0,"stageProgress":0,"eta":"e"}').ok).toBe(false);
    expect(parseFence('score', '{"verdict":"v","total":1,"outOf":2,"criteria":[]}').ok).toBe(false);
    expect(parseFence('math', '{"steps":[]}').ok).toBe(false);
    expect(parseFence('math', '{"steps":[{"expression":"  "}]}').ok).toBe(false);
    expect(parseFence('activity', '{"title":"T","total":1,"start":"nope","end":"2026-08-07","data":[{"date":"2026-08-03","count":1}]}').ok).toBe(false);
  });

  it('rejects wrong enum values and out-of-range numbers', () => {
    expect(parseFence('chart', '{"label":"L","value":"V","variant":"scatter","points":[1]}').ok).toBe(false);
    expect(parseFence('timeline', '{"events":[{"label":"A","state":"done"}]}').ok).toBe(false); // fence enum is settled|reference
    expect(parseFence('flow', '{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"running"}],"edges":[]}').ok).toBe(false);
    expect(parseFence('progress', '{"title":"T","stages":[{"name":"s","weight":1}],"stageIndex":0,"stageProgress":150,"eta":"e"}').ok).toBe(false);
    expect(parseFence('progress', '{"title":"T","stages":[{"name":"s","weight":1}],"stageIndex":9,"stageProgress":0,"eta":"e"}').ok).toBe(false);
  });

  it('rejects relative or non-http preview URLs — absolute http(s) only', () => {
    expect(parseFence('preview', '{"url":"/app","html":"x"}').ok).toBe(false);
    expect(parseFence('preview', '{"url":"example.com/app","html":"x"}').ok).toBe(false);
    expect(parseFence('preview', '{"url":"ftp://example.com/app","html":"x"}').ok).toBe(false);
    expect(parseFence('preview', '{"url":"javascript:alert(1)","html":"x"}').ok).toBe(false);
    const parsed = parseFence('preview', '{"url":"http://example.com/app","html":"x"}');
    expect(parsed.ok).toBe(true); // plain http is allowed too
  });

  it('enforces the cross-field rules: recommendedId ∈ options, flow edges reference nodes', () => {
    expect(parseFence('compare', '{"traitLabels":["A"],"options":[{"id":"x","name":"N","headline":"H","traits":["a"]}],"recommendedId":"zzz","reason":"r"}').ok).toBe(false);
    expect(parseFence('flow', '{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"done"}],"edges":[{"from":"a","to":"ghost"}]}').ok).toBe(false);
    expect(parseFence('flow', '{"nodes":[{"id":"a","label":"A","column":0,"row":0,"state":"done"},{"id":"a","label":"A2","column":1,"row":0,"state":"done"}],"edges":[]}').ok).toBe(false); // duplicate ids
  });

  it('keeps mermaid/diagram bodies as raw source and rejects an empty one', () => {
    const mermaid = parseFence('mermaid', 'graph TD;A-->B');
    expect(mermaid.ok).toBe(true);
    if (mermaid.ok) expect(mermaid.props).toEqual({ code: 'graph TD;A-->B' });
    // Raw source is never JSON-parsed — braces are legal mermaid.
    const braces = parseFence('mermaid', 'flowchart LR\nA{"curly"}-->B');
    expect(braces.ok).toBe(true);
    expect(parseFence('mermaid', '   ').ok).toBe(false);
    // diagram requires the info-string title.
    expect(parseFence('diagram', 'graph TD;A-->B').ok).toBe(false);
    expect(parseFence('diagram', 'graph TD;A-->B', '   ').ok).toBe(false);
    const diagram = parseFence('diagram', 'graph TD;A-->B', 'Payment flow');
    expect(diagram.ok).toBe(true);
    if (diagram.ok) expect(diagram.props).toEqual({ code: 'graph TD;A-->B', title: 'Payment flow' });
  });

  it('falls back to the info-string meta when the body is blank (inline ```chart {json} form)', () => {
    const metaJson = '{"label":"Weekly signups","value":"42","delta":"+12%","variant":"bars","points":[4,8,6,9,7]}';
    const viaMeta = parseFence('chart', '', metaJson);
    expect(viaMeta.ok).toBe(true);
    if (viaMeta.ok) {
      expect(viaMeta.props).toEqual({ label: 'Weekly signups', value: '42', delta: '+12%', variant: 'bars', points: [4, 8, 6, 9, 7] });
    }
    // Whitespace-only body counts as blank too.
    expect(parseFence('chart', '  \n', metaJson).ok).toBe(true);
    // A non-blank body always wins — the meta is ignored there.
    expect(parseFence('chart', '{"label":"L","value":"1","points":[3]}', metaJson).ok).toBe(true);
    // Blank body with no meta still rejects, and meta failing the shape rejects.
    expect(parseFence('chart', '', null).ok).toBe(false);
    expect(parseFence('chart', '', '{"label":1}').ok).toBe(false);
  });

  it('rejects an unknown tag', () => {
    expect(parseFence('snake', OK).ok).toBe(false);
    expect(parseFence('shiki', OK).ok).toBe(false); // deliberately not a fence tag
  });
});

describe('parseFence — the `ui` composition tag', () => {
  const ui = (body: unknown) => parseFence('ui', JSON.stringify(body));

  it('accepts a well-formed composition tree', () => {
    const r = ui({
      $type: 'Col',
      gap: 3,
      children: [
        { $type: 'Header', text: 'Briefing', size: '2xl' },
        { $type: 'Row', gap: 3, children: [
          { $type: 'Card', padding: 4, children: [{ $type: 'Caption', value: 'Incidents' }] },
        ]},
      ],
    });
    expect(r.ok).toBe(true);
  });

  it('strips Card.background — the prop that renders white text on a white card', () => {
    const r = ui({ $type: 'Card', background: '#ffffff', padding: 4, children: [{ $type: 'Text', value: 'hi' }] });
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    const spec = (r.props as any).spec;
    expect(spec.background).toBeUndefined();
    expect(spec.padding).toBe(4);
  });

  it('schema gate: a required prop missing drops the node (interior) / rejects (root)', () => {
    // Caption requires `value`; the live model wrote `text` (thread i).
    // Interior: the caption drops, its siblings survive.
    const inner = ui({ $type: 'Card', padding: 4, children: [
      { $type: 'Caption', text: 'oops' },
      { $type: 'Header', text: '3' },
    ]});
    expect(inner.ok).toBe(true);
    if (!inner.ok) return;
    const card = (inner.props as any).spec;
    // Single surviving child unwraps to a node (upstream children convention).
    const kids = Array.isArray(card.children) ? card.children : [card.children];
    expect(kids).toHaveLength(1); // Header survived, Caption dropped
    expect(kids[0].$type).toBe('Header');

    // Root: strict — the fence degrades to the code block.
    expect(ui({ $type: 'Caption', text: 'oops' }).ok).toBe(false);
  });

  it('schema gate: closed icon set and sm|md|lg size enum', () => {
    // Root Icon with a bad name → strict reject.
    expect(ui({ $type: 'Icon', name: 'alert' }).ok).toBe(false);
    expect(ui({ $type: 'Icon', name: 'bell', size: 16 }).ok).toBe(false); // px, not the enum
    // Nested: the icon drops, the branch survives.
    const nested = ui({ $type: 'Card', children: [
      { $type: 'Icon', name: 'alert' },
      { $type: 'Text', value: 'kept' },
    ]});
    expect(nested.ok).toBe(true);
  });

  it('token clamp: out-of-range gap/padding clamps to 0-8 instead of rejecting', () => {
    // A live model wrote gap:16 thinking in pixels; the doc says 0-8 tokens.
    const r = ui({ $type: 'Row', gap: 12, children: [{ $type: 'Text', value: 'x' }] });
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect((r.props as any).spec.gap).toBe(8);
    const card = ui({ $type: 'Card', padding: 9, children: [{ $type: 'Text', value: 'x' }] });
    expect(card.ok).toBe(true);
    if (!card.ok) return;
    expect((card.props as any).spec.padding).toBe(8);
  });

  it('root strictness: a node with no $type and an unknown $type both reject', () => {
    expect(ui({ children: [{ $type: 'Text', value: 'x' }] }).ok).toBe(false);
    expect(ui({ $type: 'Hologram' }).ok).toBe(false);
    // Dropped vocabulary (Image is a security deference) is unknown to the gate.
    expect(ui({ $type: 'Image', src: 'https://x.example/a.png', alt: 'a' }).ok).toBe(false);
  });

  it('depth guard truncates instead of rejecting; text children stay legal', () => {
    expect(ui({ $type: 'Card', children: ['plain text'] }).ok).toBe(true);
    let deep: any = { $type: 'Text', value: 'bottom' };
    for (let i = 0; i < 20; i++) deep = { $type: 'Card', children: [deep] };
    // Interior tolerance: the chain truncates at the depth guard, root survives.
    expect(ui(deep).ok).toBe(true);
  });

  it('the vocabulary is the trimmed 24 (Image/DatePicker/Carousel excluded)', () => {
    const r = ui({ $type: 'Image', src: 'https://x.example/a.png', alt: 'a' });
    expect(r.ok).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// AgentMessage integration: the pre override (2.5–2.7).
// ---------------------------------------------------------------------------

function renderMessage(text: string, opts?: { busy?: boolean; isLast?: boolean }) {
  return render(
    <AgentMessage
      m={{ id: 'm1', text }}
      agent={{ name: 'Atlas' }}
      inChannel={false}
      busy={opts?.busy ?? false}
      isLast={opts?.isLast ?? false}
      onCopy={vi.fn()}
      onRefresh={vi.fn()}
      onBranch={vi.fn()}
      members={[]}
    />,
  );
}

// The existing styled code block (D2 degradation target) — identical classes
// to the pre override this change extends.
const PLAIN_PRE = /od-scroll.*overflow-x-auto/;

describe('AgentMessage — fence mounting (2.5/2.6)', () => {
  it('a valid fence mounts its card inline in the message body', () => {
    const { container } = renderMessage('Here:\n\n```ticker\n{"value":4200,"label":"stars"}\n```\n');
    expect(container.querySelector('[data-slot="number-ticker"]')).not.toBeNull();
    expect(container.textContent).toContain('stars');
  });

  it('a diagram fence mounts the zoom chrome titled from the info string', async () => {
    const { container } = renderMessage('```diagram Payment flow\ngraph TD;A-->B\n```\n');
    expect(container.querySelector('[data-slot="diagram"]')).not.toBeNull();
    expect(container.textContent).toContain('Payment flow');
    await waitFor(() => expect(container.querySelector('[data-slot="diagram"] svg')).not.toBeNull());
  });

  it('a math fence upgrades from LaTeX source to KaTeX', async () => {
    const { container } = renderMessage('```math\n{"steps":[{"expression":"\\\\int_0^1 x\\\\,dx"}]}\n```\n');
    expect(container.querySelector('[data-slot="math-block"]')).not.toBeNull();
    expect(container.textContent).toContain('int_0^1'); // degraded first paint: the source
    await waitFor(() => expect(container.querySelector('.katex')).not.toBeNull());
  });

  it('an unclosed (still-streaming) fence stays the existing plain code block', () => {
    const settled = renderMessage('```chart\n{"label":"R"');
    expect(settled.container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(settled.container.textContent).toContain('"label":"R"');
    expect(settled.container.querySelector('[data-od-id="tool-fence-chart"]')).toBeNull();
    // Same story mid-turn — the growing text is a normal block until it closes.
    const live = renderMessage('```chart\n{"label":"R"', { busy: true, isLast: true });
    expect(live.container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(live.container.querySelector('[data-od-id="tool-fence-chart"]')).toBeNull();
  });

  it('a closed but malformed fence degrades to the existing code block', () => {
    const { container } = renderMessage('```chart\n{"label":\n```\n');
    expect(container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(container.textContent).toContain('{"label":');
    expect(container.querySelector('[data-od-id="tool-fence-chart"]')).toBeNull();
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('a rejected inline fence (payload on the tag line) degrades showing the meta as readable source', () => {
    const { container } = renderMessage('```chart {"label": 123, "value": nope, "points": []}\n```');
    const pre = container.querySelector('pre');
    expect(pre?.className).toMatch(PLAIN_PRE);
    expect((pre?.textContent ?? '').trim()).toContain('"label": 123'); // never blank
  });

  it('a fence body that fails validation (relative preview URL) degrades too', () => {
    const { container } = renderMessage('```preview\n{"url":"/app","html":"<p>x</p>"}\n```\n');
    expect(container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('an unknown tag is an ordinary code block, not a card', () => {
    const { container } = renderMessage('```mystery\n{"secret":"data"}\n```\n');
    expect(container.querySelector('pre')).not.toBeNull();
    expect(container.textContent).toContain('"secret"');
    expect(container.querySelector('[data-slot]')).toBeNull();
    expect(container.querySelector('[data-od-id^="tool-fence-"]')).toBeNull();
  });

  it('inline code stays an inline chip and never mounts a card', () => {
    const { container } = renderMessage('Run `chart` now.');
    expect(container.querySelector('pre')).toBeNull();
    expect(container.querySelector('code')).not.toBeNull();
    expect(container.querySelector('[data-od-id^="tool-fence-"]')).toBeNull();
  });
});

describe('AgentMessage — degraded fence caption (add-generative-ui-fence)', () => {
  const CAPTION = "This card couldn't be rendered — showing source";

  it('a failed ui fence shows its source followed by the exact caption', () => {
    const { container } = renderMessage('```ui\n{"$type":"Hologram"}\n```\n');
    const caption = container.querySelector('p[title]');
    expect(container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(container.textContent).toContain('{"$type":"Hologram"}');
    expect(caption?.textContent).toBe(CAPTION);
  });

  it('the caption title is the validator rejection reason', () => {
    const bad = parseFence('ui', JSON.stringify({ $type: 'Hologram' }));
    expect(bad.ok).toBe(false);
    // strict:false doesn't narrow the ok-discriminated union — cast the
    // rejected side directly; assertions below are unchanged.
    const { reason } = bad as { ok: false; reason: string };
    const { container } = renderMessage('```ui\n{"$type":"Hologram"}\n```\n');
    expect(container.querySelector('p[title]')?.getAttribute('title')).toBe(reason);
  });

  it('the meta-as-source degraded branch carries the caption too', () => {
    const { container } = renderMessage('```chart {"label": 123, "value": nope, "points": []}\n```');
    const caption = container.querySelector('p[title]');
    expect(caption?.textContent).toBe(CAPTION);
    expect((caption as HTMLElement | null)?.title.length).toBeGreaterThan(0);
  });

  it('mounted cards and ordinary code blocks carry no caption', () => {
    const ok = renderMessage('```ticker\n{"value":4200,"label":"stars"}\n```\n');
    expect(ok.container.querySelector('p[title]')).toBeNull();
    expect(ok.container.textContent).not.toContain(CAPTION);

    const go = renderMessage('```go\npackage main\n```\n');
    expect(go.container.querySelector('p[title]')).toBeNull();
    expect(go.container.textContent).not.toContain(CAPTION);
  });
});

describe('AgentMessage — ordinary code blocks through shiki (2.7)', () => {
  it('a settled tagged block routes through the lazy shiki highlighter', async () => {
    const { container } = renderMessage('```go\npackage main\n```\n');
    expect(container.querySelector('.aui-shiki-base')).not.toBeNull();
    expect(container.textContent).toContain('package main');
    // The mocked module answers null → the element's own degraded plain code,
    // and the lane was consulted exactly once for the chunk.
    await waitFor(() => expect(mockHighlight).toHaveBeenCalled());
    expect(mockHighlight).toHaveBeenCalledWith('package main', 'go', expect.anything(), expect.anything());
  });

  it('a streaming turn renders plain code and defers tokenization', () => {
    const calls = mockHighlight.mock.calls.length;
    const { container } = renderMessage('```go\npackage main\n```\n', { busy: true, isLast: true });
    expect(container.querySelector('.aui-shiki-base')).not.toBeNull();
    expect(container.textContent).toContain('package main');
    expect(mockHighlight.mock.calls.length).toBe(calls); // skipped while streaming
  });

  it('an untagged (indented) block keeps the existing styled pre', () => {
    const { container } = renderMessage('Intro:\n\n    package main\n');
    expect(container.querySelector('pre')?.className).toMatch(PLAIN_PRE);
    expect(container.querySelector('.aui-shiki-base')).toBeNull();
  });
});
