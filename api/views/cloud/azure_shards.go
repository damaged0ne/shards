package cloud

import "github.com/coroot/coroot/model"

// Shards fork: managed services discovered by the shards cluster agent.

// Azure lists the Azure Database flexible servers and Azure Cache for Redis instances.
func Azure(w *model.World) *View {
	v := &View{}
	if w == nil {
		return v
	}
	v.Configured = w.Azure.Configured
	v.Detected = detected(w, model.CloudProviderAzure)
	v.Errors = errors(w.Azure.DiscoveryErrors)
	v.Instances = instances(w, func(i *model.Instance) (model.LabelLastValue, model.LabelLastValue, model.LabelLastValue, bool) {
		if i.Cloud != nil && i.Cloud.Provider == model.CloudProviderAzure {
			return i.Cloud.Status, i.Cloud.Engine, i.Cloud.EngineVersion, true
		}
		return model.LabelLastValue{}, model.LabelLastValue{}, model.LabelLastValue{}, false
	})
	return v
}

// awsServices returns the AWS services discovered by the shards cluster agent (ElastiCache Serverless, MemoryDB).
func awsServices(i *model.Instance) (model.LabelLastValue, model.LabelLastValue, model.LabelLastValue, bool) {
	if i.Cloud != nil && i.Cloud.Provider == model.CloudProviderAWS {
		return i.Cloud.Status, i.Cloud.Engine, i.Cloud.EngineVersion, true
	}
	return model.LabelLastValue{}, model.LabelLastValue{}, model.LabelLastValue{}, false
}
