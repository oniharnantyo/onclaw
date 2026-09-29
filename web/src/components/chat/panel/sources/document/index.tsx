// Document source registration (add-reference-documents task 8.3): importing
// this module registers the 'document' renderer — an ordinary registration
// through the same registry API a plugin would use (AGENTS.md); nothing
// downstream edits the panel shell.

import { registerPanelSource } from "../../../../../lib/panel/registry";
import { DocumentSource } from "./DocumentSource";

registerPanelSource('document', ({ tab }) => <DocumentSource tab={tab}/>);
