package constructor

import (
	"net"

	"github.com/coroot/coroot/model"
)

// Shards fork: attaching the Kafka, ClickHouse and Elasticsearch/OpenSearch metrics of the shards cluster agent
// to instances.
//
// The metrics are labeled with the target address (ip:port, the agent resolves the configured hosts). Like the
// upstream database targets, they are attached to the instance listening on that address (a container seen by
// the node agent, a pod, or an external service the monitored apps connect to). A target that matches no instance,
// e.g. a Kafka cluster on VMs without the node agent configured statically in the cluster agent, gets an instance
// of its own in an external service application named after the address, so that it is still monitored.

type clusterTargetLoader struct {
	queries []Query
	types   []model.ApplicationType
	load    func(instance *model.Instance, queryName string, m *model.MetricValues)
}

var clusterTargetLoaders = []clusterTargetLoader{
	{queries: kafkaQueries, types: []model.ApplicationType{model.ApplicationTypeKafka}, load: kafka},
	{queries: clickhouseQueries, types: []model.ApplicationType{model.ApplicationTypeClickHouse}, load: clickhouse},
	{queries: elasticsearchQueries, types: []model.ApplicationType{model.ApplicationTypeElasticsearch, model.ApplicationTypeOpensearch}, load: elasticsearch},
}

func loadClusterTargets(w *model.World, metrics map[string][]*model.MetricValues, instancesByPod map[podId]*model.Instance, instancesByListenAddr map[string]*model.Instance, cloudInstancesById map[string]*model.Instance) {
	created := map[string]*model.Instance{}
	for _, l := range clusterTargetLoaders {
		for _, q := range l.queries {
			for _, m := range metrics[q.Name] {
				instance := findInstance(instancesByPod, instancesByListenAddr, cloudInstancesById, m.Labels, l.types...)
				if instance == nil {
					instance = clusterTargetInstance(w, created, m.Labels["address"])
				}
				if instance == nil {
					continue
				}
				l.load(instance, q.Name, m)
			}
		}
	}
}

// clusterTargetInstance returns the instance of a target that matches no known instance.
func clusterTargetInstance(w *model.World, created map[string]*model.Instance, address string) *model.Instance {
	if address == "" {
		return nil
	}
	if i := created[address]; i != nil {
		return i
	}
	id := model.NewApplicationId(model.ClusterIdExternal, "external", model.ApplicationKindExternalService, address)
	instance := w.GetOrCreateApplication(id, false).GetOrCreateInstance(address, nil)
	if host, port, err := net.SplitHostPort(address); err == nil {
		instance.TcpListens[model.Listen{IP: host, Port: port}] = true
	}
	created[address] = instance
	return instance
}
