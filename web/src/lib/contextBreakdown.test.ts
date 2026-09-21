import { describe, it, expect } from 'vitest';
import { computeContextBreakdown, postCompactionEntries } from './contextBreakdown';

const agent = {
  prompt: 'x'.repeat(400), // 100 tokens estimated (~4 chars/token)
  role: 'y'.repeat(80), // 20
  tools: ['a', 'b', 'c'], // 3 × 120 = 360
  skills: ['s1'], // 40
};
// Face-value estimate sum: 100 + 20 + 360 + 40 = 520
const AGENT_ESTIMATE = 520;

describe('computeContextBreakdown — honest remainder (D2, replaces scale-to-fit)', () => {
  it('keeps segments at face value — never scaled to fit the reported total', () => {
    const { segments, serverContext, headroom } = computeContextBreakdown(agent, [], 50_000, 200_000);
    const sum = segments.reduce((s, seg) => s + seg.tokens, 0);
    expect(sum).toBe(AGENT_ESTIMATE);
    // The remainder is Server context, not a silent rescale of the rows.
    expect(serverContext).toBe(50_000 - AGENT_ESTIMATE);
    expect(headroom).toBe(150_000);
  });

  it('emits the documented categories in order when data exists', () => {
    const messages = [
      { text: 'z'.repeat(200), tools: [{ args: 'a'.repeat(100), res: 'r'.repeat(300) }], attachments: [{ mime: 'image/png', size: 9_000 }] },
      { text: 'w'.repeat(400) },
    ];
    const { segments } = computeContextBreakdown(agent, messages, 10_000, 100_000);
    expect(segments.map((s) => s.label)).toEqual(['Instructions', 'Tools & skills', 'Files', 'Conversation']);
    // Legend dot and bar slice share one tint class verbatim.
    expect(new Set(segments.map((s) => s.tint)).size).toBe(segments.length);
  });

  it('excludes categories with no visible share instead of drawing zero-width slices', () => {
    const { segments } = computeContextBreakdown(undefined, [], 5_000, 100_000);
    expect(segments).toEqual([]);
  });

  it('floors headroom at zero when usage exceeds the window', () => {
    const { headroom } = computeContextBreakdown(agent, [], 250_000, 200_000);
    expect(headroom).toBe(0);
  });

  it('floors Server context at zero when estimates sum over used — still estimated', () => {
    const bigAgent = { prompt: 'x'.repeat(400_000) }; // 100k tokens estimated
    const { segments, serverContext } = computeContextBreakdown(bigAgent, [], 5_000, 200_000);
    expect(serverContext).toBe(0);
    // Face value survives the overshoot — the rows are NOT shrunk to fit.
    expect(segments[0].tokens).toBe(100_000);
    expect(segments.every((s) => s.estimated)).toBe(true);
  });

  it('applies the documented estimator constants (~4 chars/token, 8 tok/message, 120/tool, 40/skill, 1100/image)', () => {
    const messages = [
      { text: 'c'.repeat(200), reasoning: 'r'.repeat(40), tools: [{ args: 'a'.repeat(80), res: 'b'.repeat(40) }] },
    ];
    const attachmentMessages = [{ attachments: [{ mime: 'image/png', size: 9_000 }, { mime: 'text/plain', size: 400 }] }];
    const { segments } = computeContextBreakdown(
      { prompt: '', role: '', tools: ['t'], skills: [] },
      [...messages, ...attachmentMessages],
      99_999,
      200_000
    );
    const byLabel = (label: string) => segments.find((s) => s.label === label)?.tokens ?? 0;
    // Instructions: empty prompt/role → 0 → excluded
    expect(segments.some((s) => s.label === 'Instructions')).toBe(false);
    expect(byLabel('Tools & skills')).toBe(120);
    // Files: 1100 image + max(64, ceil(400/4)=100) doc
    expect(byLabel('Files')).toBe(1100 + 100);
    // Conversation: (8 + 50 + 10) + (8 + 20 + 10) = 106
    expect(byLabel('Conversation')).toBe(8 + 50 + 10 + 8 + 20 + 10);
  });
});

