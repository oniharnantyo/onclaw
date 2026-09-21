/**
 * @vitest-environment jsdom
 */
// Generative-UI registry tests (change adopt-assistant-ui-elements, tasks
// 1.1–1.2, 1.4, 7.5): the coverage guard pins the registry universes (the
// counterpart of toolDisplay's catalog guard), and the dispatch tests pin the
// spec semantics — recognized `$type` renders its element, unknown `$type` is
// silent, children nest, malformed envelopes degrade instead of crashing.
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/react';
import {
  GENERATIVE_UI_TOOLS,
  GENERATIVE_UI_TYPES,
  parseFence,
  renderGenerativeUi,
  renderSpecByType,
  registeredSpecTypes,
  registeredToolKeys,
  registerSpecRenderer,
} from './index';
import { revealDelay } from './reveal';
import type { GenerativeUiCtx } from './registry';

// The lazy mermaid engine (mermaid/diagram fences) is a mocked dynamic import
// — the mount tests never pay for the real chunk (mock idiom from
// mermaidEngine.test).
vi.mock('beautiful-mermaid', () => ({
  renderMermaidSVG: vi.fn((code: string) => `<svg data-code="${code}"></svg>`),
}));

// Snapshot taken at module import — before any test-only registration below.
const SNAPSHOT_TYPES = registeredSpecTypes();
const SNAPSHOT_TOOLS = registeredToolKeys();

const ctx = (over: Partial<GenerativeUiCtx> = {}): GenerativeUiCtx => ({
  live: false,
  running: false,
  error: false,
  tool: 'ui.chart',
  ...over,
});

const CHART_ENVELOPE = {
  $type: 'chart',
  label: 'Revenue',
  value: '$12.4k',
  delta: -3.2,
  points: [3, 1, 4, 1, 5],
};

describe('generative-UI registry coverage guard (1.4)', () => {
  it('pins the registry universes', () => {
    // markdown-card-elements 2.1: the universe IS the 14 fence tags now.
    expect(GENERATIVE_UI_TYPES).toHaveLength(14);
    expect(GENERATIVE_UI_TYPES).toEqual([
      'chart', 'timeline', 'preview', 'table', 'ticker', 'activity', 'spec',
      'compare', 'progress', 'score', 'flow', 'math', 'mermaid', 'diagram',
    ]);
    expect(GENERATIVE_UI_TOOLS).toHaveLength(2);
    expect(SNAPSHOT_TYPES).toEqual([...GENERATIVE_UI_TYPES].sort());
    expect(SNAPSHOT_TOOLS).toEqual([...GENERATIVE_UI_TOOLS].sort());
  });
});

describe('registry dispatch (D4)', () => {
  it('a recognized $type renders its registered element in app tokens', () => {
    const el = renderGenerativeUi({
      tool: 'ui.chart',
      rawRes: JSON.stringify(CHART_ENVELOPE),
      ctx: ctx(),
    });
    expect(el).not.toBeNull();
    const { container } = render(<>{el}</>);
    expect(container.querySelector('[data-od-id="tool-ui.chart"]')).not.toBeNull();
    expect(container.textContent).toContain('Revenue');
    expect(container.textContent).toContain('$12.4k');
  });

  it('the result envelope wins over the echoed arguments', () => {
    const el = renderGenerativeUi({
      tool: 'ui.chart',
      rawRes: JSON.stringify({ $type: 'chart', label: 'From result', value: '1' }),
      rawArgs: JSON.stringify({ $type: 'chart', label: 'From args', value: '2' }),
      ctx: ctx(),
    });
    const { container } = render(<>{el}</>);
    expect(container.textContent).toContain('From result');
    expect(container.textContent).not.toContain('From args');
  });

  it('an unrecognized $type is silent — null, generic card keeps over', () => {
    expect(
      renderGenerativeUi({
        tool: 'mcp__x__widget',
        rawRes: JSON.stringify({ $type: 'mystery', data: 'secret' }),
        ctx: ctx({ tool: 'mcp__x__widget' }),
      })
    ).toBeNull();
    expect(
      renderGenerativeUi({ tool: 'ls', rawRes: '{"no":"type"}', ctx: ctx({ tool: 'ls' }) })
    ).toBeNull();
  });

  it('nested specs render under children; scalars pass through', () => {
    registerSpecRenderer('nest.box', ({ props, children }) => (
      <div data-od-id="nest-box">
        <span>{String(props.title)}</span>
        {children}
      </div>
    ));
    const el = renderGenerativeUi({
      tool: 'nest.box',
      rawRes: JSON.stringify({
        $type: 'nest.box',
        title: 'outer',
        children: [
          { $type: 'chart', label: 'inner', value: '9' },
          'plain text',
        ],
      }),
      ctx: ctx({ tool: 'nest.box' }),
    });
    const { container } = render(<>{el}</>);
    expect(container.querySelector('[data-od-id="nest-box"]')).not.toBeNull();
    expect(container.textContent).toContain('outer');
    expect(container.textContent).toContain('inner');
    expect(container.textContent).toContain('plain text');
  });
});

