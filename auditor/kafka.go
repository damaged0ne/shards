package auditor

import (
	"fmt"
	"math"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: the Kafka report built from the metrics of the shards cluster agent.

const (
	kafkaRateWindow     = 5 * timeseries.Minute  // the current produce/consume rates are averaged over this period
	kafkaLagTrendWindow = 15 * timeseries.Minute // the lag is considered growing if it increased over this period...
	kafkaLagGrowthShare = 0.8                    // ...in at least this share of the steps (Burrow: a window of offsets)
	kafkaLagMinSteps    = 4
	kafkaStallWindow    = 10 * timeseries.Minute // no committed offsets for this period with lag > 0: the group is stalled
	kafkaChartTopN      = 10
)

// kafkaCluster is the cluster-wide data of a Kafka cluster, reported by one of the targets of the cluster.
type kafkaCluster struct {
	name     string
	instance *model.Instance
	kafka    *model.Kafka
}

// kafkaClusters returns one entry per cluster (by cluster ID): when several brokers of the same cluster are targets,
// every one of them may report the cluster-wide metrics.
func kafkaClusters(instances []*model.Instance) []kafkaCluster {
	byId := map[string]kafkaCluster{}
	for _, i := range instances {
		if i.Kafka == nil || i.IsObsolete() || !i.Kafka.ReportsClusterMetrics() {
			continue
		}
		id := i.Kafka.ClusterId.Value()
		if id == "" {
			id = i.Name
		}
		if c, ok := byId[id]; ok && c.instance.Name <= i.Name {
			continue
		}
		byId[id] = kafkaCluster{name: id, instance: i, kafka: i.Kafka}
	}
	res := make([]kafkaCluster, 0, len(byId))
	for _, c := range byId {
		res = append(res, c)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].name < res[j].name })
	return res
}

type kafkaGroupStatus struct {
	status  model.Status // OK, WARNING or CRITICAL
	reason  string
	lag     float32 // messages
	timeLag float32 // seconds, +Inf if the group doesn't consume
	growing bool
}

// evalKafkaConsumerGroup evaluates the lag of a consumer group the way Burrow does
// (https://github.com/linkedin/Burrow/wiki/Consumer-Lag-Evaluation-Rules): what matters is the trend of the lag
// and whether the consumer commits offsets, not the absolute lag.
//   - STALLED (critical): the lag is > 0 and the committed offsets haven't moved for kafkaStallWindow,
//     or the group is Dead/Empty (no consumers) while the lag is > 0;
//   - WARNING: the lag increased in most of the steps of kafkaLagTrendWindow while the group consumes slower
//     than the producers write, or the estimated time lag (lag / consume rate) > timeLagThreshold seconds.
func evalKafkaConsumerGroup(ctx timeseries.Context, g *model.KafkaConsumerGroup, produceRate *timeseries.TimeSeries, timeLagThreshold float32) kafkaGroupStatus {
	lag := g.Lag()
	consumeRate := g.ConsumeRate()
	res := kafkaGroupStatus{status: model.OK, lag: lastValue(lag), timeLag: timeseries.NaN}
	if timeseries.IsNaN(res.lag) {
		return res
	}
	consumeNow := recentAvg(consumeRate, recentPoints(ctx, kafkaRateWindow))
	switch {
	case res.lag <= 0:
		res.timeLag = 0
	case consumeNow > 0:
		res.timeLag = res.lag / consumeNow
	case !timeseries.IsNaN(consumeNow):
		res.timeLag = float32(math.Inf(1))
	}

	// the trend of the lag
	trendPoints := recentPoints(ctx, kafkaLagTrendWindow)
	var defined []float32
	for _, v := range tail(lag, trendPoints) {
		if !timeseries.IsNaN(v) {
			defined = append(defined, v)
		}
	}
	if steps := len(defined) - 1; steps >= kafkaLagMinSteps {
		increases := 0
		for i := 1; i < len(defined); i++ {
			if defined[i] > defined[i-1] {
				increases++
			}
		}
		consume, produce := recentAvg(consumeRate, trendPoints), recentAvg(produceRate, trendPoints)
		slower := timeseries.IsNaN(produce) || timeseries.IsNaN(consume) || consume < produce
		res.growing = float32(increases) >= kafkaLagGrowthShare*float32(steps) && defined[len(defined)-1] > defined[0] && slower
	}

	// stalled
	stallPoints := recentPoints(ctx, kafkaStallWindow)
	lagTail := tail(lag, stallPoints)
	stalled := len(lagTail) >= stallPoints &&
		sustained(lag, stallPoints, func(v float32) bool { return v > 0 }) &&
		sustained(consumeRate, stallPoints, func(v float32) bool { return v == 0 })
	state := g.State.Value()
	switch {
	case res.lag > 0 && (state == "Dead" || state == "Empty"):
		res.status = model.CRITICAL
		res.reason = fmt.Sprintf("no active consumers (%s) with %s messages of lag", state, utils.FormatFloat(res.lag))
	case stalled:
		res.status = model.CRITICAL
		res.reason = fmt.Sprintf("stalled: no offsets committed for %s with %s messages of lag", kafkaStallWindow, utils.FormatFloat(res.lag))
	case res.growing:
		res.status = model.WARNING
		res.reason = fmt.Sprintf("the lag keeps growing (%s messages): the group consumes slower than the producers write", utils.FormatFloat(res.lag))
	case res.timeLag > timeLagThreshold:
		res.status = model.WARNING
		res.reason = fmt.Sprintf("estimated time lag %s > %s", formatTimeLag(res.timeLag), utils.FormatDuration(timeseries.Duration(timeLagThreshold), 1))
	}
	return res
}

