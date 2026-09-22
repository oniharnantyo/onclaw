import { describe, it, expect } from 'vitest';
import { extractFrontmatter, rewriteRelativeAssets } from './prerender';

// Prefixes every URL it sees — makes "was resolve() consulted" observable.
const rebase = (u: string) => `/files/src/${u}`;

describe('extractFrontmatter', () => {
  it('returns null meta and the untouched body when there is no frontmatter', () => {
    const text = '# Hello\n\n![img](a.png)\n';
    expect(extractFrontmatter(text)).toEqual({ meta: null, body: text });
  });

  it('parses flat key: value pairs and returns the body after the closing fence', () => {
    const r = extractFrontmatter('---\ntitle: Incident 42\nowner: atlas\n---\n# Body\n');
    expect(r.meta).toEqual({ title: 'Incident 42', owner: 'atlas' });
    expect(r.body).toBe('# Body\n');
  });

  it('tolerates CRLF fences and trailing spaces/tabs on fence lines', () => {
    const r = extractFrontmatter('---  \r\ntitle: A\r\n---\t\r\nbody text');
    expect(r.meta).toEqual({ title: 'A' });
    expect(r.body).toBe('body text');
  });

  it('keeps a --- inside a value from closing the block early', () => {
    const r = extractFrontmatter('---\ntitle: Run --- notes\nstatus: open\n---\nbody');
    expect(r.meta).toEqual({ title: 'Run --- notes', status: 'open' });
    expect(r.body).toBe('body');
  });

  it('treats an opening fence with no closing fence as no frontmatter', () => {
    const text = '---\ntitle: never closed\nmore text';
    expect(extractFrontmatter(text)).toEqual({ meta: null, body: text });
  });

  it('requires the opening fence to be the very first line', () => {
    for (const text of ['\n---\ntitle: x\n---\nbody', ' ---\ntitle: x\n---\nbody']) {
      expect(extractFrontmatter(text)).toEqual({ meta: null, body: text });
    }
  });

  it('does not treat ---- or other near-fences as a fence', () => {
    const text = '----\ntitle: x\n';
    expect(extractFrontmatter(text)).toEqual({ meta: null, body: text });
  });

  it('strips matching surrounding quotes from values but leaves others', () => {
    const r = extractFrontmatter('---\ntitle: "Quoted name"\ntag: \'single\'\nplain: no quotes"\n---\nb');
    expect(r.meta).toEqual({ title: 'Quoted name', tag: 'single', plain: 'no quotes"' });
  });

  it('keeps nested structures as raw string continuations of the previous key', () => {
    const r = extractFrontmatter('---\ntags:\n  - ops\n  - incidents\nauthor: kim\n---\nb');
    expect(r.meta).toEqual({ tags: '  - ops\n  - incidents', author: 'kim' });
    expect(r.body).toBe('b');
  });

  it('returns an empty meta object for an empty frontmatter block', () => {
    const r = extractFrontmatter('---\n---\nonly body');
    expect(r.meta).toEqual({});
    expect(r.body).toBe('only body');
  });

  it('yields an empty body when the file ends at the closing fence', () => {
    const r = extractFrontmatter('---\ntitle: x\n---');
    expect(r.meta).toEqual({ title: 'x' });
    expect(r.body).toBe('');
  });

  it('trims whitespace around keys and values (without leading indentation)', () => {
    const r = extractFrontmatter('---\nspaced key :   spaced value  \n---\nb');
    expect(r.meta).toEqual({ 'spaced key': 'spaced value' });
  });

  it('keeps colons inside values (times, paths, URLs)', () => {
    const r = extractFrontmatter('---\ntime: 12:30\npath: C:\\dir\\file\nurl: https://x.io/a\n---\nb');
    expect(r.meta).toEqual({ time: '12:30', path: 'C:\\dir\\file', url: 'https://x.io/a' });
  });

  it('ignores stray non-pair lines before any key', () => {
    const r = extractFrontmatter('---\njust text\nkey: v\n---\nb');
    expect(r.meta).toEqual({ key: 'v' });
  });
});

describe('rewriteRelativeAssets — markdown images and links', () => {
  it('rewrites relative image and link URLs', () => {
    expect(rewriteRelativeAssets('![chart](img/a.png) and [doc](notes.md)', rebase))
      .toBe('![chart](/files/src/img/a.png) and [doc](/files/src/notes.md)');
  });

  it('leaves absolute, protocol-relative, data, mailto and anchor URLs untouched', () => {
    const md =
      '![x](https://e.com/i.png) ![y](//e.com/i.png) ![z](data:image/png;base64,AAA) ' +
      '[m](mailto:a@b.c) [j](#anchor)';
    expect(rewriteRelativeAssets(md, rebase)).toBe(md);
  });

  it('does not consult resolve for non-rewritten URLs', () => {
    const seen: string[] = [];
    const out = rewriteRelativeAssets('![a](https://e.com/i.png) [b](c.md)', (u) => {
      seen.push(u);
      return 'R(' + u + ')';
    });
    expect(seen).toEqual(['c.md']);
    expect(out).toBe('![a](https://e.com/i.png) [b](R(c.md))');
  });

  it('passes the exact original URL to resolve, including angle-wrapped spaces', () => {
    const seen: string[] = [];
    rewriteRelativeAssets('![a](dir/name.png) [b](<spaced name.md>)', (u) => {
      seen.push(u);
      return 'R';
    });
    expect(seen).toEqual(['dir/name.png', 'spaced name.md']);
  });

  it('keeps the empty destination untouched', () => {
    expect(rewriteRelativeAssets('[t]() ![i]()', rebase)).toBe('[t]() ![i]()');
  });

  it('preserves link titles and whitespace shaping', () => {
    expect(rewriteRelativeAssets('[t]( a.md "Title" )', rebase))
      .toBe('[t](/files/src/a.md "Title" )');
  });
});

