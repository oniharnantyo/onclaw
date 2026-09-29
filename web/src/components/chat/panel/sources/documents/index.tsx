// Documents source registration (add-reference-documents task 10.5): importing
// this module registers the 'documents' renderer — an ordinary registration
// through the same registry API a plugin would use (AGENTS.md); nothing
// downstream edits the panel shell.

import { registerPanelSource } from "../../../../../lib/panel/registry";
import { DocumentsListSource } from "./DocumentsListSource";

registerPanelSource('documents', ({ tab, ctx }) => <DocumentsListSource tab={tab} ctx={ctx}/>);
