package constructor

// Shards fork: queries for the metrics exported by the shards node agent.
// All query names start with "shards_" so they don't collide with the upstream node_*/container_* handling.

const (
	qShardsFsSize      = "shards_fs_size_bytes"
	qShardsFsAvail     = "shards_fs_avail_bytes"
	qShardsFsFiles     = "shards_fs_files"
	qShardsFsFilesFree = "shards_fs_files_free"
	qShardsFsReadonly  = "shards_fs_readonly"

	qShardsLoad1  = "shards_load1"
	qShardsLoad5  = "shards_load5"
	qShardsLoad15 = "shards_load15"

	qShardsNodeCpuPressure    = "shards_node_cpu_pressure"
	qShardsNodeMemoryPressure = "shards_node_memory_pressure"
	qShardsNodeIOPressure     = "shards_node_io_pressure"

	qShardsF2bUp     = "shards_f2b_up"
	qShardsF2bBanned = "shards_f2b_banned"
	qShardsF2bBans1h = "shards_f2b_bans_1h"

	qShardsNftCounterBytes   = "shards_nft_counter_bytes"
	qShardsNftCounterPackets = "shards_nft_counter_packets"
	qShardsNftRuleBytes      = "shards_nft_rule_bytes"
	qShardsNftRulePackets    = "shards_nft_rule_packets"

	qShardsAgentEbpfLostSamples     = "shards_agent_ebpf_lost_samples"
	qShardsAgentEbpfDecodeErrors    = "shards_agent_ebpf_decode_errors"
	qShardsAgentL7ParseErrors       = "shards_agent_l7_parse_errors"
	qShardsAgentRecoveredPanics     = "shards_agent_recovered_panics"
	qShardsAgentRemoteWriteFailures = "shards_agent_remote_write_failures"
	qShardsAgentEventsQueueLength   = "shards_agent_events_queue_length"

	qShardsContainerState         = "shards_container_state"
	qShardsContainerHealth        = "shards_container_health"
	qShardsContainerExitCode      = "shards_container_exit_code"
	qShardsContainerOOMKilled     = "shards_container_oom_killed"
	qShardsContainerStartedAge    = "shards_container_started_age"
	qShardsContainerFinishedAge   = "shards_container_finished_age"
	qShardsContainerDockerRestart = "shards_container_docker_restarts"
	qShardsContainerRestartPolicy = "shards_container_restart_policy"
	qShardsContainerImageInfo     = "shards_container_image_info"
	qShardsComposeInfo            = "shards_compose_info"
	qShardsContainerCreatedHi     = "shards_container_created_hi"
	qShardsContainerCreatedLo     = "shards_container_created_lo"
	qShardsReleaseWindow          = "shards_release_window"
)

var shardsQueries = []Query{
	Q(qShardsFsSize, `shards_fs_size_bytes`, "mount", "device", "fs"),
	Q(qShardsFsAvail, `shards_fs_avail_bytes`, "mount", "device", "fs"),
	Q(qShardsFsFiles, `shards_fs_files`, "mount", "device", "fs"),
	Q(qShardsFsFilesFree, `shards_fs_files_free`, "mount", "device", "fs"),
	Q(qShardsFsReadonly, `shards_fs_readonly`, "mount", "device", "fs"),

	Q(qShardsLoad1, `shards_load1`),
	Q(qShardsLoad5, `shards_load5`),
	Q(qShardsLoad15, `shards_load15`),

	// node PSI (the upstream server only uses the container-level PSI)
	Q(qShardsNodeCpuPressure, `rate(node_resources_cpu_pressure_waiting_seconds_total[$RANGE])`, "kind"),
	Q(qShardsNodeMemoryPressure, `rate(node_resources_memory_pressure_waiting_seconds_total[$RANGE])`, "kind"),
	Q(qShardsNodeIOPressure, `rate(node_resources_io_pressure_waiting_seconds_total[$RANGE])`, "kind"),

	Q(qShardsF2bUp, `shards_f2b_up`),
	Q(qShardsF2bBanned, `shards_f2b_banned`, "jail"),
	Q(qShardsF2bBans1h, `shards_f2b_bans_1h`, "jail"),

	Q(qShardsNftCounterBytes, `rate(shards_nft_counter_bytes_total[$RANGE])`, "family", "table", "counter"),
	Q(qShardsNftCounterPackets, `rate(shards_nft_counter_packets_total[$RANGE])`, "family", "table", "counter"),
	Q(qShardsNftRuleBytes, `rate(shards_nft_rule_bytes_total[$RANGE])`, "family", "table", "chain", "comment"),
	Q(qShardsNftRulePackets, `rate(shards_nft_rule_packets_total[$RANGE])`, "family", "table", "chain", "comment"),

	// node agent self-observability
	Q(qShardsAgentEbpfLostSamples, `sum without(buffer) (rate(node_agent_ebpf_lost_samples_total[$RANGE]))`),
	Q(qShardsAgentEbpfDecodeErrors, `sum without(buffer) (rate(node_agent_ebpf_decode_errors_total[$RANGE]))`),
	Q(qShardsAgentL7ParseErrors, `sum without(protocol) (rate(node_agent_l7_parse_errors_total[$RANGE]))`),
	Q(qShardsAgentRecoveredPanics, `sum without(component) (rate(node_agent_recovered_panics_total[$RANGE]))`),
	Q(qShardsAgentRemoteWriteFailures, `rate(node_agent_remote_write_failures_total[$RANGE])`),
	Q(qShardsAgentEventsQueueLength, `node_agent_events_queue_length`),

	// Docker-level container metrics (container_id is a regular label, the series exist for stopped containers too)
	Q(qShardsContainerState, `shards_container_state`, "state"),
	Q(qShardsContainerHealth, `shards_container_health`, "status"),
	Q(qShardsContainerExitCode, `shards_container_exit_code`),
	Q(qShardsContainerOOMKilled, `shards_container_oom_killed`),
	// unix times don't fit into float32: ages are loaded instead (precise enough for displaying)
	Q(qShardsContainerStartedAge, `time() - shards_container_started_seconds`),
	Q(qShardsContainerFinishedAge, `time() - shards_container_finished_seconds`),
	Q(qShardsContainerDockerRestart, `shards_container_docker_restarts`),
	Q(qShardsContainerRestartPolicy, `shards_container_restart_policy`, "policy"),
	Q(qShardsContainerImageInfo, `shards_container_image_info`, "image", "image_id", "version", "revision"),
	Q(qShardsComposeInfo, `shards_compose_info`, "project", "service"),
	// the creation time is used as the start of a release, so it must be exact: it is loaded as two float32-exact parts
	Q(qShardsContainerCreatedHi, `floor(shards_container_created_seconds / 65536)`),
	Q(qShardsContainerCreatedLo, `shards_container_created_seconds % 65536`),
	Q(qShardsReleaseWindow, `shards_release_window`, "version", "image_id"),
}

func init() {
	QUERIES = append(QUERIES, shardsQueries...)
}
