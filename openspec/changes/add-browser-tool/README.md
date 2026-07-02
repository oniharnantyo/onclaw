# add-browser-tool

An engine-swappable browser tool for the agent: a Lightpanda-by-default headless engine behind an Engine/Context/Page seam, driven by go-rod over CDP, exposed as 11 granular tools (navigate/snapshot/act/screenshot/open/close/tabs/status/start/stop/console). The engine is replaceable (Lightpanda → Chromium → remote CDP) by swapping the CDP endpoint; enable/disable and configuration flow through the `add-tools-management` surface.