describe('rewriteRelativeAssets — code exclusion', () => {
  it('does not touch URLs inside backtick fences', () => {
    const md = '```\n![x](a.png)\n[a](b.md)\n```\n![y](c.png)';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('```\n![x](a.png)\n[a](b.md)\n```\n![y](/files/src/c.png)');
  });

  it('does not touch URLs inside tilde fences, which backtick lines cannot close', () => {
    const md = '~~~\n[x](a.md)\n```\n~~~\n[y](b.md)';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('~~~\n[x](a.md)\n```\n~~~\n[y](/files/src/b.md)');
  });

  it('honors fence info strings and closes only on a lone marker line', () => {
    const md = '```ts\nconst s = "[a](b.md)";\n```  \n[c](d.md)';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('```ts\nconst s = "[a](b.md)";\n```  \n[c](/files/src/d.md)');
  });

  it('keeps indented fences (up to three spaces) verbatim', () => {
    const md = '   ```\n[x](a.md)\n   ```\n[y](b.md)';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('   ```\n[x](a.md)\n   ```\n[y](/files/src/b.md)');
  });

  it('does not open a fence at four-space indentation, so content rewrites', () => {
    const md = '    ```\n[x](a.md)\n';
    expect(rewriteRelativeAssets(md, rebase)).toBe('    ```\n[x](/files/src/a.md)\n');
  });

  it('never recovers content from an unterminated fence', () => {
    const md = '```\n[x](a.md)\nnever closed';
    expect(rewriteRelativeAssets(md, rebase)).toBe(md);
  });

  it('skips inline code spans', () => {
    const md = 'Use `[a](b.md)` or ![i](c.png) inline';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('Use `[a](b.md)` or ![i](/files/src/c.png) inline');
  });

  it('handles double-backtick spans containing single backticks and links', () => {
    const md = '``see `x` [c](d.md)`` [e](f.md)';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('``see `x` [c](d.md)`` [e](/files/src/f.md)');
  });

  it('treats an unterminated backtick run as literal and still rewrites later links', () => {
    expect(rewriteRelativeAssets('` [a](b.md)', rebase)).toBe('` [a](/files/src/b.md)');
  });
});

describe('rewriteRelativeAssets — HTML and composition', () => {
  it('rewrites HTML img src and a href with double and single quotes', () => {
    expect(rewriteRelativeAssets('<img src="pics/a.png"> <a href=\'docs/b.html\'>x</a>', rebase))
      .toBe('<img src="/files/src/pics/a.png"> <a href=\'/files/src/docs/b.html\'>x</a>');
  });

  it('leaves absolute and data URLs in HTML attributes untouched', () => {
    const md = '<img src="https://e.com/i.png"> <img src=data:c.png> <a href="#top">y</a>';
    expect(rewriteRelativeAssets(md, rebase)).toBe(md);
  });

  it('matches HTML tag names case-insensitively', () => {
    expect(rewriteRelativeAssets('<IMG SRC="a.png" ALT="x"> and <A HREF="b.html">l</A>', rebase))
      .toBe('<IMG SRC="/files/src/a.png" ALT="x"> and <A HREF="/files/src/b.html">l</A>');
  });

  it('rewrites HTML inside no tag boundary confusion (data-src is not src)', () => {
    expect(rewriteRelativeAssets('<img data-src="keep.png" src="rewrite.png">', rebase))
      .toBe('<img data-src="keep.png" src="/files/src/rewrite.png">');
  });

  it('rewrites both the image and the target of an image inside a link', () => {
    expect(rewriteRelativeAssets('[![alt](img.png)](page.html)', rebase))
      .toBe('[![alt](/files/src/img.png)](/files/src/page.html)');
  });

  it('rewrites a mixed document across CRLF line endings', () => {
    const md = '# T\r\n\r\n![i](a.png)\r\n\r\n```js\r\n![no](b.png)\r\n```\r\n[l](c.md "Title")\r\n';
    expect(rewriteRelativeAssets(md, rebase))
      .toBe('# T\r\n\r\n![i](/files/src/a.png)\r\n\r\n```js\r\n![no](b.png)\r\n```\r\n[l](/files/src/c.md "Title")\r\n');
  });

  it('leaves text without any references byte-identical', () => {
    const md = 'Plain text, `ticks`, and a ](stray.md) paren.\n';
    expect(rewriteRelativeAssets(md, rebase)).toBe(md);
  });
});
