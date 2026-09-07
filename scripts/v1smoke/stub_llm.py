#!/usr/bin/env python3
"""Stub OpenAI-compatible LLM for the /v1 wire smoke.

The eino agenticopenai client speaks the Chat Completions API with
stream:true. Scripted by the last user message text:

  "APPROVE"  -> tool call to `execute` with a dangerous command (HITL path)
  "TOOLRUN"  -> tool call to `execute` with a safe command
  otherwise  -> plain assistant text

When the conversation already carries a role=="tool" message (the tool ran),
the stub returns the post-tool assistant text. promptgen calls arrive without
those markers and receive the identity/soul/bootstrap JSON contract.
"""
import json
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer

TEXT = "stub says hi from wire smoke"
POST_TOOL_TEXT = "stub finished after tool"
GEN_TEXT = json.dumps({
    "identity": "Atlas is the OnClaw wire-smoke runner.",
    "soul": "Terse, precise, and helpful.",
    "bootstrap": "Run the smoke checklist on start.",
})


def last_user_text(messages):
    out = ""
    for m in messages:
        if m.get("role") == "user":
            c = m.get("content")
            out = c if isinstance(c, str) else json.dumps(c)
    return out


def scripted_reply(messages):
    # Phase on the LAST message only: the full replayed history may contain
    # older tool results, but a continuation turn always ends with one.
    msgs = [m for m in (messages or []) if m.get("role") in ("user", "tool", "assistant")]
    if msgs and msgs[-1].get("role") == "tool":
        return {"kind": "text", "text": POST_TOOL_TEXT}
    text = last_user_text(messages)
    if "APPROVE" in text:
        # A unique command per run: the durable approval ledger remembers
        # previously-approved commands, so a fixed one would auto-approve.
        return {"kind": "tool", "name": "execute",
                "arguments": json.dumps({"command": "rm -rf ./v1smoke-target-" + uuid.uuid4().hex[:8]})}
    if "TOOLRUN" in text:
        return {"kind": "tool", "name": "execute",
                "arguments": json.dumps({"command": "echo wire-smoke-tool-ok"})}
    if "identity" in text.lower() and "soul" in text.lower():
        return {"kind": "text", "text": GEN_TEXT}
    return {"kind": "text", "text": TEXT}


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        if self.path.endswith("/chat/completions"):
            return self.chat_completions(body)
        self.send_error(404)

    def chat_completions(self, body):
        reply = scripted_reply(body.get("messages") or [])
        cid = "chatcmpl_" + uuid.uuid4().hex[:10]
        model = body.get("model", "stub-1")

        if not body.get("stream"):
            if reply["kind"] == "tool":
                msg = {"role": "assistant", "content": None,
                       "tool_calls": [{"id": "call_" + uuid.uuid4().hex[:8], "type": "function",
                                       "function": {"name": reply["name"], "arguments": reply["arguments"]}}]}
            else:
                msg = {"role": "assistant", "content": reply["text"]}
            return self.json({
                "id": cid, "object": "chat.completion", "created": 0, "model": model,
                "choices": [{"index": 0, "finish_reason": "stop", "message": msg}],
                "usage": {"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21},
            })

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def chunk(delta, finish=None, usage=None):
            payload = {"id": cid, "object": "chat.completion.chunk", "created": 0, "model": model,
                       "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
            if usage is not None:
                payload["usage"] = usage
            self.wfile.write(f"data: {json.dumps(payload)}\n\n".encode())
            self.wfile.flush()

        chunk({"role": "assistant"})
        if reply["kind"] == "tool":
            chunk({"tool_calls": [{"index": 0, "id": "call_" + uuid.uuid4().hex[:8], "type": "function",
                                   "function": {"name": reply["name"], "arguments": ""}}]})
            chunk({"tool_calls": [{"index": 0, "function": {"arguments": reply["arguments"]}}]})
        else:
            text = reply["text"]
            for i in range(0, len(text), 8):
                chunk({"content": text[i:i + 8]})
        chunk({}, finish="stop", usage={"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21})
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def json(self, payload):
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    import sys
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9147
    HTTPServer(("127.0.0.1", port), Handler).serve_forever()