describe('chart card (7.2/7.5)', () => {
  it('delta tints by sign: falling red, rising green', () => {
    const down = render(<>{renderGenerativeUi({ tool: 'ui.chart', rawRes: JSON.stringify({ $type: 'chart', label: 'L', value: '1', delta: -3.2, points: [1, 2] }), ctx: ctx() })}</>);
    expect(down.container.querySelector('.text-danger')).not.toBeNull();
    const up = render(<>{renderGenerativeUi({ tool: 'ui.chart', rawRes: JSON.stringify({ $type: 'chart', label: 'L', value: '1', delta: '+5', points: [1, 2] }), ctx: ctx() })}</>);
    expect(up.container.querySelector('.text-success')).not.toBeNull();
  });

  it('visible-count clamp draws a prefix of the series, never zero points', () => {
    const bars = (visible: unknown) =>
      JSON.stringify({ $type: 'chart', label: 'L', value: '1', variant: 'bars', points: [1, 2, 3, 4, 5], visible });
    const two = render(<>{renderGenerativeUi({ tool: 'ui.chart', rawRes: bars(2), ctx: ctx() })}</>);
    expect(two.container.querySelectorAll('rect')).toHaveLength(2);
    const junk = render(<>{renderGenerativeUi({ tool: 'ui.chart', rawRes: bars('x'), ctx: ctx() })}</>);
    expect(junk.container.querySelectorAll('rect')).toHaveLength(5); // unparsable count → all points
    const zero = render(<>{renderGenerativeUi({ tool: 'ui.chart', rawRes: bars(0), ctx: ctx() })}</>);
    expect(zero.container.querySelectorAll('rect')).toHaveLength(1); // at least one always drawn
  });

  it('malformed or empty series renders the header only — no svg, no crash', () => {
    const el = renderGenerativeUi({
      tool: 'ui.chart',
      rawRes: JSON.stringify({ $type: 'chart', label: 'Revenue', value: '$1', points: 'nope' }),
      ctx: ctx(),
    });
    const { container } = render(<>{el}</>);
    expect(container.querySelector('svg')).toBeNull();
    expect(container.textContent).toContain('Revenue');
    // Present-only: nothing at all to show → no card.
    expect(
      renderGenerativeUi({ tool: 'ui.chart', rawRes: '{"$type":"chart"}', ctx: ctx() })
    ).toBeNull();
  });
});

describe('timeline card (7.4/7.5)', () => {
  it('renders events in envelope order with settled vs reference styling', () => {
    const envelope = {
      $type: 'timeline',
      title: 'Launch plan',
      events: [
        { label: 'Kickoff', at: '2026-01-05T09:00:00Z', state: 'done' },
        { label: 'Review', at: '2099-01-05T09:00:00Z', state: 'upcoming' },
        { label: 'TBD' },
      ],
    };
    const { container } = render(
      <>{renderGenerativeUi({ tool: 'ui.timeline', rawRes: JSON.stringify(envelope), ctx: ctx({ tool: 'ui.timeline' }) })}</>
    );
    const items = container.querySelectorAll('li');
    const labelOf = (li: Element) => (li.querySelector('p') as HTMLElement).textContent;
    expect(labelOf(items[0])).toBe('Kickoff');
    expect(labelOf(items[1])).toBe('Review');
    expect(labelOf(items[2])).toBe('TBD');
    // Settled: filled accent dot; reference/upcoming: hollow muted dot.
    const dots = container.querySelectorAll('li span[aria-hidden]');
    expect(dots[0].className).toContain('bg-accent');
    expect(dots[1].className).toContain('border-muted');
    expect(dots[2].className).toContain('border-muted');
  });

  it('an unusable event list falls back to the generic card', () => {
    expect(
      renderGenerativeUi({ tool: 'ui.timeline', rawRes: '{"$type":"timeline","events":"nope"}', ctx: ctx({ tool: 'ui.timeline' }) })
    ).toBeNull();
    expect(
      renderGenerativeUi({ tool: 'ui.timeline', rawRes: '{"$type":"timeline","events":[]}', ctx: ctx({ tool: 'ui.timeline' }) })
    ).toBeNull();
  });
});

