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
			name:        "Owner can edit Admin (strict subset)",
			actorPerms:  domain.OwnerPermissions,
			targetPerms: domain.AdminPermissions,
			expected:    true,
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
			name:        "Admin cannot edit Owner (target has perms actor lacks)",
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
			name:       "Admin cannot assign Owner (role has roles.write which admin lacks)",
			actorPerms: domain.AdminPermissions,
			rolePerms:  domain.OwnerPermissions,
			expected:   false,
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