describe('computeContextBreakdown — compaction (D2: conversation counts only post-divider entries)', () => {
  it('counts only entries after the latest compaction divider', () => {
    const messages = [
      { text: 'q'.repeat(4000) }, // pre-compaction: 1000 tokens, must not count
      { author: 'compaction', text: '', compaction: { tokensBefore: 154_000, tokensAfter: 9_200 } },
      { text: 'z'.repeat(200) }, // 8 + 50
    ];
    const { segments } = computeContextBreakdown(undefined, messages, 50_000, 200_000);
    const conversation = segments.find((s) => s.label === 'Conversation');
    expect(conversation?.tokens).toBe(8 + 50);
  });

  it('uses every entry when no divider exists', () => {
    const messages = [{ text: 'z'.repeat(200) }, { text: 'w'.repeat(200) }];
    expect(postCompactionEntries(messages)).toEqual(messages);
    const { segments } = computeContextBreakdown(undefined, messages, 50_000, 200_000);
    expect(segments.find((s) => s.label === 'Conversation')?.tokens).toBe(2 * (8 + 50));
  });

  it('counts zero conversation when the divider is the newest entry', () => {
    const messages = [{ text: 'z'.repeat(200) }, { author: 'compaction', text: '' }];
    const { segments } = computeContextBreakdown(undefined, messages, 50_000, 200_000);
    expect(segments.some((s) => s.label === 'Conversation')).toBe(false);
  });

  it('counts attachments from after the divider only (Files segment)', () => {
    const messages = [
      { attachments: [{ mime: 'image/png', size: 1 }] },
      { author: 'compaction', text: '' },
      { attachments: [{ mime: 'application/pdf', size: 400 }] },
    ];
    const { segments } = computeContextBreakdown(undefined, messages, 50_000, 200_000);
    expect(segments.find((s) => s.label === 'Files')?.tokens).toBe(100);
  });
});

describe('computeContextBreakdown — server-provided breakdown wins when present (D7)', () => {
  it('replaces the client estimate for the reported segments and drops the estimated caption', () => {
    const { segments, serverContext } = computeContextBreakdown(
      agent,
      [],
      50_000,
      200_000,
      { instructions: 1_200, tools: 3_400, conversation: 12_000 }
    );
    const byLabel = Object.fromEntries(segments.map((s) => [s.label, s]));
    expect(byLabel['Instructions']).toMatchObject({ tokens: 1_200, estimated: false });
    expect(byLabel['Tools & skills']).toMatchObject({ tokens: 3_400, estimated: false });
    expect(byLabel['Conversation']).toMatchObject({ tokens: 12_000, estimated: false });
    // No files in this fixture → no Files row at all.
    expect(byLabel['Files']).toBeUndefined();
    expect(serverContext).toBe(50_000 - (1_200 + 3_400 + 12_000));
  });

  it('keeps the client estimate (caption "estimated") for segments the wire did not report', () => {
    const messages = [{ attachments: [{ mime: 'image/png', size: 1 }] }];
    const { segments } = computeContextBreakdown(
      undefined,
      messages,
      20_000,
      200_000,
      { conversation: 4_000 }
    );
    const files = segments.find((s) => s.label === 'Files');
    expect(files).toMatchObject({ tokens: 1_100, estimated: true });
    const conversation = segments.find((s) => s.label === 'Conversation');
    expect(conversation).toMatchObject({ tokens: 4_000, estimated: false });
  });

  it('falls back to the full client estimate when the wire field is absent (today)', () => {
    const { segments } = computeContextBreakdown(agent, [], 50_000, 200_000, undefined);
    expect(segments.every((s) => s.estimated)).toBe(true);
    expect(segments.reduce((s, seg) => s + seg.tokens, 0)).toBe(AGENT_ESTIMATE);
  });

  it('falls back when the wire field exists but carries no usable numbers', () => {
    const { segments } = computeContextBreakdown(agent, [], 50_000, 200_000, {} as any);
    expect(segments.every((s) => s.estimated)).toBe(true);

    const junk = computeContextBreakdown(agent, [], 50_000, 200_000, {
      instructions: -5,
      tools: Number.NaN,
      files: 'lots' as unknown as number,
    });
    expect(junk.segments.every((s) => s.estimated)).toBe(true);
  });
});
