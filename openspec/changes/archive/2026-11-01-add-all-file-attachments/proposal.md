# Proposal

## Why

The chat composer rejects most files: the picker only offers images, PDFs, videos, and text, and the client-side mirror rejects everything else before upload. A user who attaches a .docx (or any other document) cannot send it, even though the backend storage and the turn path already accept office documents (add-document-editing task 5.5) and `read_file` already reads arbitrary workspace files. Chat should accept any file and let the existing degradation machinery decide what the model receives.

## What Changes

- The composer's file picker drops its `accept` filter: any file is selectable (drag-and-drop and paste are already unfiltered).
- The attachment store gains a catch-all **file lane**: any mime not in the image/PDF/video/text/office lanes — including an empty mime — is accepted up to 20 MB (the office/PDF/video cap). The "unsupported file type" rejection disappears; only oversize and per-message-count rejections remain.
- The turn delivers file-lane attachments (and office attachments, unchanged) as a reference pointer note, never as content parts: the note names that the bytes are not sent and addresses `/workspace/uploads/<name>` where the filesystem tools read the file (images ride as parts, PDFs/video per the carriage matrix — all unchanged).
- The frontend lane mirror matches: known lanes keep their caps, everything else takes the file lane's 20 MB; rejection copy and tests updated. No composer capability warning is added for the file lane — it never rides by design, like the office lane today.
- `chat-attachments` spec updated: selection requirement covers all file types and the new cap; the degradation requirement states the never-ride rule for office and file lanes.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `chat-attachments`: "Attachment selection and limits" accepts every file type with a catch-all file lane; "Model capability degradation" gains the always-pointer rule for office and file lanes.

## Impact

- `internal/attachments/attachments.go` (lane classifier + caps + rejection copy), `internal/agents/turninput.go` (pointer-note reason + lane name), their tests.
- `frontend/src/features/chat/runtime/useChatRuntime.ts` (picker accept), `frontend/src/features/chat/attachments.ts` (lane mirror), `frontend/src/features/chat/composer/ChatComposer.tsx` (no change expected — warnings are per-kind), `frontend/src/features/chat/__test__/attachments.test.tsx`.
- No binding changes: the `chat.saveAttachment` call shape is unchanged.
