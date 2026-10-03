package domain

// Closed catalog of permission constants.
const (
	// Workspace permissions
	WorkspaceRead  = "workspace.read"
	WorkspaceWrite = "workspace.write"

	// Members permissions
	MembersRead   = "members.read"
	MembersWrite  = "members.write"
	MembersRemove = "members.remove"

	// Roles permissions (roles.write is retired — fix-role-permission-audit
	// D5: no endpoint ever enforced it, so it left the closed catalog; roles
	// remain immutable built-ins and roles.read covers the Members-pane view)
	RolesRead = "roles.read"

	// Providers permissions
	ProvidersRead  = "providers.read"
	ProvidersWrite = "providers.write"

	// Agents permissions
	AgentsRead  = "agents.read"
	AgentsWrite = "agents.write"

	// Skills permissions
	SkillsRead  = "skills.read"
	SkillsWrite = "skills.write"

	// Tools permissions (workspace tool settings; reads are covered by membership)
	ToolsWrite = "tools.write"

	// Hooks permissions (workspace agent lifecycle hooks)
	HooksRead  = "hooks.read"
	HooksWrite = "hooks.write"

	// Channels permissions (team rooms with shared human/agent feeds)
	ChannelsRead  = "channels.read"
	ChannelsWrite = "channels.write"

	// Scheduler permissions (workspace scheduled agent runs)
	SchedulerRead  = "scheduler.read"
	SchedulerWrite = "scheduler.write"

	// Gateways permissions (workspace platform gateways — the Telegram bot
	// connection, bindings, and admin unpair; pairing is member-level).
	// Reads ride the same write permission: the pane is admin-only surface.
	GatewaysWrite = "gateways.write"

	// Integrations permissions (workspace service connections — the
	// Integrations gallery's connect, probe, and disconnect). Reads ride
	// membership. Same trust tier as gateways.write: credential-bearing admin
	// surface, held by built-in Owner and Admin (Superadmin via its
	// all-workspace-permissions set), never by Member, and by custom roles
	// only on explicit grant.
	IntegrationsWrite = "integrations.write"

	// Reference documents permissions (workspace reference library —
	// add-reference-documents D7). Upload, attach, edit, and delete ride the
	// ordinary membership surfaces; reference_documents.promote guards the
	// admin-only scope flip between attached (agent/channel tiers) and
	// workspace (all agents). Held by built-in Owner and Admin (Superadmin
	// via its all-workspace-permissions set), never by Member, and by custom
	// roles only on explicit grant.
	PermissionReferenceDocumentsPromote = "reference_documents.promote"

	// Admin permissions (master tenant control plane)
	AdminWorkspacesRead   = "admin.workspaces.read"
	AdminWorkspacesWrite  = "admin.workspaces.write"
	AdminUsersRead        = "admin.users.read"
	AdminUsersWrite       = "admin.users.write"
	AdminSuperadminsWrite = "admin.superadmins.write"
	// AdminIntegrationsWrite guards the instance OAuth app registrations
	// (add-connection-oauth): master-tenant rows, one app per provider,
	// sealed with the instance key — the same trust tier as the other
	// admin.* permissions (Superadmin only; no workspace role holds it and
	// no workspace backfill applies).
	AdminIntegrationsWrite = "admin.integrations.write"
)

var (
	// OwnerPermissions contains all standard workspace permissions (22 permissions).
	// Since roles.write was retired (fix-role-permission-audit D5) the set is
	// identical to AdminPermissions — owner authority rides the is_owner flag
	// and the birth transaction, not an extra permission.
	OwnerPermissions = []string{
		WorkspaceRead,
		WorkspaceWrite,
		MembersRead,
		MembersWrite,
		MembersRemove,
		RolesRead,
		ProvidersRead,
		ProvidersWrite,
		AgentsRead,
		AgentsWrite,
		SkillsRead,
		SkillsWrite,
		ToolsWrite,
		HooksRead,
		HooksWrite,
		ChannelsRead,
		ChannelsWrite,
		SchedulerRead,
		SchedulerWrite,
		GatewaysWrite,
		IntegrationsWrite,
		PermissionReferenceDocumentsPromote,
	}

	// AdminPermissions contains all standard workspace permissions (22 permissions) —
	// every remaining catalog permission, identical to OwnerPermissions now that
	// roles.write is retired (fix-role-permission-audit D5).
	AdminPermissions = []string{
		WorkspaceRead,
		WorkspaceWrite,
		MembersRead,
		MembersWrite,
		MembersRemove,
		RolesRead,
		ProvidersRead,
		ProvidersWrite,
		AgentsRead,
		AgentsWrite,
		SkillsRead,
		SkillsWrite,
		ToolsWrite,
		HooksRead,
		HooksWrite,
		ChannelsRead,
		ChannelsWrite,
		SchedulerRead,
		SchedulerWrite,
		GatewaysWrite,
		IntegrationsWrite,
		PermissionReferenceDocumentsPromote,
	}

	// MemberPermissions contains the read family plus channel participation
	// (9 permissions): members can list channels, post messages, and manage
	// channel membership (fix-role-permission-audit D5) while every
	// workspace-administration write stays admin-gated.
	MemberPermissions = []string{
		WorkspaceRead,
		MembersRead,
		RolesRead,
		ProvidersRead,
		AgentsRead,
		SkillsRead,
		SchedulerRead,
		ChannelsRead,
		ChannelsWrite,
	}

	// SuperadminPermissions contains all workspace permissions plus all instance-admin permissions (28 permissions).
	SuperadminPermissions = []string{
		WorkspaceRead,
		WorkspaceWrite,
		MembersRead,
		MembersWrite,
		MembersRemove,
		RolesRead,
		ProvidersRead,
		ProvidersWrite,
		AgentsRead,
		AgentsWrite,
		SkillsRead,
		SkillsWrite,
		ToolsWrite,
		HooksRead,
		HooksWrite,
		ChannelsRead,
		ChannelsWrite,
		SchedulerRead,
		SchedulerWrite,
		GatewaysWrite,
		IntegrationsWrite,
		PermissionReferenceDocumentsPromote,
		AdminWorkspacesRead,
		AdminWorkspacesWrite,
		AdminUsersRead,
		AdminUsersWrite,
		AdminSuperadminsWrite,
		AdminIntegrationsWrite,
	}
)

