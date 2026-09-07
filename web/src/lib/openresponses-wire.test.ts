import { describe, it, expect } from 'vitest';
import OpenAI from 'openai';

// node:http via a non-literal specifier: the web tsconfig has no node types,
// and a literal `import('node:http')` would fail `tsc -b`.
const httpSpec = 'node:http';

// Wire-level regression for the context meter: its live capture reads
// `usage.final_input_tokens` — a field the OpenAI SDK does not model — off the
// parsed `response.completed` event. Unlike the suites that mock
// `client.responses.create`, this drives the REAL SDK against a local
// data-only SSE server and pins the assumption that unmodeled fields survive
// parsing on known event types. (Data-only frames per the /v1 codec; the SDK
// ignores `event:` lines when resolving ev.type.)
describe('openresponses wire: unmodeled usage fields survive SDK parsing', () => {
  it('preserves usage.final_input_tokens on response.completed', async () => {
    const http: any = await import(httpSpec);
    const frames = [
      { type: 'response.created', response: { id: 'resp_1', object: 'response', status: 'in_progress' } },
      {
        type: 'response.completed',
        response: {
          id: 'resp_1',
          object: 'response',
          status: 'completed',
          usage: { input_tokens: 135, output_tokens: 5, total_tokens: 140, final_input_tokens: 120 },
        },
      },
    ];
    const server = http.createServer((req, res) => {
      res.writeHead(200, { 'content-type': 'text/event-stream' });
      for (const f of frames) res.write(`data: ${JSON.stringify(f)}\n\n`);
      res.write('data: [DONE]\n\n');
      res.end();
    });
    await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
    const addr = server.address() as { port: number };
    const client = new OpenAI({ apiKey: 'test', baseURL: `http://127.0.0.1:${addr.port}/v1`, dangerouslyAllowBrowser: true });
    const stream = await client.responses.create({ model: 'a', input: 'x', stream: true } as any);
    const seen: any[] = [];
    for await (const ev of stream as any) {
      seen.push(ev);
    }
    const completed = seen.find((e) => e.type === 'response.completed');
    expect(completed?.response?.usage?.final_input_tokens).toBe(120);
    expect(completed?.response?.usage?.input_tokens).toBe(135);
    server.close();
  });
});
