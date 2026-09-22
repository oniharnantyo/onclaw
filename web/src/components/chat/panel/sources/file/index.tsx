// File source registration (add-right-panel tasks 3.1/3.4): importing this
// module registers the 'file' renderer and the document.create / files.write
// candidate matchers — ordinary registrations through the same registry API
// a plugin would use (AGENTS.md); nothing downstream edits the panel shell.

import { registerPanelCandidate, registerPanelSource } from "../../../../../lib/panel/registry";
import { FileSource } from "./FileSource";
import { fileCandidateMatcher } from "./candidates";

registerPanelSource('file', ({ tab, ctx }) => <FileSource tab={tab} ctx={ctx}/>);
registerPanelCandidate(fileCandidateMatcher);