describe('web preview card (7.3/7.5)', () => {
  const envelope = { $type: 'preview', url: 'https://example.com/app', html: '<p>hi</p>' };

  it('while the producing call runs the element renders nothing', () => {
    expect(
      renderGenerativeUi({ tool: 'ui.preview', rawArgs: JSON.stringify(envelope), ctx: ctx({ tool: 'ui.preview', running: true }) })
    ).toBeNull();
  });

  it('renders sandboxed chrome: host in the URL bar, no allow-same-origin', () => {
    const { container } = render(
      <>{renderGenerativeUi({ tool: 'ui.preview', rawRes: JSON.stringify(envelope), ctx: ctx({ tool: 'ui.preview' }) })}</>
    );
    expect(container.textContent).toContain('example.com');
    const frame = container.querySelector('iframe') as HTMLIFrameElement;
    expect(frame).not.toBeNull();
    const sandbox = frame.getAttribute('sandbox') ?? '';
    expect(sandbox).toContain('allow-scripts');
    expect(sandbox).not.toContain('allow-same-origin');
    expect(frame.getAttribute('srcdoc')).toContain('<p>hi</p>');
    const open = container.querySelector('a[aria-label="Open in new tab"]') as HTMLAnchorElement;
    expect(open.getAttribute('href')).toBe('https://example.com/app');
    expect(open.getAttribute('rel')).toContain('noreferrer');
  });

  it('reload remounts the frame by key', () => {
    const { container } = render(
      <>{renderGenerativeUi({ tool: 'ui.preview', rawRes: JSON.stringify(envelope), ctx: ctx({ tool: 'ui.preview' }) })}</>
    );
    const before = container.querySelector('iframe');
    fireEvent.click(container.querySelector('button[aria-label="Reload preview"]') as HTMLButtonElement);
    const after = container.querySelector('iframe');
    expect(after).not.toBeNull();
    expect(after).not.toBe(before);
  });

  it('failure renders a failure state, never a blank frame', () => {
    const errored = render(
      <>{renderGenerativeUi({ tool: 'ui.preview', rawArgs: JSON.stringify(envelope), ctx: ctx({ tool: 'ui.preview', error: true, errorText: 'invalid schema' }) })}</>
    );
    expect(errored.container.textContent).toContain('Preview failed');
    expect(errored.container.textContent).toContain('invalid schema');
    expect(errored.container.querySelector('iframe')).toBeNull();
    const empty = render(
      <>{renderGenerativeUi({ tool: 'ui.preview', rawRes: '{"$type":"preview"}', ctx: ctx({ tool: 'ui.preview' }) })}</>
    );
    expect(empty.container.textContent).toContain('Preview failed');
    expect(empty.container.querySelector('iframe')).toBeNull();
  });
});

describe('stagger reveal primitive (1.2)', () => {
  it('delays clamp so long lists finish promptly', () => {
    expect(revealDelay(0)).toBe(0);
    expect(revealDelay(3)).toBe(180);
    expect(revealDelay(999)).toBe(1200); // 20 steps × 60 ms
  });

  it('live arrivals stagger; hydrated renders are fully revealed immediately', async () => {
    const sources = {
      results: [
        { title: 'One', url: 'https://one.example' },
        { title: 'Two', url: 'https://two.example' },
      ],
    };
    const live = render(
      <>{renderGenerativeUi({ tool: 'web.search', rawArgs: '{"query":"q"}', rawRes: JSON.stringify(sources), ctx: ctx({ tool: 'web.search', live: true }) })}</>
    );
    expect(live.container.querySelectorAll('.od-genui-reveal')).toHaveLength(2);
    const hydrated = render(
      <>{renderGenerativeUi({ tool: 'web.search', rawArgs: '{"query":"q"}', rawRes: JSON.stringify(sources), ctx: ctx({ tool: 'web.search', live: false }) })}</>
    );
    expect(hydrated.container.querySelectorAll('.od-genui-reveal')).toHaveLength(0);
    expect(hydrated.container.querySelectorAll('li')).toHaveLength(2);
  });
});

