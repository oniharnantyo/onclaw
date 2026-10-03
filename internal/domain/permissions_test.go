package domain_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestCanEdit(t *testing.T) {
	tests := []struct {
		name        string
		actorPerms  []string
		targetPerms []string
		expected    bool
	}{
		{
			name:        "Owner cannot edit Admin (peers — equal sets once roles.write retired)",
			actorPerms:  domain.OwnerPermissions,
			targetPerms: domain.AdminPermissions,
			expected:    false,
		},
		{
			name:        "Owner can edit Member (strict subset)",
			actorPerms:  domain.OwnerPermissions,
			targetPerms: domain.MemberPermissions,
			expected:    true,
		},
		{
			name:        "Owner cannot edit Owner (peers / equal sets)",
			actorPerms:  domain.OwnerPermissions,
			targetPerms: domain.OwnerPermissions,
			expected:    false,
		},
		{
			name:        "Admin can edit Member (strict subset)",
			actorPerms:  domain.AdminPermissions,
			targetPerms: domain.MemberPermissions,
			expected:    true,
		},
		{
			name:        "Admin cannot edit Admin (peers / equal sets)",
			actorPerms:  domain.AdminPermissions,
			targetPerms: domain.AdminPermissions,
			expected:    false,
		},
		{
			name:        "Admin cannot edit Owner (peers — equal sets once roles.write retired)",
			actorPerms:  domain.AdminPermissions,
			targetPerms: domain.OwnerPermissions,
			expected:    false,
		},
		{
			name:        "Member cannot edit Member (peers / equal sets)",
			actorPerms:  domain.MemberPermissions,
			targetPerms: domain.MemberPermissions,
			expected:    false,
		},
		{
			name:        "Member cannot edit Admin (target has perms actor lacks)",
			actorPerms:  domain.MemberPermissions,
			targetPerms: domain.AdminPermissions,
			expected:    false,
		},
		{
			name:        "Member cannot edit Owner (target has perms actor lacks)",
			actorPerms:  domain.MemberPermissions,
			targetPerms: domain.OwnerPermissions,
			expected:    false,
		},
		{
			name:        "Superadmin can edit Owner (strict subset)",
			actorPerms:  domain.SuperadminPermissions,
			targetPerms: domain.OwnerPermissions,
			expected:    true,
		},
		{
			name:        "Superadmin cannot edit Superadmin (peers)",
			actorPerms:  domain.SuperadminPermissions,
			targetPerms: domain.SuperadminPermissions,
			expected:    false,
		},
		{
			name:        "Disjoint sets cannot edit",
			actorPerms:  []string{domain.WorkspaceRead, domain.WorkspaceWrite},
			targetPerms: []string{domain.MembersRead, domain.MembersWrite},
			expected:    false,
		},
		{
			name:        "Overlapping non-subset sets cannot edit",
			actorPerms:  []string{domain.WorkspaceRead, domain.WorkspaceWrite, domain.MembersRead},
			targetPerms: []string{domain.WorkspaceRead, domain.RolesRead},
			expected:    false,
		},
		{
			name:        "Empty target perms is editable by non-empty actor",
			actorPerms:  domain.MemberPermissions,
			targetPerms: []string{},
			expected:    true,
		},
		{
			name:        "Empty actor perms cannot edit empty target perms",
			actorPerms:  []string{},
			targetPerms: []string{},
			expected:    false,
		},
		{
			name:        "Duplicates in actor and target are normalized",
			actorPerms:  []string{domain.WorkspaceRead, domain.WorkspaceWrite, domain.WorkspaceRead},
			targetPerms: []string{domain.WorkspaceRead},
			expected:    true,
		},
		{
			name:        "Duplicates resulting in same set cannot edit",
			actorPerms:  []string{domain.WorkspaceRead, domain.WorkspaceRead},
			targetPerms: []string{domain.WorkspaceRead},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.CanEdit(tt.actorPerms, tt.targetPerms)
			if got != tt.expected {
				t.Errorf("CanEdit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCanAssign(t *testing.T) {
	tests := []struct {
		name       string
		actorPerms []string
		rolePerms  []string
		expected   bool
	}{
		{
			name:       "Owner can assign Owner (subset)",
			actorPerms: domain.OwnerPermissions,
			rolePerms:  domain.OwnerPermissions,
			expected:   true,
		},
		{
			name:       "Owner can assign Admin (subset)",
			actorPerms: domain.OwnerPermissions,
			rolePerms:  domain.AdminPermissions,
			expected:   true,
		},
		{
			name:       "Owner can assign Member (subset)",
			actorPerms: domain.OwnerPermissions,
			rolePerms:  domain.MemberPermissions,
			expected:   true,
		},
		{
			name:       "Admin can assign Admin (subset of itself)",
			actorPerms: domain.AdminPermissions,
			rolePerms:  domain.AdminPermissions,
			expected:   true,
		},
		{
			name:       "Admin can assign Member (subset)",
			actorPerms: domain.AdminPermissions,
			rolePerms:  domain.MemberPermissions,
			expected:   true,
		},
		{
			name:       "Admin can assign Owner (equal sets are assignable — roles.write retired)",
			actorPerms: domain.AdminPermissions,
			rolePerms:  domain.OwnerPermissions,
			expected:   true,
		},
		{
			name:       "Member can assign Member (subset of itself)",
			actorPerms: domain.MemberPermissions,
			rolePerms:  domain.MemberPermissions,
			expected:   true,
		},
		{
			name:       "Member cannot assign Admin (role has perms member lacks)",
			actorPerms: domain.MemberPermissions,
			rolePerms:  domain.AdminPermissions,
			expected:   false,
		},
		{
			name:       "Member cannot assign Owner (role has perms member lacks)",
			actorPerms: domain.MemberPermissions,
			rolePerms:  domain.OwnerPermissions,
			expected:   false,
		},
		{
			name:       "Superadmin can assign Superadmin role",
			actorPerms: domain.SuperadminPermissions,
			rolePerms:  domain.SuperadminPermissions,
			expected:   true,
		},
		{
			name:       "Superadmin can assign Owner role",
			actorPerms: domain.SuperadminPermissions,
			rolePerms:  domain.OwnerPermissions,
			expected:   true,
		},
		{
			name:       "Empty role permissions is always assignable",
			actorPerms: []string{},
			rolePerms:  []string{},
			expected:   true,
		},
		{
			name:       "Non-empty role is not assignable by actor with empty perms",
			actorPerms: []string{},
			rolePerms:  domain.MemberPermissions,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.CanAssign(tt.actorPerms, tt.rolePerms)
			if got != tt.expected {
				t.Errorf("CanAssign() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLastOwnerGuard(t *testing.T) {
	tests := []struct {
		name            string
		isTargetOwner   bool
		totalOwnerCount int
		expectedLast    bool
		expectedErr     error
	}{
		{
			name:            "Target is only owner in workspace",
			isTargetOwner:   true,
			totalOwnerCount: 1,
			expectedLast:    true,
			expectedErr:     domain.ErrLastOwnerProtected,
		},
		{
			name:            "Target is owner with other owners present",
			isTargetOwner:   true,
			totalOwnerCount: 2,
			expectedLast:    false,
			expectedErr:     nil,
		},
		{
			name:            "Target is owner with 0 total count (edge/corrupted state)",
			isTargetOwner:   true,
			totalOwnerCount: 0,
			expectedLast:    true,
			expectedErr:     domain.ErrLastOwnerProtected,
		},
		{
			name:            "Target is not owner with 1 total owner",
			isTargetOwner:   false,
			totalOwnerCount: 1,
			expectedLast:    false,
			expectedErr:     nil,
		},
		{
			name:            "Target is not owner with multiple owners",
			isTargetOwner:   false,
			totalOwnerCount: 3,
			expectedLast:    false,
			expectedErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsLastOwner(tt.isTargetOwner, tt.totalOwnerCount)
			if got != tt.expectedLast {
				t.Errorf("IsLastOwner() = %v, want %v", got, tt.expectedLast)
			}

			err := domain.ValidateLastOwner(tt.isTargetOwner, tt.totalOwnerCount)
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("ValidateLastOwner() error = %v, want %v", err, tt.expectedErr)
			}
		})
	}
}

func TestHasPermission(t *testing.T) {
	perms := []string{domain.WorkspaceRead, domain.MembersRead}

	if !domain.HasPermission(perms, domain.WorkspaceRead) {
		t.Errorf("expected HasPermission to return true for WorkspaceRead")
	}
	if domain.HasPermission(perms, domain.WorkspaceWrite) {
		t.Errorf("expected HasPermission to return false for WorkspaceWrite")
	}
}

func TestIsValidPermission(t *testing.T) {
	catalog := domain.AllPermissions()
	for _, p := range catalog {
		if !domain.IsValidPermission(p) {
			t.Errorf("expected %q to be a valid permission", p)
		}
	}

	invalid := []string{"", "workspace.delete", "admin.*", "members.view", "unknown", "providers.delete", "providers.*", "agents.delete", "skills.delete"}
	for _, p := range invalid {
		if domain.IsValidPermission(p) {
			t.Errorf("expected %q to be invalid permission", p)
		}
	}
}

func TestProviderPermissions(t *testing.T) {
	// Owner has both providers.read and providers.write
	if !domain.HasPermission(domain.OwnerPermissions, domain.ProvidersRead) {
		t.Errorf("expected OwnerPermissions to have %s", domain.ProvidersRead)
	}
	if !domain.HasPermission(domain.OwnerPermissions, domain.ProvidersWrite) {
		t.Errorf("expected OwnerPermissions to have %s", domain.ProvidersWrite)
	}

	// Admin has both providers.read and providers.write
	if !domain.HasPermission(domain.AdminPermissions, domain.ProvidersRead) {
		t.Errorf("expected AdminPermissions to have %s", domain.ProvidersRead)
	}
	if !domain.HasPermission(domain.AdminPermissions, domain.ProvidersWrite) {
		t.Errorf("expected AdminPermissions to have %s", domain.ProvidersWrite)
	}

	// Member has providers.read but NOT providers.write
	if !domain.HasPermission(domain.MemberPermissions, domain.ProvidersRead) {
		t.Errorf("expected MemberPermissions to have %s", domain.ProvidersRead)
	}
	if domain.HasPermission(domain.MemberPermissions, domain.ProvidersWrite) {
		t.Errorf("expected MemberPermissions NOT to have %s", domain.ProvidersWrite)
	}

	// Superadmin inherits both providers.read and providers.write
	if !domain.HasPermission(domain.SuperadminPermissions, domain.ProvidersRead) {
		t.Errorf("expected SuperadminPermissions to have %s", domain.ProvidersRead)
	}
	if !domain.HasPermission(domain.SuperadminPermissions, domain.ProvidersWrite) {
		t.Errorf("expected SuperadminPermissions to have %s", domain.ProvidersWrite)
	}
}

func TestToolsPermissions(t *testing.T) {
	// Owner has tools.write
	if !domain.HasPermission(domain.OwnerPermissions, domain.ToolsWrite) {
		t.Errorf("expected OwnerPermissions to have %s", domain.ToolsWrite)
	}

	// Admin has tools.write
	if !domain.HasPermission(domain.AdminPermissions, domain.ToolsWrite) {
		t.Errorf("expected AdminPermissions to have %s", domain.ToolsWrite)
	}

	// Member does NOT have tools.write (reads are covered by membership)
	if domain.HasPermission(domain.MemberPermissions, domain.ToolsWrite) {
		t.Errorf("expected MemberPermissions NOT to have %s", domain.ToolsWrite)
	}

	// Superadmin inherits tools.write
	if !domain.HasPermission(domain.SuperadminPermissions, domain.ToolsWrite) {
		t.Errorf("expected SuperadminPermissions to have %s", domain.ToolsWrite)
	}

	// tools.write is part of the closed catalog
	if !domain.IsValidPermission(domain.ToolsWrite) {
		t.Errorf("expected %s to be a valid permission", domain.ToolsWrite)
	}
}

func TestGatewaysPermissions(t *testing.T) {
	// Owner has gateways.write
	if !domain.HasPermission(domain.OwnerPermissions, domain.GatewaysWrite) {
		t.Errorf("expected OwnerPermissions to have %s", domain.GatewaysWrite)
	}

	// Admin has gateways.write
	if !domain.HasPermission(domain.AdminPermissions, domain.GatewaysWrite) {
		t.Errorf("expected AdminPermissions to have %s", domain.GatewaysWrite)
	}

	// Member does NOT have gateways.write (pairing is member-level and not
	// permission-gated)
	if domain.HasPermission(domain.MemberPermissions, domain.GatewaysWrite) {
		t.Errorf("expected MemberPermissions NOT to have %s", domain.GatewaysWrite)
	}

	// Superadmin inherits gateways.write
	if !domain.HasPermission(domain.SuperadminPermissions, domain.GatewaysWrite) {
		t.Errorf("expected SuperadminPermissions to have %s", domain.GatewaysWrite)
	}

	// gateways.write is part of the closed catalog
	if !domain.IsValidPermission(domain.GatewaysWrite) {
		t.Errorf("expected %s to be a valid permission", domain.GatewaysWrite)
	}
}

// integrations.write defaults into built-in Owner and Admin (Superadmin via
// its all-workspace-permissions set), is absent from Member, and reaches
// custom roles only by explicit grant (add-workspace-connections design.md
// D10, tasks.md 1.6).
func TestIntegrationsPermissions(t *testing.T) {
	// Owner has integrations.write
	if !domain.HasPermission(domain.OwnerPermissions, domain.IntegrationsWrite) {
		t.Errorf("expected OwnerPermissions to have %s", domain.IntegrationsWrite)
	}

	// Admin has integrations.write
	if !domain.HasPermission(domain.AdminPermissions, domain.IntegrationsWrite) {
		t.Errorf("expected AdminPermissions to have %s", domain.IntegrationsWrite)
	}

	// Member does NOT have integrations.write
	if domain.HasPermission(domain.MemberPermissions, domain.IntegrationsWrite) {
		t.Errorf("expected MemberPermissions NOT to have %s", domain.IntegrationsWrite)
	}

	// Superadmin inherits integrations.write
	if !domain.HasPermission(domain.SuperadminPermissions, domain.IntegrationsWrite) {
		t.Errorf("expected SuperadminPermissions to have %s", domain.IntegrationsWrite)
	}

	// integrations.write is part of the closed catalog
	if !domain.IsValidPermission(domain.IntegrationsWrite) {
		t.Errorf("expected %s to be a valid permission", domain.IntegrationsWrite)
	}
}

func TestAgentAndSkillPermissions(t *testing.T) { // Owner has agents.read/write and skills.read/write
	for _, p := range []string{domain.AgentsRead, domain.AgentsWrite, domain.SkillsRead, domain.SkillsWrite} {
		if !domain.HasPermission(domain.OwnerPermissions, p) {
			t.Errorf("expected OwnerPermissions to have %s", p)
		}
		if !domain.HasPermission(domain.AdminPermissions, p) {
			t.Errorf("expected AdminPermissions to have %s", p)
		}
		if !domain.HasPermission(domain.SuperadminPermissions, p) {
			t.Errorf("expected SuperadminPermissions to have %s", p)
		}
	}

	// Member has reads but NOT writes
	for _, p := range []string{domain.AgentsRead, domain.SkillsRead} {
		if !domain.HasPermission(domain.MemberPermissions, p) {
			t.Errorf("expected MemberPermissions to have %s", p)
		}
	}
	for _, p := range []string{domain.AgentsWrite, domain.SkillsWrite} {
		if domain.HasPermission(domain.MemberPermissions, p) {
			t.Errorf("expected MemberPermissions NOT to have %s", p)
		}
	}
}

func TestHooksPermissions(t *testing.T) {
	// Owner, Admin and Superadmin have both hooks.read and hooks.write
	for _, p := range []string{domain.HooksRead, domain.HooksWrite} {
		if !domain.HasPermission(domain.OwnerPermissions, p) {
			t.Errorf("expected OwnerPermissions to have %s", p)
		}
		if !domain.HasPermission(domain.AdminPermissions, p) {
			t.Errorf("expected AdminPermissions to have %s", p)
		}
		if !domain.HasPermission(domain.SuperadminPermissions, p) {
			t.Errorf("expected SuperadminPermissions to have %s", p)
		}
	}

	// Member has neither (reads are not granted either)
	for _, p := range []string{domain.HooksRead, domain.HooksWrite} {
		if domain.HasPermission(domain.MemberPermissions, p) {
			t.Errorf("expected MemberPermissions NOT to have %s", p)
		}
	}

	// Both are part of the closed catalog
	for _, p := range []string{domain.HooksRead, domain.HooksWrite} {
		if !domain.IsValidPermission(p) {
			t.Errorf("expected %s to be a valid permission", p)
		}
	}
}

// TestChannelsPermissions pins the decided member channel grant
// (fix-role-permission-audit proposal, design D5): channels.read and
// channels.write join every built-in role including Member, so members can
// list channels, post messages, and manage channel membership.
func TestChannelsPermissions(t *testing.T) {
	for _, role := range []struct {
		name  string
		perms []string
	}{
		{"Owner", domain.OwnerPermissions},
		{"Admin", domain.AdminPermissions},
		{"Member", domain.MemberPermissions},
		{"Superadmin", domain.SuperadminPermissions},
	} {
		for _, p := range []string{domain.ChannelsRead, domain.ChannelsWrite} {
			if !domain.HasPermission(role.perms, p) {
				t.Errorf("expected %sPermissions to have %s", role.name, p)
			}
		}
	}

	// Both are part of the closed catalog
	for _, p := range []string{domain.ChannelsRead, domain.ChannelsWrite} {
		if !domain.IsValidPermission(p) {
			t.Errorf("expected %s to be a valid permission", p)
		}
	}
}

// TestRolesWriteRetired pins the catalog surgery (fix-role-permission-audit
// design D5): roles.write left the closed catalog and every built-in set.
func TestRolesWriteRetired(t *testing.T) {
	const retired = "roles.write"

	if domain.IsValidPermission(retired) {
		t.Error("expected roles.write to be invalid after retirement")
	}
	for _, p := range domain.AllPermissions() {
		if p == retired {
			t.Error("expected AllPermissions to not contain roles.write")
		}
	}
	for _, role := range []struct {
		name  string
		perms []string
	}{
		{"Owner", domain.OwnerPermissions},
		{"Admin", domain.AdminPermissions},
		{"Member", domain.MemberPermissions},
		{"Superadmin", domain.SuperadminPermissions},
	} {
		for _, p := range role.perms {
			if p == retired {
				t.Errorf("expected %sPermissions to not contain roles.write", role.name)
			}
		}
	}
}

// TestBuiltInRoleSetShapes documents the decided built-in set sizes
// (fix-role-permission-audit D5): Owner and Admin collapse to identical
// 22-permission sets once roles.write is retired — owner authority rides the
// is_owner flag, not an extra permission — and Member carries the 9-permission
// read family plus channel participation.
func TestBuiltInRoleSetShapes(t *testing.T) {
	if len(domain.OwnerPermissions) != 22 {
		t.Errorf("OwnerPermissions has %d permissions, want 22", len(domain.OwnerPermissions))
	}
	if len(domain.MemberPermissions) != 9 {
		t.Errorf("MemberPermissions has %d permissions, want 9", len(domain.MemberPermissions))
	}
	if len(domain.SuperadminPermissions) != 28 {
		t.Errorf("SuperadminPermissions has %d permissions, want 28", len(domain.SuperadminPermissions))
	}

	// Owner == Admin content-wise (peers under the strict-subset algebra).
	ownerSet := make(map[string]bool, len(domain.OwnerPermissions))
	for _, p := range domain.OwnerPermissions {
		ownerSet[p] = true
	}
	if len(ownerSet) != len(domain.AdminPermissions) {
		t.Fatalf("Owner set has %d distinct permissions, Admin set has %d", len(ownerSet), len(domain.AdminPermissions))
	}
	for _, p := range domain.AdminPermissions {
		if !ownerSet[p] {
			t.Errorf("Admin permission %s missing from Owner set — sets must be identical", p)
		}
	}

	// Member stays a strict subset of Admin (Admin can still edit Member).
	memberSet := make(map[string]bool, len(domain.MemberPermissions))
	for _, p := range domain.MemberPermissions {
		memberSet[p] = true
		if !ownerSet[p] {
			t.Errorf("Member permission %s missing from Admin set — Member must stay a subset", p)
		}
	}
	if len(memberSet) >= len(ownerSet) {
		t.Error("Member set must remain a strict subset of Admin")
	}
}
