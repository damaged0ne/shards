package rbac

// Shards fork: synthetic probes. Viewing is covered by the project view permission,
// creating, changing, deleting and test-running probes requires the edit permission (Admin, Editor).
const ScopeProjectProbes Scope = "project.probes"

func (as ProjectActionSet) Probes() ProjectAction {
	return ProjectAction{project: &as, scope: ScopeProjectProbes}
}
