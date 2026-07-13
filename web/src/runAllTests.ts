import { runGroupBlocksTests } from './components/chat/groupBlocks.test.ts';
import { runMergeBlockDeltaTests } from './components/chat/mergeBlockDelta.test.ts';
import { runChatReducerTests } from './components/chatReducer.test.ts';
import { runChatStreamTests } from './components/chat/runChatStream.test.ts';
import { runComposerActionsTests } from './components/composerActions.test';

runGroupBlocksTests();
runMergeBlockDeltaTests();
runChatReducerTests();
runComposerActionsTests();
runChatStreamTests()
  .then(() => {
    console.log('All web unit tests passed');
  })
  .catch((err) => {
    console.error(err);
    throw err;
  });
