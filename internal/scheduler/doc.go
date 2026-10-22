// Package scheduler is the cron brain for scheduled agent runs: it parses
// five-field cron expressions, computes next fire times in local time, and
// describes recurrences in human-readable sentences. Its Service owns the
// validated schedule CRUD over the store, the tick's due state machine
// (fire, miss, or wait per gateway state and lateness), the run-now path,
// and change notification — the single place the schedule editor and the
// agent tool both call, so they can never disagree.
package scheduler
