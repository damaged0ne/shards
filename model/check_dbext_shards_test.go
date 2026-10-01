package model

import (
	"testing"

	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBExtChecksRegistered(t *testing.T) {
	configs := GetCheckConfigs()
	for _, id := range []CheckId{"PostgresDeadlocks", "PgbouncerClientWaiting", "RabbitmqAlarms", "EtcdNoLeader", "CloudCapacity"} {
		require.Contains(t, configs, id)
		assert.NotEmpty(t, configs[id].Category)
	}
	rules := map[AlertingRuleId]AlertingRule{}
	for _, r := range BuiltinAlertingRules() {
		_, dup := rules[r.Id]
		assert.False(t, dup, r.Id)
		rules[r.Id] = r
		if r.Source.Check != nil {
			assert.Contains(t, configs, r.Source.Check.CheckId, r.Id)
		}
	}
	assert.Equal(t, CRITICAL, rules["postgres-checksum-failures"].Severity)
	assert.Equal(t, CRITICAL, rules["rabbitmq-alarms"].Severity)
	assert.Equal(t, CRITICAL, rules["etcd-no-leader"].Severity)
	assert.Equal(t, WARNING, rules["pgbouncer-client-waiting"].Severity)
}

func TestCheckEscalate(t *testing.T) {
	r := NewAuditReport(&Application{}, timeseries.Context{}, CheckConfigs{}, AuditReportPgbouncer, false)
	ch := r.CreateCheck(DBExtChecks.PgbouncerClientWaiting)
	ch.Escalate()
	ch.Calc()
	assert.Equal(t, OK, ch.Status) // not fired

	ch = r.CreateCheck(DBExtChecks.PgbouncerClientWaiting)
	ch.AddItem("app@shop")
	ch.Calc()
	assert.Equal(t, WARNING, ch.Status)
	ch.Escalate()
	ch.Calc()
	assert.Equal(t, CRITICAL, ch.Status)
	assert.Equal(t, "clients are waiting for a server connection in 1 pool", ch.Message)
}

func TestEtcdDbSizePercent(t *testing.T) {
	e := &Etcd{DbSize: timeseries.NewWithData(0, 30, []float32{1 << 30})}
	assert.Equal(t, float32(50), e.DbSizePercent().Last())
	e.Quota = timeseries.NewWithData(0, 30, []float32{4 << 30})
	assert.Equal(t, float32(25), e.DbSizePercent().Last())
}
