package constructor

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// loadNodesShards loads the node-level metrics of the shards node agent: filesystems, load averages,
// PSI, fail2ban, nftables and the agent's self-observability metrics.
func loadNodesShards(metrics map[string][]*model.MetricValues, nodes nodeCache) {
	get := func(queryName string, f func(s *model.NodeShards, m *model.MetricValues)) {
		for _, m := range metrics[queryName] {
			node := nodes[model.NewNodeIdFromLabels(m)]
			if node == nil {
				continue
			}
			if node.Shards == nil {
				node.Shards = model.NewNodeShards()
			}
			f(node.Shards, m)
		}
	}

	fs := func(s *model.NodeShards, m *model.MetricValues) *model.NodeFilesystem {
		mount := m.Labels["mount"]
		f := s.Filesystems[mount]
		if f == nil {
			f = &model.NodeFilesystem{MountPoint: mount}
			s.Filesystems[mount] = f
		}
		if d := m.Labels["device"]; d != "" {
			f.Device = d
		}
		if t := m.Labels["fs"]; t != "" {
			f.FsType = t
		}
		return f
	}
	get(qShardsFsSize, func(s *model.NodeShards, m *model.MetricValues) {
		f := fs(s, m)
		f.SizeBytes = merge(f.SizeBytes, m.Values, timeseries.Any)
	})
	get(qShardsFsAvail, func(s *model.NodeShards, m *model.MetricValues) {
		f := fs(s, m)
		f.AvailBytes = merge(f.AvailBytes, m.Values, timeseries.Any)
	})
	get(qShardsFsFiles, func(s *model.NodeShards, m *model.MetricValues) {
		f := fs(s, m)
		f.Files = merge(f.Files, m.Values, timeseries.Any)
	})
	get(qShardsFsFilesFree, func(s *model.NodeShards, m *model.MetricValues) {
		f := fs(s, m)
		f.FilesFree = merge(f.FilesFree, m.Values, timeseries.Any)
	})
	get(qShardsFsReadonly, func(s *model.NodeShards, m *model.MetricValues) {
		f := fs(s, m)
		f.Readonly = merge(f.Readonly, m.Values, timeseries.Any)
	})

	get(qShardsLoad1, func(s *model.NodeShards, m *model.MetricValues) {
		s.Load1 = merge(s.Load1, m.Values, timeseries.Any)
	})
	get(qShardsLoad5, func(s *model.NodeShards, m *model.MetricValues) {
		s.Load5 = merge(s.Load5, m.Values, timeseries.Any)
	})
	get(qShardsLoad15, func(s *model.NodeShards, m *model.MetricValues) {
		s.Load15 = merge(s.Load15, m.Values, timeseries.Any)
	})

	for q, resource := range map[string]string{qShardsNodeCpuPressure: "cpu", qShardsNodeMemoryPressure: "memory", qShardsNodeIOPressure: "io"} {
		get(q, func(s *model.NodeShards, m *model.MetricValues) {
			k := resource + "/" + m.Labels["kind"]
			s.Pressure[k] = merge(s.Pressure[k], m.Values, timeseries.Any)
		})
	}

	get(qShardsF2bUp, func(s *model.NodeShards, m *model.MetricValues) {
		s.F2bUp = merge(s.F2bUp, m.Values, timeseries.Any)
	})
	jail := func(s *model.NodeShards, m *model.MetricValues) *model.F2bJail {
		name := m.Labels["jail"]
		j := s.F2bJails[name]
		if j == nil {
			j = &model.F2bJail{}
			s.F2bJails[name] = j
		}
		return j
	}
	get(qShardsF2bBanned, func(s *model.NodeShards, m *model.MetricValues) {
		j := jail(s, m)
		j.Banned = merge(j.Banned, m.Values, timeseries.Any)
	})
	get(qShardsF2bBans1h, func(s *model.NodeShards, m *model.MetricValues) {
		j := jail(s, m)
		j.Bans1h = merge(j.Bans1h, m.Values, timeseries.Any)
	})

	nft := func(stats map[model.NftKey]*model.NftStat, m *model.MetricValues) *model.NftStat {
		k := model.NftKey{Family: m.Labels["family"], Table: m.Labels["table"], Chain: m.Labels["chain"], Name: m.Labels["counter"]}
		if k.Name == "" {
			k.Name = m.Labels["comment"]
		}
		st := stats[k]
		if st == nil {
			st = &model.NftStat{}
			stats[k] = st
		}
		return st
	}
	get(qShardsNftCounterBytes, func(s *model.NodeShards, m *model.MetricValues) {
		st := nft(s.NftCounters, m)
		st.Bytes = merge(st.Bytes, m.Values, timeseries.Any)
	})
	get(qShardsNftCounterPackets, func(s *model.NodeShards, m *model.MetricValues) {
		st := nft(s.NftCounters, m)
		st.Packets = merge(st.Packets, m.Values, timeseries.Any)
	})
	get(qShardsNftRuleBytes, func(s *model.NodeShards, m *model.MetricValues) {
		st := nft(s.NftRules, m)
		st.Bytes = merge(st.Bytes, m.Values, timeseries.Any)
	})
	get(qShardsNftRulePackets, func(s *model.NodeShards, m *model.MetricValues) {
		st := nft(s.NftRules, m)
		st.Packets = merge(st.Packets, m.Values, timeseries.Any)
	})

	get(qShardsAgentEbpfLostSamples, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.EbpfLostSamples = merge(s.Agent.EbpfLostSamples, m.Values, timeseries.NanSum)
	})
	get(qShardsAgentEbpfDecodeErrors, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.EbpfDecodeErrors = merge(s.Agent.EbpfDecodeErrors, m.Values, timeseries.NanSum)
	})
	get(qShardsAgentL7ParseErrors, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.L7ParseErrors = merge(s.Agent.L7ParseErrors, m.Values, timeseries.NanSum)
	})
	get(qShardsAgentRecoveredPanics, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.RecoveredPanics = merge(s.Agent.RecoveredPanics, m.Values, timeseries.NanSum)
	})
	get(qShardsAgentRemoteWriteFailures, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.RemoteWriteFailures = merge(s.Agent.RemoteWriteFailures, m.Values, timeseries.NanSum)
	})
	get(qShardsAgentEventsQueueLength, func(s *model.NodeShards, m *model.MetricValues) {
		s.Agent.EventsQueueLength = merge(s.Agent.EventsQueueLength, m.Values, timeseries.NanSum)
	})
}