// AllPermissions returns all permissions defined in the closed catalog.
func AllPermissions() []string {
	return []string{
		WorkspaceRead,
		WorkspaceWrite,
		MembersRead,
		MembersWrite,
		MembersRemove,
		RolesRead,
		ProvidersRead,
		ProvidersWrite,
		AgentsRead,
		AgentsWrite,
		SkillsRead,
		SkillsWrite,
		ToolsWrite,
		HooksRead,
		HooksWrite,
		ChannelsRead,
		ChannelsWrite,
		SchedulerRead,
		SchedulerWrite,
		GatewaysWrite,
		IntegrationsWrite,
		PermissionReferenceDocumentsPromote,
		AdminWorkspacesRead,
		AdminWorkspacesWrite,
		AdminUsersRead,
		AdminUsersWrite,
		AdminSuperadminsWrite,
		AdminIntegrationsWrite,
	}
}

// IsValidPermission checks if a permission string belongs to the catalog.
func IsValidPermission(p string) bool {
	switch p {
	case WorkspaceRead, WorkspaceWrite,
		MembersRead, MembersWrite, MembersRemove,
		RolesRead,
		ProvidersRead, ProvidersWrite,
		AgentsRead, AgentsWrite,
		SkillsRead, SkillsWrite,
		ToolsWrite,
		HooksRead, HooksWrite,
		ChannelsRead, ChannelsWrite,
		SchedulerRead, SchedulerWrite,
		GatewaysWrite,
		IntegrationsWrite,
		PermissionReferenceDocumentsPromote,
		AdminWorkspacesRead, AdminWorkspacesWrite,
		AdminUsersRead, AdminUsersWrite,
		AdminSuperadminsWrite,
		AdminIntegrationsWrite:
		return true
	default:
		return false
	}
}

// HasPermission checks if the given permission set contains the required permission.
func HasPermission(perms []string, required string) bool {
	for _, p := range perms {
		if p == required {
			return true
		}
	}
	return false
}

// CanEdit implements the strict subset permission algebra rule (target ⊊ actor).
// Managing a target member requires that the target's permission set is a strict subset
// of the actor's permission set. Peers (equal sets) are not manageable.
func CanEdit(actorPerms, targetPerms []string) bool {
	actorSet := toSet(actorPerms)
	targetSet := toSet(targetPerms)

	// Target must be a subset of actor: every perm in targetSet must exist in actorSet.
	for p := range targetSet {
		if !actorSet[p] {
			return false
		}
	}

	// Strict subset: actorSet must have at least one permission that targetSet lacks.
	return len(actorSet) > len(targetSet)
}

// CanAssign implements the subset permission algebra rule (role ⊆ actor).
// Assigning a role to a member requires that the role's permission set is a subset
// of the actor's permission set.
func CanAssign(actorPerms, rolePerms []string) bool {
	actorSet := toSet(actorPerms)
	roleSet := toSet(rolePerms)

	// Every permission in roleSet must be present in actorSet.
	for p := range roleSet {
		if !actorSet[p] {
			return false
		}
	}
	return true
}

// IsLastOwner checks if demoting or removing the target would violate the last owner guard.
func IsLastOwner(isTargetOwner bool, totalOwnerCount int) bool {
	return isTargetOwner && totalOwnerCount <= 1
}

// ValidateLastOwner returns ErrLastOwnerProtected if the target is an owner and there are no other owners.
func ValidateLastOwner(isTargetOwner bool, totalOwnerCount int) error {
	if IsLastOwner(isTargetOwner, totalOwnerCount) {
		return ErrLastOwnerProtected
	}
	return nil
}

func toSet(perms []string) map[string]bool {
	set := make(map[string]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return set
}
