package domain

import (
	"errors"
	"testing"
)

func TestValidateAgentSessionID(t *testing.T) {
	valid := []string{
		"sess_d2b1f0a2-6a63-4e4e-9f2f-9e4a6d1f7b33",         // web client-minted
		"chan_ab12cd34-1111-2222-3333-444455556666_agentid", // channel
		"sched_schedid_1726142400",                          // scheduler run
		"hb_agentid",                                        // heartbeat tick session
		"tg_dm_593821092_atlas",                             // gateway DM
		"tg_dm_593821092_atlas_3",                           // gateway DM, /new suffix bump
		"tg_group_-1001234567890_atlas",                     // gateway group
		"plain-session-id",                                  // no '_' separator: legacy/plain ids pass
		"sess",                                              // no separator
	}
	for _, id := range valid {
		if err := ValidateAgentSessionID(id); err != nil {
			t.Fatalf("expected %q to validate, got %v", id, err)
		}
	}

	invalid := []string{
		"",                 // empty
		"unknown_prefix_1", // unregistered binding prefix shape
		"tg_voice_1_a",     // gateway family typo / unregistered tg_ prefix
		"tg_",              // truncated gateway prefix
	}
	for _, id := range invalid {
		if err := ValidateAgentSessionID(id); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected %q refused with ErrInvalid, got %v", id, err)
		}
	}
}

func TestIsPrivateIndexSessionID(t *testing.T) {
	listed := []string{
		"sess_d2b1f0a2-6a63-4e4e-9f2f-9e4a6d1f7b33",
		"tg_dm_593821092_atlas",
		"plain-session-id",
	}
	for _, id := range listed {
		if !IsPrivateIndexSessionID(id) {
			t.Fatalf("expected %q listed in the per-user index", id)
		}
	}

	excluded := []string{
		"chan_ab12cd34_agentid",
		"sched_schedid_1726142400",
		"hb_agentid", // heartbeat ticks share one ambient session (add-agent-heartbeat D2)
		"tg_group_-1001234567890_atlas",
	}
	for _, id := range excluded {
		if IsPrivateIndexSessionID(id) {
			t.Fatalf("expected %q excluded from the per-user index", id)
		}
	}
}
