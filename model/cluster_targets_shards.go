package model

// Shards fork: Kafka, ClickHouse and Elasticsearch/OpenSearch targets of the shards cluster agent.

// clusterTargetType returns the type of the cluster agent target attached to the instance, if any.
func (instance *Instance) clusterTargetType() ApplicationType {
	switch {
	case instance.Kafka != nil:
		return ApplicationTypeKafka
	case instance.ClickHouse != nil:
		return ApplicationTypeClickHouse
	case instance.Elasticsearch != nil:
		return instance.Elasticsearch.ApplicationType()
	}
	return ApplicationTypeUnknown
}

// addClusterTargetTypes marks the instance with the type of its cluster agent target, so that an application
// created for a target without a container (e.g. a Kafka cluster on VMs) is recognized as a database/queue.
func (instance *Instance) addClusterTargetTypes(res map[ApplicationType]bool) {
	if t := instance.clusterTargetType(); t != ApplicationTypeUnknown {
		res[t] = true
	}
}

func (app *Application) IsKafka() bool {
	for _, i := range app.Instances {
		if i.Kafka != nil {
			return true
		}
	}
	return false
}

func (app *Application) IsClickHouse() bool {
	for _, i := range app.Instances {
		if i.ClickHouse != nil {
			return true
		}
	}
	return false
}

func (app *Application) IsElasticsearch() bool {
	for _, i := range app.Instances {
		if i.Elasticsearch != nil {
			return true
		}
	}
	return false
}

// clusterTargetDefaultInstrumentation is the default target of the cluster agent for the type
// (the agent accepts these types from the instrumentation settings like the upstream database types).
func clusterTargetDefaultInstrumentation(t ApplicationType) *ApplicationInstrumentation {
	switch t {
	case ApplicationTypeKafka:
		return &ApplicationInstrumentation{Type: t, Port: "9092"}
	case ApplicationTypeClickHouse:
		return &ApplicationInstrumentation{Type: t, Port: "9000"} // the native protocol
	case ApplicationTypeElasticsearch, ApplicationTypeOpensearch:
		return &ApplicationInstrumentation{Type: t, Port: "9200"}
	}
	return nil
}
