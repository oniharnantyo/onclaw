import { runGroupBlocksTests } from './components/chat/groupBlocks.test.ts';
import { runMergeBlockDeltaTests } from './components/chat/mergeBlockDelta.test.ts';
import { runChatReducerTests } from './components/chatReducer.test.ts';
import { runChatStreamTests } from './components/chat/runChatStream.test.ts';
import { runComposerActionsTests } from './components/composerActions.test';
import { runCompactionTests } from './components/compaction.test.ts';

runGroupBlocksTests();
runMergeBlockDeltaTests();
runChatReducerTests();
runComposerActionsTests();
runCompactionTests();
runChatStreamTests()
  .then(() => {
    console.log('All web unit tests passed');
  })
  .catch((err) => {
    console.error(err);
    throw err;
  });
