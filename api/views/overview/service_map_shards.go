package overview

import (
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
)

// shards fork: service map groups and category display modes.

type ServiceMapView struct {
	CategoryModes map[model.ApplicationCategory]db.ServiceMapCategoryMode `json:"category_modes"`
}

func annotateServiceMap(apps []*Application, w *model.World, project *db.Project) *ServiceMapView {
	v := &ServiceMapView{CategoryModes: map[model.ApplicationCategory]db.ServiceMapCategoryMode{}}
	all := make([]*model.Application, 0, len(w.Applications))
	for _, a := range w.Applications {
		all = append(all, a)
	}
	groups := project.ServiceMapGroups(all)
	for _, a := range apps {
		if g, ok := groups[a.Id]; ok {
			a.Group = g.Name
			a.GroupSource = string(g.Source)
		}
		if app := w.GetApplication(a.Id); app != nil {
			a.Node = singleNode(app)
		}
		v.CategoryModes[a.Category] = project.ServiceMapCategoryMode(a.Category)
	}
	for c := range project.Settings.ApplicationCategorySettings {
		v.CategoryModes[c] = project.ServiceMapCategoryMode(c)
	}
	for c := range model.BuiltinCategoryPatterns {
		v.CategoryModes[c] = project.ServiceMapCategoryMode(c)
	}
	return v
}

func singleNode(app *model.Application) string {
	node := ""
	for _, i := range app.Instances {
		n := i.NodeName()
		if n == "" || (node != "" && n != node) {
			return ""
		}
		node = n
	}
	return node
}
