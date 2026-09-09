// The lazy seam for the script editor (D23): every CodeMirror import lives
// behind this module's dynamic import, so the main chunk never pulls the
// editor in — it loads only when a HookDialog shows the script handler.
import type { Diagnostic } from '@codemirror/lint';

export interface ScriptEditorHandle {
  /** Replaces the whole document (the Format button's write-back path). */
  setDoc(next: string): void;
  destroy(): void;
}

export interface ScriptEditorMount {
  (parent: HTMLElement, opts: {
    value: string;
    onChange: (next: string) => void;
    onSyntaxError: (hasError: boolean) => void;
  }): ScriptEditorHandle;
}

export async function loadScriptEditor(): Promise<ScriptEditorMount> {
  const [{ EditorView, keymap, lineNumbers, drawSelection }, { defaultKeymap, history, historyKeymap }, { javascriptLanguage }, { linter }, { syntaxTree, syntaxHighlighting, defaultHighlightStyle }] =
    await Promise.all([
      import('@codemirror/view'),
      import('@codemirror/commands'),
      import('@codemirror/lang-javascript'),
      import('@codemirror/lint'),
      import('@codemirror/language'),
    ]);

  return (parent, { value, onChange, onSyntaxError }) => {
    // Parse-diagnostics-as-you-type (D23, "mini lsp"): the Lezer parser is
    // error-tolerant — syntax damage shows up as error nodes in the tree with
    // exact from/to, so no parse ever throws.
    const scriptLinter = linter((view) => {
      const diagnostics: Diagnostic[] = [];
      syntaxTree(view.state).iterate({
        enter: (node) => {
          if (!node.type.isError) return;
          const line = view.state.doc.lineAt(node.from);
          diagnostics.push({
            from: node.from,
            to: Math.max(node.to, node.from + 1),
            severity: 'error',
            message: `Syntax error at line ${line.number}, column ${node.from - line.from + 1}`,
          });
        },
      });
      onSyntaxError(diagnostics.length > 0);
      return diagnostics;
    });

    const view = new EditorView({
      parent,
      doc: value,
      extensions: [
        lineNumbers(),
        drawSelection(),
        history(),
        keymap.of([...defaultKeymap, ...historyKeymap]),
        javascriptLanguage,
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        scriptLinter,
        EditorView.lineWrapping,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChange(u.state.doc.toString());
        }),
        // Match the app's field look: light surface, hairline gutter divider,
        // mono type via CSS inheritance from the host element.
        EditorView.theme({
          '&': { maxHeight: '280px' },
          '.cm-gutters': { backgroundColor: 'transparent', borderRight: '1px solid var(--border)', color: 'var(--muted)' },
          '.cm-activeLine': { backgroundColor: 'color-mix(in oklab, var(--fg) 4%, transparent)' },
          '.cm-activeLineGutter': { backgroundColor: 'transparent' },
        }),
      ],
    });

    return {
      setDoc(next) {
        const current = view.state.doc.toString();
        if (current !== next) view.dispatch({ changes: { from: 0, to: current.length, insert: next } });
      },
      destroy() {
        view.destroy();
      },
    };
  };
}
