// Panel source registration (add-right-panel 1.3): importing this module
// registers the built-in sources — ordinary registrations through the same
// registry API a plugin would use (AGENTS.md). The shell (RightPanel) only
// calls renderPanelSource and never learns about specific sources. The file
// and browser sources land in later slices; their modules exist so the
// imports resolve today.

import "./members";
import "./file";
import "./browser";
import "./document";
import "./documents";
