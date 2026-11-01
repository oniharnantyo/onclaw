# Tasks

## 1. Backend — catch-all file lane

- [x] 1.1 `internal/attachments/attachments.go`: add `LaneFile` ("file") and `MaxFileBytes = 20 << 20`; make `laneOf` return the file lane for every unknown mime (including the empty mime) instead of rejecting; update the package and lane comments so the rejection copy matches reality (only oversize and count reject now). Verify: `go test ./internal/attachments/...`.
- [x] 1.2 `internal/attachments/attachments_test.go`: replace the unsupported-type rejection cases with file-lane cases (unknown mime within cap saved; unknown mime over 20 MB rejected with the size reason; empty mime takes the file lane). Verify: `go test ./internal/attachments/...`.
- [x] 1.3 `internal/agents/turninput.go`: add the `default`-mime case before the fall-through — file-lane mimes return `pointerBlock` with a new `reasonFileBytesWithheld` ("its bytes are not sent; inspect it with the filesystem tools"); extend `laneName` with a "file" branch. Verify: `go test ./internal/agents/...`.
- [x] 1.4 `internal/agents/attachments_turn_test.go`: a file-lane attachment (e.g. mime `application/zip`) produces the withheld-bytes pointer note addressing `/workspace/uploads/<name>`; an empty-mime attachment does the same. Verify: `go test ./internal/agents/...`.

## 2. Frontend — accept-all picker and lane mirror

- [x] 2.1 `frontend/src/features/chat/runtime/useChatRuntime.ts`: remove `ATTACHMENT_ACCEPT` and the `accept` property so the picker is unfiltered; update the comment that mirrors the lanes. Verify: `cd frontend && npx vp check src` and the attach button still opens the OS dialog with all files.
- [x] 2.2 `frontend/src/features/chat/attachments.ts`: mirror the office lane (the three OOXML mimes → `MAX_OFFICE_BYTES = 20 << 20`) and the file lane (`MAX_FILE_BYTES = 20 << 20`, default branch including empty mime); the unsupported-type rejection disappears; `laneLabel` gains office and file branches; header comment updated. Verify: `cd frontend && npx vp check src`.
- [x] 2.3 `frontend/src/features/chat/__test__/attachments.test.tsx`: add lane coverage — a `.zip`/unknown-mime file within 20 MB uploads, one over 20 MB rejects with the size reason, an empty-mime file uploads, an office document within cap uploads. Verify: `cd frontend && npx vitest run src/features/chat/__test__/attachments.test.tsx`.

## 3. Verification

- [x] 3.1 Full gates: `gofmt -w .`, `go vet ./...`, `go test ./...` in the repo root; `cd frontend && npx vp check src`. Verify: all green.
- [ ] 3.2 Live smoke (GUI, `task dev`): attach a `.docx` and a `.zip` to a chat turn — both become ready chips, send succeeds, the transcript shows the withheld-bytes reference note, and the agent reads the file at `/workspace/uploads/<name>` via `read_file`. Verify: screenshots/notes in `.team/smoke/add-all-file-attachments/`.
