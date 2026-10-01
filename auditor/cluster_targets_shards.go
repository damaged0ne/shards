package auditor

import (
	"math"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: helpers of the Kafka, ClickHouse and Elasticsearch/OpenSearch reports.

// recentPoints is the number of points covering the last d of the window (at least 1).
func recentPoints(ctx timeseries.Context, d timeseries.Duration) int {
	if ctx.Step <= 0 {
		return 1
	}
	return max(1, int(math.Ceil(float64(d)/float64(ctx.Step))))
}

// tail returns the last n values of ts (fewer if the series is shorter).
func tail(ts *timeseries.TimeSeries, n int) []float32 {
	if ts.IsEmpty() {
		return nil
	}
	var all []float32
	iter := ts.Iter()
	for iter.Next() {
		_, v := iter.Value()
		all = append(all, v)
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// sustained reports whether all the defined values of the last n points satisfy f (and there is at least one).
func sustained(ts *timeseries.TimeSeries, n int, f func(v float32) bool) bool {
	found := false
	for _, v := range tail(ts, n) {
		if timeseries.IsNaN(v) {
			continue
		}
		if !f(v) {
			return false
		}
		found = true
	}
	return found
}

// recentSum is the sum of the per-second rate over the last n points, i.e. the number of events in that period.
func recentSum(ctx timeseries.Context, rate *timeseries.TimeSeries, n int) float32 {
	var sum float32
	for _, v := range tail(rate, n) {
		if !timeseries.IsNaN(v) {
			sum += v * float32(ctx.Step)
		}
	}
	return sum
}

func recentAvg(ts *timeseries.TimeSeries, n int) float32 {
	var sum float32
	count := 0
	for _, v := range tail(ts, n) {
		if !timeseries.IsNaN(v) {
			sum += v
			count++
		}
	}
	if count == 0 {
		return timeseries.NaN
	}
	return sum / float32(count)
}

// lastValue is the last defined value of ts (NaN if there is none).
func lastValue(ts *timeseries.TimeSeries) float32 {
	res := timeseries.NaN
	if ts.IsEmpty() {
		return res
	}
	iter := ts.Iter()
	for iter.Next() {
		if _, v := iter.Value(); !timeseries.IsNaN(v) {
			res = v
		}
	}
	return res
}

func floatCell(v float32, unit string) *model.TableCell {
	c := model.NewTableCell()
	if timeseries.IsNaN(v) {
		return c
	}
	return c.SetValue(utils.FormatFloat(v)).SetUnit(unit)
}

func formatBytes(v float32) string {
	if timeseries.IsNaN(v) {
		return "unknown"
	}
	value, unit := utils.FormatBytes(v)
	return value + unit
}

func bytesCell(v float32) *model.TableCell {
	c := model.NewTableCell()
	if timeseries.IsNaN(v) {
		return c
	}
	value, unit := utils.FormatBytes(v)
	return c.SetValue(value).SetUnit(unit)
}

// targetStatusCell is the status of a cluster agent target in the instance tables.
func targetStatusCell(up bool, err, warning string) *model.TableCell {
	status := model.NewTableCell().SetStatus(model.OK, "up")
	switch {
	case !up && err != "":
		status.SetStatus(model.WARNING, err)
	case !up:
		status.SetStatus(model.WARNING, "down (no metrics)")
	case warning != "":
		status.SetStatus(model.OK, warning)
	}
	return status
}
