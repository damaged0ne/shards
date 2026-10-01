package db

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/utils"
)

// shards fork: service map settings (category display modes, application groups) and node display names.

type ServiceMapCategoryMode string

const (
	ServiceMapCategoryExpanded  ServiceMapCategoryMode = "expanded"  // every application is a node
	ServiceMapCategoryCollapsed ServiceMapCategoryMode = "collapsed" // the whole category is one node, expandable
	ServiceMapCategoryMuted     ServiceMapCategoryMode = "muted"     // collapsed and visually de-emphasized (legacy)
	ServiceMapCategoryHidden    ServiceMapCategoryMode = "hidden"    // not shown until toggled on
)

func (m ServiceMapCategoryMode) valid() bool {
	switch m {
	case ServiceMapCategoryExpanded, ServiceMapCategoryCollapsed, ServiceMapCategoryMuted, ServiceMapCategoryHidden:
		return true
	}
	return false
}

// DefaultServiceMapCategoryModes are used for categories without an explicit mode.
var DefaultServiceMapCategoryModes = map[model.ApplicationCategory]ServiceMapCategoryMode{
	model.ApplicationCategoryApplication:  ServiceMapCategoryExpanded,
	model.ApplicationCategoryMonitoring:   ServiceMapCategoryCollapsed,
	model.ApplicationCategoryControlPlane: ServiceMapCategoryHidden,
	model.ApplicationCategorySystem:       ServiceMapCategoryHidden,
	"legacy":                              ServiceMapCategoryMuted,
}

type ServiceMapGroupRule struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"` // globs matched against the application name, e.g. minust_*
}

type ServiceMapSettings struct {
	CategoryModes map[model.ApplicationCategory]ServiceMapCategoryMode `json:"category_modes,omitempty"`
	Groups        []ServiceMapGroupRule                                `json:"groups,omitempty"`
	// DisablePrefixGrouping turns off grouping containers by their name prefix (minust_parser -> minust) when no
	// compose project is known.
	DisablePrefixGrouping bool `json:"disable_prefix_grouping,omitempty"`
	// NodeDisplayNames maps a machine_id or a hostname to the name shown everywhere (node list, node page, map,
	// alerts, MCP). The shards node agent's --hostname-override does the same on the agent side.
	NodeDisplayNames map[string]string `json:"node_display_names,omitempty"`
}

var nodeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

func (s *ServiceMapSettings) Validate() error {
	for c, m := range s.CategoryModes {
		if !m.valid() {
			return fmt.Errorf("invalid mode %q for category %q", m, c)
		}
	}
	seen := map[string]bool{}
	for _, g := range s.Groups {
		g.Name = strings.TrimSpace(g.Name)
		if g.Name == "" {
			return fmt.Errorf("group name is required")
		}
		if seen[g.Name] {
			return fmt.Errorf("duplicate group %q", g.Name)
		}
		seen[g.Name] = true
		if len(g.Patterns) == 0 {
			return fmt.Errorf("group %q has no patterns", g.Name)
		}
	}
	for k, v := range s.NodeDisplayNames {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("empty machine id or hostname")
		}
		if !nodeNameRe.MatchString(v) {
			return fmt.Errorf("invalid display name %q: letters, digits, '.', '_' and '-' only", v)
		}
	}
	return nil
}

func (p *Project) GetServiceMapSettings() ServiceMapSettings {
	if p.Settings.ServiceMap == nil {
		return ServiceMapSettings{}
	}
	return *p.Settings.ServiceMap
}

// ServiceMapCategoryMode returns how a category is drawn on the service map.
func (p *Project) ServiceMapCategoryMode(c model.ApplicationCategory) ServiceMapCategoryMode {
	if s := p.Settings.ServiceMap; s != nil {
		if m, ok := s.CategoryModes[c]; ok && m.valid() {
			return m
		}
	}
	if m, ok := DefaultServiceMapCategoryModes[c]; ok {
		return m
	}
	return ServiceMapCategoryExpanded
}

// NodeDisplayName returns the configured display name for a node, or hostname if there's none.
func (p *Project) NodeDisplayName(machineId, hostname string) string {
	s := p.Settings.ServiceMap
	if s == nil || len(s.NodeDisplayNames) == 0 {
		return hostname
	}
	if n := s.NodeDisplayNames[machineId]; n != "" && machineId != "" {
		return n
	}
	if n := s.NodeDisplayNames[hostname]; n != "" && hostname != "" {
		return n
	}
	return hostname
}

func (db *DB) SaveServiceMapSettings(p *Project, s ServiceMapSettings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	p.Settings.ServiceMap = &s
	return db.SaveProjectSettings(p)
}

type ServiceMapGroupSource string

const (
	ServiceMapGroupByRule   ServiceMapGroupSource = "rule"      // a project setting pattern
	ServiceMapGroupCompose  ServiceMapGroupSource = "compose"   // shards_compose_info / swarm id
	ServiceMapGroupNs       ServiceMapGroupSource = "namespace" // Kubernetes/Nomad namespace
	ServiceMapGroupPrefix   ServiceMapGroupSource = "prefix"    // container name prefix
	ServiceMapGroupCategory ServiceMapGroupSource = "category"  // a collapsed category
)

type ServiceMapGroup struct {
	Name   string                `json:"name"`
	Source ServiceMapGroupSource `json:"source"`
}

// namePrefix splits a container name on its first '_' or '-': minust_parser -> minust, shards-coroot -> shards.
func namePrefix(name string) string {
	i := strings.IndexAny(name, "_-")
	if i < 2 || i == len(name)-1 {
		return ""
	}
	return name[:i]
}

// ServiceMapGroups assigns applications to groups: the compose project (by precedence: a group rule from the
// project settings, the compose project reported by the shards agent or the swarm id, the Kubernetes/Nomad
// namespace, then the container name prefix shared by at least two applications).
func (p *Project) ServiceMapGroups(apps []*model.Application) map[model.ApplicationId]ServiceMapGroup {
	s := p.GetServiceMapSettings()
	res := map[model.ApplicationId]ServiceMapGroup{}
	byPrefix := map[string][]model.ApplicationId{}
	for _, app := range apps {
		id := app.Id
		if id.Kind == model.ApplicationKindExternalService {
			continue
		}
		matched := false
		for _, g := range s.Groups {
			if utils.GlobMatch(id.Name, g.Patterns...) {
				res[id] = ServiceMapGroup{Name: g.Name, Source: ServiceMapGroupByRule}
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if cp := app.ComposeProject(); cp != "" {
			res[id] = ServiceMapGroup{Name: cp, Source: ServiceMapGroupCompose}
			continue
		}
		if id.Kind == model.ApplicationKindDockerSwarmService && id.Namespace != "_" {
			res[id] = ServiceMapGroup{Name: id.Namespace, Source: ServiceMapGroupCompose}
			continue
		}
		if id.Namespace != "" && id.Namespace != "_" {
			res[id] = ServiceMapGroup{Name: id.Namespace, Source: ServiceMapGroupNs}
			continue
		}
		if s.DisablePrefixGrouping || app.IsSystemdUnit() || app.Category.System() {
			continue
		}
		if prefix := namePrefix(id.Name); prefix != "" {
			byPrefix[prefix] = append(byPrefix[prefix], id)
		}
	}
	prefixes := make([]string, 0, len(byPrefix))
	for prefix := range byPrefix {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		ids := byPrefix[prefix]
		if len(ids) < 2 {
			continue
		}
		for _, id := range ids {
			res[id] = ServiceMapGroup{Name: prefix, Source: ServiceMapGroupPrefix}
		}
	}
	return res
}