// ---------------------------------------------------------------------------
// Fence transport mounts (markdown-card-elements 2.1/2.8, design D1/D3): each
// of the 14 tags mounts its element through renderSpecByType with a validated
// body from parseFence — the same registered renderers the legacy $type path
// above exercises. Mermaid/diagram run on the mocked lazy engine.
// ---------------------------------------------------------------------------

const FENCE_BODY: Record<string, string> = {
  chart: '{"label":"Revenue","value":"$12.4k","delta":"+3","variant":"area","points":[1,2,3,4]}',
  timeline: '{"title":"Launch","events":[{"label":"Kickoff","at":"2026-01-05T09:00:00Z","state":"settled"},{"label":"Review","state":"reference"}]}',
  preview: '{"url":"https://example.com/app","html":"<p>hi</p>","title":"App"}',
  table: '{"caption":"Models","columns":[{"key":"name","label":"Model"},{"key":"cost","label":"Cost"}],"rows":[{"name":"Atlas","cost":"$3"},{"name":"Beacon","cost":"$1"}]}',
  ticker: '{"value":4200,"label":"stars"}',
  activity: '{"title":"Deploys","total":12,"start":"2026-08-01","end":"2026-08-28","data":[{"date":"2026-08-03","count":2},{"date":"2026-08-10","count":5}]}',
  spec: '{"title":"Atlas","subtitle":"resolver agent","rows":[{"label":"Runtime","value":"go 1.27"},{"label":"Region","value":"iad","emphasis":true}]}',
  compare: '{"traitLabels":["Context","Price"],"options":[{"id":"a","name":"Atlas","headline":"resolver","traits":["200k",false]},{"id":"b","name":"Beacon","headline":"scout","traits":["128k","$1"]}],"recommendedId":"a","reason":"Atlas wins on context."}',
  progress: '{"title":"Indexing","stages":[{"name":"fetch","weight":1},{"name":"embed","weight":3}],"stageIndex":1,"stageProgress":50,"eta":"2m"}',
  score: '{"verdict":"solid","total":7.5,"outOf":10,"criteria":[{"label":"Latency","score":8,"weight":2,"note":"p95 under 900ms"}]}',
  flow: '{"nodes":[{"id":"a","label":"fetch","column":0,"row":0,"state":"done"},{"id":"b","label":"embed","column":1,"row":0,"state":"active"}],"edges":[{"from":"a","to":"b"}]}',
  math: '{"label":"Bayes","steps":[{"expression":"P(A|B)=\\\\frac{P(B|A)P(A)}{P(B)}","note":"posterior"}]}',
  mermaid: 'graph TD;A-->B',
  diagram: 'graph LR;X-->Y',
};

