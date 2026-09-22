// Candidate matcher tests (add-right-panel task 3.4): realistic folded-card
// fixtures — the same shape livechat folds tool_call events into
// ({callId, name, args, res, ms, error}, args/res raw strings).
import { describe, expect, it } from 'vitest';
import { baseName, fileCandidateMatcher, fileCandidatesFromCard, jailRelativePath } from './candidates';

describe('file candidates — document.create', () => {
  it('maps the result envelope path to a jail-relative payload', () => {
    const card = { name: 'document.create', args: '{"path":"invoice.xlsx","format":"xlsx"}', res: '{"result":"Created invoice.xlsx (xlsx)","path":"invoice.xlsx","format":"xlsx"}', ms: 210 };
    expect(fileCandidateMatcher(card)).toEqual({
      kind: 'file',
      title: 'invoice.xlsx',
      payload: { path: 'invoice.xlsx' },
    });
  });

  it('strips the /workspace mount point from absolute tool paths', () => {
    const card = { name: 'document.create', args: '{"path":"/workspace/brief.md"}', res: undefined, ms: 40 };
    expect(fileCandidateMatcher(card)?.payload).toEqual({ path: 'brief.md' });
  });

  it('falls back to args.path while the result is still in flight', () => {
    const card = { name: 'document.create', args: '{"path":"reports/q3.pdf"}' };
    expect(fileCandidateMatcher(card)?.payload).toEqual({ path: 'reports/q3.pdf' });
    expect(fileCandidateMatcher(card)?.title).toBe('q3.pdf');
  });

  it('yields one candidate per file for a multi-file result', () => {
    const card = {
      name: 'document.create',
      args: '{"path":"report/"}',
      res: '{"files":[{"path":"report/part1.docx"},{"path":"report/part2.docx"},{"path":"report/charts.xlsx"}]}',
      ms: 900,
    };
    expect(fileCandidatesFromCard(card)).toEqual([
      { kind: 'file', title: 'part1.docx', payload: { path: 'report/part1.docx' } },
      { kind: 'file', title: 'part2.docx', payload: { path: 'report/part2.docx' } },
      { kind: 'file', title: 'charts.xlsx', payload: { path: 'report/charts.xlsx' } },
    ]);
    // One affordance per card — the first file is the one that opens.
    expect(fileCandidateMatcher(card)?.payload).toEqual({ path: 'report/part1.docx' });
  });

  it('rejects jail escapes and legacy envelopes without a path', () => {
    expect(fileCandidateMatcher({ name: 'document.create', args: '{"path":"../etc/passwd"}' })).toBeNull();
    expect(fileCandidateMatcher({ name: 'document.create', args: '{"name":"brief.docx","content":"# Brief"}', res: '{"name":"brief.docx"}' })).toBeNull();
  });
});

describe('file candidates — files.write', () => {
  it('maps the written path from args', () => {
    const card = { name: 'files.write', args: '{"path":"notes/meeting.md","content":"# Notes"}', res: '{"result":"ok"}', ms: 12 };
    expect(fileCandidateMatcher(card)).toEqual({
      kind: 'file',
      title: 'meeting.md',
      payload: { path: 'notes/meeting.md' },
    });
  });

  it('accepts the file_path alias and multi-file args', () => {
    expect(fileCandidateMatcher({ name: 'files.write', args: '{"file_path":"src/main.go"}' })?.payload).toEqual({ path: 'src/main.go' });
    expect(fileCandidatesFromCard({ name: 'files.write', args: '{"files":[{"path":"a.md"},{"path":"b.md"}]}' })).toHaveLength(2);
  });

  it('mints nothing for other tools, failures, or unparseable args', () => {
    expect(fileCandidateMatcher({ name: 'execute', args: '{"command":"ls"}', res: 'total 0' })).toBeNull();
    expect(fileCandidateMatcher({ name: 'files.write', args: '{"path":"x.md"}', error: 'jail escape' })).toBeNull();
    expect(fileCandidateMatcher({ name: 'files.write', args: 'not json' })).toBeNull();
    expect(fileCandidateMatcher({})).toBeNull();
  });
});

describe('file candidates — runtime filesystem tools', () => {
  // Payload shapes captured from live transcripts (internal/agents/tool_gate.go
  // registers ls/read_file/write_file/edit_file; args carry file_path).
  it('maps write_file file_path (mount-prefixed) to a jail-relative payload', () => {
    const card = { name: 'write_file', args: '{"content":"# Latest Posts","file_path":"/workspace/lilianweng_latest_posts.md"}', res: 'Updated file /workspace/lilianweng_latest_posts.md', ms: 1402 };
    expect(fileCandidateMatcher(card)).toEqual({
      kind: 'file',
      title: 'lilianweng_latest_posts.md',
      payload: { path: 'lilianweng_latest_posts.md' },
    });
  });

  it('maps edit_file file_path to a jail-relative payload', () => {
    const card = { name: 'edit_file', args: '{"file_path":"/workspace/timeline-scratch/a.txt","new_string":"x","old_string":"y"}', res: "Successfully replaced the string in '/workspace/timeline-scratch/a.txt'", ms: 1 };
    expect(fileCandidateMatcher(card)?.payload).toEqual({ path: 'timeline-scratch/a.txt' });
    expect(fileCandidateMatcher(card)?.title).toBe('a.txt');
  });

  it('maps read_file file_path (opening what the agent inspected)', () => {
    const card = { name: 'read_file', args: '{"file_path":"/workspace/timeline-scratch/a.txt","limit":10,"offset":1}', res: '     1\tfirst line of a', ms: 2 };
    expect(fileCandidateMatcher(card)?.payload).toEqual({ path: 'timeline-scratch/a.txt' });
  });

  it('accepts the path alias and rejects escapes/failures', () => {
    expect(fileCandidateMatcher({ name: 'write_file', args: '{"path":"notes/x.md"}' })?.payload).toEqual({ path: 'notes/x.md' });
    expect(fileCandidateMatcher({ name: 'write_file', args: '{"file_path":"/workspace/../etc/passwd"}' })).toBeNull();
    expect(fileCandidateMatcher({ name: 'write_file', args: '{"file_path":"/workspace/x.md"}', error: 'disk full' })).toBeNull();
    expect(fileCandidateMatcher({ name: 'ls', args: '{"path":"/workspace"}' })).toBeNull();
  });
});

describe('path helpers', () => {
  it('jailRelativePath normalizes mount prefixes, slashes, and rejects ..', () => {
    expect(jailRelativePath('/workspace/a/b.md')).toBe('a/b.md');
    expect(jailRelativePath('/a/b.md')).toBe('a/b.md');
    expect(jailRelativePath('a/b.md')).toBe('a/b.md');
    expect(jailRelativePath('')).toBeNull();
    expect(jailRelativePath('../escape')).toBeNull();
    expect(jailRelativePath('/workspace')).toBeNull();
  });

  it('baseName returns the filename', () => {
    expect(baseName('report/charts.xlsx')).toBe('charts.xlsx');
    expect(baseName('README')).toBe('README');
  });
});