func formatTimeLag(v float32) string {
	switch {
	case timeseries.IsNaN(v):
		return ""
	case timeseries.IsInf(v, 1):
		return "∞"
	case v < 1:
		return "<1s"
	}
	return utils.FormatDuration(timeseries.Duration(v), 1)
}

func (a *appAuditor) kafka() {
	isKafka := a.app.ApplicationTypes()[model.ApplicationTypeKafka]
	if !isKafka && !a.app.IsKafka() {
		return
	}
	report := a.addReport(model.AuditReportKafka)
	report.Instrumentation = model.ApplicationTypeKafka
	if !a.app.IsKafka() {
		report.Status = model.UNKNOWN
		return
	}

	availabilityCheck := report.CreateCheck(model.Checks.KafkaAvailability)
	offlineCheck := report.CreateCheck(model.Checks.KafkaOfflinePartitions)
	underReplicatedCheck := report.CreateCheck(model.Checks.KafkaUnderReplicatedPartitions)
	lagCheck := report.CreateCheck(model.Checks.KafkaConsumerLag)

	targetsTable := report.GetOrCreateTable("Target", "Status", "Cluster ID", "Brokers", "Controller").Group("Cluster", 1)
	topicsTable := report.GetOrCreateTable("Topic", "Partitions", "Under-replicated", "Offline", "Produce rate").Group("Topics", 2)
	groupsTable := report.GetOrCreateTable("Consumer group", "Status", "State", "Members", "Lag", "Lag trend", "Consume rate", "Time lag").Group("Consumer groups", 3)

	brokersChart := report.GetOrCreateChart("Brokers", nil).Group("Cluster", 1)
	partitionsChart := report.GetOrCreateChart("Unhealthy partitions", nil).Group("Topics", 2)
	produceChart := report.GetOrCreateChart("Produce rate by topic, messages/second", nil).Group("Topics", 2).Stacked().Sorted()
	consumeChart := report.GetOrCreateChart("Consume rate by consumer group, messages/second", nil).Group("Consumer groups", 3).Stacked().Sorted()
	lagChart := report.GetOrCreateChart("Consumer lag by group, messages", nil).Group("Consumer groups", 3).Sorted()
	timeLagChart := report.GetOrCreateChart("Estimated time lag by group, seconds", nil).Group("Consumer groups", 3).Sorted()

	availabilityCheck.AddWidget(targetsTable.Widget())
	offlineCheck.AddWidget(topicsTable.Widget())
	offlineCheck.AddWidget(partitionsChart.Widget())
	underReplicatedCheck.AddWidget(topicsTable.Widget())
	underReplicatedCheck.AddWidget(partitionsChart.Widget())
	lagCheck.AddWidget(groupsTable.Widget())
	lagCheck.AddWidget(lagChart.Widget())
	lagCheck.AddWidget(timeLagChart.Widget())

	for _, i := range a.app.Instances {
		k := i.Kafka
		if k == nil || i.IsObsolete() {
			continue
		}
		up := k.IsUp()
		if !up {
			availabilityCheck.AddItem("%s", i.Name)
			if e := k.Error.Value(); e != "" {
				availabilityCheck.AddDetail("%s: %s", i.Name, e)
			}
		}
		controller := model.NewTableCell()
		if c := lastValue(k.ControllerId); !timeseries.IsNaN(c) && c >= 0 {
			controller.SetValue(fmt.Sprintf("broker %.0f", c))
		}
		brokers := floatCell(lastValue(k.Brokers), "")
		if !k.ReportsClusterMetrics() && up {
			brokers.SetStub("reported by another target")
		}
		targetsTable.AddRow(
			model.NewTableCell(i.Name),
			targetStatusCell(up, k.Error.Value(), k.Warning.Value()),
			model.NewTableCell(k.ClusterId.Value()),
			brokers,
			controller,
		)
	}

	clusters := kafkaClusters(a.app.Instances)
	prefix := func(c kafkaCluster, name string) string {
		if len(clusters) > 1 {
			return c.name + "/" + name
		}
		return name
	}
	urPoints := recentPoints(a.w.Ctx, timeseries.Duration(underReplicatedCheck.Threshold))
	underReplicatedTotal := timeseries.NewAggregate(timeseries.NanSum)
	offlineTotal := timeseries.NewAggregate(timeseries.NanSum)
	for _, c := range clusters {
		k := c.kafka
		brokersChart.AddSeries(c.name, k.Brokers)
		produce := map[string]model.SeriesData{}
		for _, name := range k.TopicNames() {
			t := k.Topics[name]
			topic := prefix(c, name)
			underReplicatedTotal.Add(t.UnderReplicated)
			offlineTotal.Add(t.Offline)
			produce[topic] = t.ProduceRate

			offline := lastValue(t.Offline)
			underReplicated := lastValue(t.UnderReplicated)
			offlineCell := floatCell(offline, "")
			underReplicatedCell := floatCell(underReplicated, "")
			if offline > 0 {
				offlineCheck.AddItem("%s", topic)
				offlineCheck.AddDetail("%s: %.0f offline partitions", topic, offline)
				offlineCheck.SetCritical()
				offlineCell.SetStatus(model.CRITICAL, fmt.Sprintf("%.0f", offline))
			}
			if sustained(t.UnderReplicated, urPoints, func(v float32) bool { return v > 0 }) && len(tail(t.UnderReplicated, urPoints)) >= urPoints {
				underReplicatedCheck.AddItem("%s", topic)
				underReplicatedCheck.AddDetail("%s: %.0f under-replicated partitions", topic, underReplicated)
				underReplicatedCell.SetStatus(model.WARNING, fmt.Sprintf("%.0f", underReplicated))
			}
			topicsTable.AddRow(
				model.NewTableCell(topic),
				floatCell(lastValue(t.Partitions), ""),
				underReplicatedCell,
				offlineCell,
				floatCell(recentAvg(t.ProduceRate, recentPoints(a.w.Ctx, kafkaRateWindow)), "msg/s"),
			)
		}
		produceChart.AddMany(produce, kafkaChartTopN, timeseries.Max)

		consume := map[string]model.SeriesData{}
		lags := map[string]model.SeriesData{}
		timeLags := map[string]model.SeriesData{}
		for _, name := range k.ConsumerGroupNames() {
			g := k.ConsumerGroups[name]
			group := prefix(c, name)
			lag := g.Lag()
			consumeRate := g.ConsumeRate()
			consume[group] = consumeRate
			lags[group] = lag
			timeLags[group] = timeseries.Aggregate2(lag, consumeRate, func(l, r float32) float32 {
				if timeseries.IsNaN(l) || timeseries.IsNaN(r) || r <= 0 {
					return timeseries.NaN
				}
				return l / r
			})

			st := evalKafkaConsumerGroup(a.w.Ctx, g, k.ProduceRate(g), lagCheck.Threshold)
			statusCell := model.NewTableCell().SetStatus(model.OK, "ok")
			if st.status > model.OK {
				lagCheck.AddItem("%s", group)
				lagCheck.AddDetail("%s: %s", group, st.reason)
				statusCell.SetStatus(st.status, st.reason)
				if st.status == model.CRITICAL {
					lagCheck.SetCritical()
				}
			}
			lagCell := floatCell(st.lag, "msg")
			if st.growing {
				lagCell.AddTag("growing")
			}
			groupsTable.AddRow(
				model.NewTableCell(group),
				statusCell,
				model.NewTableCell(g.State.Value()),
				floatCell(lastValue(g.Members), ""),
				lagCell,
				model.NewTableCell().SetChart(lag),
				floatCell(recentAvg(consumeRate, recentPoints(a.w.Ctx, kafkaRateWindow)), "msg/s"),
				model.NewTableCell(formatTimeLag(st.timeLag)),
			)
		}
		consumeChart.AddMany(consume, kafkaChartTopN, timeseries.Max)
		lagChart.AddMany(lags, kafkaChartTopN, timeseries.Max)
		timeLagChart.AddMany(timeLags, kafkaChartTopN, timeseries.Max)
	}
	partitionsChart.AddSeries("under-replicated", underReplicatedTotal.Get(), "orange")
	partitionsChart.AddSeries("offline", offlineTotal.Get(), "red")
}
