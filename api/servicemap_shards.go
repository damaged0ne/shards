package api

import (
	"net/http"
	"sort"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// shards fork: service map settings (category display modes, group rules, node display names).

type serviceMapSettingsView struct {
	Settings   db.ServiceMapSettings                                   `json:"settings"`
	Effective  map[model.ApplicationCategory]db.ServiceMapCategoryMode `json:"effective_category_modes"`
	Categories []model.ApplicationCategory                             `json:"categories"`
	Nodes      []serviceMapNode                                        `json:"nodes"`
	Editable   bool                                                    `json:"editable"`
}

type serviceMapNode struct {
	MachineId   string `json:"machine_id"`
	Hostname    string `json:"hostname"`
	DisplayName string `json:"display_name"`
}

func (api *Api) ServiceMapSettings(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	pid := string(project.Id)
	editable := api.IsAllowed(u, rbac.Actions.Project(pid).ApplicationCategories().Edit()) && !project.Settings.Readonly
	if r.Method == http.MethodPut {
		if !editable {
			http.Error(w, "You are not allowed to change the service map settings.", http.StatusForbidden)
			return
		}
		var s db.ServiceMapSettings
		if err := utils.ReadJson(r, &s); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := api.db.SaveServiceMapSettings(project, s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	v := serviceMapSettingsView{
		Settings:  project.GetServiceMapSettings(),
		Effective: map[model.ApplicationCategory]db.ServiceMapCategoryMode{},
		Editable:  editable,
	}
	for c := range project.GetApplicationCategories() {
		v.Categories = append(v.Categories, c)
		v.Effective[c] = project.ServiceMapCategoryMode(c)
	}
	sort.Slice(v.Categories, func(i, j int) bool { return v.Categories[i] < v.Categories[j] })

	if world, _, _, err := api.LoadWorldByRequest(r); err != nil {
		klog.Warningln(err)
	} else if world != nil {
		for _, n := range world.Nodes {
			hostname := n.GetName()
			if n.Shards != nil && n.Shards.Hostname != "" {
				hostname = n.Shards.Hostname
			}
			v.Nodes = append(v.Nodes, serviceMapNode{MachineId: n.Id.MachineID, Hostname: hostname, DisplayName: n.GetName()})
		}
		sort.Slice(v.Nodes, func(i, j int) bool { return v.Nodes[i].Hostname < v.Nodes[j].Hostname })
	}
	utils.WriteJson(w, v)
}
