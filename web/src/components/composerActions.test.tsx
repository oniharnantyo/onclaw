import { renderToStaticMarkup } from 'react-dom/server';
import ChatProvider from './ChatProvider';
import { Composer } from './primitives/Composer';
import { ComposerActions } from './Chat';

// Lightweight, dependency-free toggle test: renderToStaticMarkup runs in Node
// (no jsdom needed) and lets us assert which composer control renders for a
// given isStreaming value — pinning Requirement 1's UI scenarios against
// regression.

const noop = () => {};

function renderToggle(isStreaming: boolean): string {
  return renderToStaticMarkup(
    <ChatProvider showToast={noop}>
      <Composer.Root>
        <ComposerActions isStreaming={isStreaming} stopChat={noop} />
      </Composer.Root>
    </ChatProvider>,
  );
}

export function runComposerActionsTests(): void {
  // Scenario A: a stop control appears while streaming
  const streaming = renderToggle(true);
  if (!streaming.includes('composer-cancel-btn')) {
    throw new Error('ComposerActions: expected composer-cancel-btn while streaming');
  }
  if (streaming.includes('composer-send-btn')) {
    throw new Error('ComposerActions: composer-send-btn must not render while streaming');
  }

  // Scenario B: the send control returns when streaming ends (idle)
  const idle = renderToggle(false);
  if (!idle.includes('composer-send-btn')) {
    throw new Error('ComposerActions: expected composer-send-btn while idle');
  }
  if (idle.includes('composer-cancel-btn')) {
    throw new Error('ComposerActions: composer-cancel-btn must not render while idle');
  }

  console.log('composerActions tests passed');
}
