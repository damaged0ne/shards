package db

import (
	"testing"

	"github.com/coroot/coroot/model"
	"github.com/stretchr/testify/assert"
)

func TestServiceMapSettings(t *testing.T) {
	p := &Project{}
	assert.Equal(t, ServiceMapCategoryCollapsed, p.ServiceMapCategoryMode(model.ApplicationCategoryMonitoring))
	assert.Equal(t, ServiceMapCategoryHidden, p.ServiceMapCategoryMode(model.ApplicationCategorySystem))
	assert.Equal(t, ServiceMapCategoryMuted, p.ServiceMapCategoryMode("legacy"))
	assert.Equal(t, ServiceMapCategoryExpanded, p.ServiceMapCategoryMode("payments"))
	assert.Equal(t, "minust-parser", p.NodeDisplayName("m1", "minust-parser"))

	p.Settings.ServiceMap = &ServiceMapSettings{
		CategoryModes:    map[model.ApplicationCategory]ServiceMapCategoryMode{model.ApplicationCategoryMonitoring: ServiceMapCategoryHidden},
		NodeDisplayNames: map[string]string{"m1": "parsers", "old-host": "renamed"},
	}
	assert.Equal(t, ServiceMapCategoryHidden, p.ServiceMapCategoryMode(model.ApplicationCategoryMonitoring))
	assert.Equal(t, "parsers", p.NodeDisplayName("m1", "minust-parser"))
	assert.Equal(t, "renamed", p.NodeDisplayName("m2", "old-host"))
	assert.Equal(t, "other", p.NodeDisplayName("m3", "other"))

	assert.NoError(t, p.Settings.ServiceMap.Validate())
	assert.Error(t, (&ServiceMapSettings{CategoryModes: map[model.ApplicationCategory]ServiceMapCategoryMode{"x": "big"}}).Validate())
	assert.Error(t, (&ServiceMapSettings{Groups: []ServiceMapGroupRule{{Name: "a"}}}).Validate())
	assert.Error(t, (&ServiceMapSettings{NodeDisplayNames: map[string]string{"m1": "bad name"}}).Validate())

	id := func(name string) model.ApplicationId {
		return model.NewApplicationId("", "", model.ApplicationKindUnknown, name)
	}
	assert.Equal(t, model.ApplicationCategorySystem, p.CalcApplicationCategory(id("docker")))
	assert.Equal(t, model.ApplicationCategorySystem, p.CalcApplicationCategory(id("chrony")))
	assert.Equal(t, model.ApplicationCategoryApplication, p.CalcApplicationCategory(id("billing")))
	assert.Equal(t, model.ApplicationCategoryMonitoring, p.CalcApplicationCategory(id("shards-clickhouse")))
}
