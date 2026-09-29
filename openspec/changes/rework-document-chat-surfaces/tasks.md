# Tasks

## 1. Standalone preview modal (panel-less surfaces)

- [x] 1.1 `DocumentsPane`: add a `previewing` document state and render `Modal` hosting `DocumentSource` (tab shape `{kind:'document', title:name, payload:{name,url}}`); the eye button sets it instead of `openPanelTab`. Verify: component test — clicking Preview renders the document source in the modal without navigation.
- [x] 1.2 `AgentConfigModal`: its documents-section preview links use the same modal pattern. Verify: component test — preview link renders the modal.

## 2. Composer → panel Documents move

- [x] 2.1 `Composer`: delete popover state/JSX; the toolbar button opens `openPanelTab({kind:'documents', title:'Documents', payload:{}})` when a panel host exists (chat route). Verify: component test — button click calls openPanelTab with the documents kind.
- [x] 2.2 `ChatRoute`: build the mention bridge — `insertDocumentMention(doc)` registered by the composer through `ChatView`, passed into `RightPanel` ctx. Verify: component test — bridge invokes the composer callback and adds the chip + text token.
- [x] 2.3 `DocumentsListSource`: add per-row Insert action (stopPropagation, calls `ctx.insertDocumentMention` when present, hidden otherwise). Verify: component test — row Insert inserts the mention token; row click still opens the preview tab.

## 3. Cleanup & gates

- [x] 3.1 Remove dead popover code paths and stale tests (popover lens testids move to the panel listing tests). Verify: `grep` shows no `docsOpen` remnants; web test suite green.
- [x] 3.2 Full gates: `go build ./... && go vet ./...` untouched-backend sanity, web lint + test suite. Verify: all green.

## 4. Live pass (user-gated)

- [x] 4.1 Browser: settings eye button opens the preview modal; agent-config preview link works from /agents; composer 📄 opens the panel Documents tab; Insert from a panel row adds the mention pill; sending the turn carries the pointer note. Verify: manual checklist recorded.
  - 2026-09-28 21:0x WIB live pass (dev, vite 5173 + fresh backend on the wave tree): settings Documents eye → standalone dialog hosting DocumentSource (degrade card + capability-URL download, docx); /agents → Configure → Capabilities → Preview → same dialog nested in the agent modal; composer Reference documents → right panel Documents tab selected with the FDS row; row Insert → composer input `[📄 FDS_Sokratech_Spesifikasi_Input_Output.docx](references/FDS_Sokratech_Spesifikasi_Input_Output.docx)` + chip, panel stayed open; sent turn carries the pointer note in the transcript.
- [x] 4.2 Amendment (2026-09-28 user pivot): documents affordance moves from the composer toolbar to the chat header beside the panel toggle — header toggle opens/closes the panel Documents listing (pressed = visible), composer keeps only the mention bridge. Verify: ChatHeader/ChatRoute tests cover the toggle; live pass confirms open → close via panel X → re-open via header.
  - Implemented in ChatHeader.tsx (`btn-panel-documents`), ChatView.tsx (documentsOpen/toggleDocuments from the panel slice), Composer.tsx (button removed); tests updated (ChatHeader.test.tsx new describe, Composer.test.tsx affordance describe removed, ChatRoute.test.tsx 2.1 e2e uses the header toggle); web suites green. Live verification below.
  - 2026-09-28 21:4x WIB live pass (dev, HMR): header button renders beside the panel toggle (aria-pressed false, "Show documents"); click → panel opens with Documents as the active tab, FDS row + Insert action listed, pressed true; panel's "Close Documents tab" X → panel closes, pressed false; header click again → Documents listing back with Insert action (the reported stranded-state scenario is fixed).
