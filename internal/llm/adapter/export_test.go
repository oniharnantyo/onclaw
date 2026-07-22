package adapter

// Test bridges: expose unexported wrappers to the black-box adapter_test package
// so the caching wrappers can be exercised without a live provider.

var NewGeminiPrefixCache = newGeminiPrefixCache
var NewClaudePrefixCache = newClaudePrefixCache