describe('fence transport mounts (2.1/2.8)', () => {
  const mount = (tag: string) => {
    const parsed = parseFence(tag, FENCE_BODY[tag], tag === 'diagram' ? 'Payment flow' : null);
    if (parsed.ok === false) throw new Error(`${tag} fixture rejected: ${parsed.reason}`);
    return renderSpecByType(tag, parsed.props, ctx({ tool: 'fence-' + tag }));
  };

  it('chart mounts the chart card with its sparkline', () => {
    const { container } = render(<>{mount('chart')}</>);
    expect(container.querySelector('[data-od-id="tool-fence-chart"]')).not.toBeNull();
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('timeline mounts the timeline card in declared event order', () => {
    const { container } = render(<>{mount('timeline')}</>);
    expect(container.querySelector('[data-od-id="tool-fence-timeline"]')).not.toBeNull();
    expect(container.textContent).toContain('Kickoff');
    expect(container.querySelectorAll('li')).toHaveLength(2);
  });

  it('preview mounts the sandboxed preview chrome', () => {
    const { container } = render(<>{mount('preview')}</>);
    expect(container.querySelector('iframe')?.getAttribute('sandbox')).toContain('allow-scripts');
    expect(container.textContent).toContain('example.com');
  });

  it('table mounts the generalized data table', () => {
    const { container } = render(<>{mount('table')}</>);
    expect(container.querySelector('[data-slot="data-table"]')).not.toBeNull();
    expect(container.textContent).toContain('Models');
    expect(container.textContent).toContain('Atlas');
    expect(container.textContent).toContain('$3');
  });

  it('ticker mounts the number ticker with a formatted value', () => {
    const { container } = render(<>{mount('ticker')}</>);
    expect(container.querySelector('[data-slot="number-ticker"]')).not.toBeNull();
    // The rolling digits stack 0-9 per place (textContent lists them all);
    // the formatted value is announced on the value span.
    expect(container.querySelector('[aria-label="4,200"]')).not.toBeNull();
    expect(container.textContent).toContain('stars');
  });

  it('activity mounts the heat graph', () => {
    const { container } = render(<>{mount('activity')}</>);
    expect(container.querySelector('[data-slot="activity-graph"]')).not.toBeNull();
    expect(container.textContent).toContain('Deploys');
    expect(container.textContent).toContain('12');
  });

  it('spec mounts the spec sheet with its rows', () => {
    const { container } = render(<>{mount('spec')}</>);
    expect(container.querySelector('[data-slot="spec-sheet"]')).not.toBeNull();
    expect(container.textContent).toContain('go 1.27');
  });

  it('compare mounts the comparison card with the recommendation marked', () => {
    const { container } = render(<>{mount('compare')}</>);
    expect(container.querySelector('[data-slot="comparison-card"]')).not.toBeNull();
    expect(container.textContent).toContain('pick');
    expect(container.textContent).toContain('Atlas wins on context.');
  });

  it('progress mounts job progress with the 0–100 fence value scaled to the element fraction', () => {
    const { container } = render(<>{mount('progress')}</>);
    expect(container.querySelector('[data-slot="job-progress"]')).not.toBeNull();
    // stageIndex 1 of [1,3], stageProgress 50% → completed 1 + 3·0.5 of 4 = 62.5%.
    const bar = container.querySelector('[role="progressbar"]') as HTMLElement;
    expect(bar.getAttribute('aria-valuenow')).toBe('62.5');
    // A fence has nothing to cancel — the element's cancel control stays inert.
    const cancel = container.querySelector('button[aria-label="Cancel the job"]') as HTMLButtonElement;
    expect(cancel).not.toBeNull();
    expect(cancel.onclick).toBeNull();
  });

  it('score mounts the score breakdown with its verdict', () => {
    const { container } = render(<>{mount('score')}</>);
    expect(container.querySelector('[data-slot="score-breakdown"]')).not.toBeNull();
    expect(container.textContent).toContain('solid');
    expect(container.textContent).toContain('7.5');
  });

  it('flow mounts the flow graph with every node visible', () => {
    const { container } = render(<>{mount('flow')}</>);
    expect(container.querySelector('[data-slot="flow-graph"]')).not.toBeNull();
    expect(container.textContent).toContain('fetch');
    expect(container.textContent).toContain('embed');
  });

  it('math mounts the math block and maps LaTeX through the KaTeX lane', async () => {
    const { container } = render(<>{mount('math')}</>);
    expect(container.querySelector('[data-slot="math-block"]')).not.toBeNull();
    // Real katex, same lazy chunk the MarkdownBody math lane uses — the
    // stylesheet import is idempotent module cache.
    await waitFor(() => expect(container.querySelector('.katex')).not.toBeNull());
  });

  it('mermaid upgrades from raw source to the mocked lazy engine render', async () => {
    const { container } = render(<>{mount('mermaid')}</>);
    expect(container.querySelector('svg')).toBeNull(); // degraded first paint (D9)
    await waitFor(() => expect(container.querySelector('[data-slot="mermaid-diagram"]')).not.toBeNull());
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('diagram wraps the mermaid render in the zoom chrome with the info-string title', async () => {
    const { container } = render(<>{mount('diagram')}</>);
    expect(container.querySelector('[data-slot="diagram"]')).not.toBeNull();
    expect(container.textContent).toContain('Payment flow');
    expect(container.querySelector('button[aria-label="Zoom in"]')).not.toBeNull();
    await waitFor(() => expect(container.querySelector('[data-slot="diagram"] svg')).not.toBeNull());
  });

  it('a garbage body reaching the registry mints nothing — renderer tolerance', () => {
    expect(renderSpecByType('table', { columns: 'junk', rows: 4 }, ctx())).toBeNull();
    expect(renderSpecByType('ticker', { value: '4', label: 2 }, ctx())).toBeNull();
    expect(renderSpecByType('flow', { nodes: [], edges: 'x' }, ctx())).toBeNull();
    expect(renderSpecByType('mermaid', { code: '   ' }, ctx())).toBeNull();
    expect(renderSpecByType('diagram', { code: 'graph TD', title: '' }, ctx())).toBeNull();
  });
});
