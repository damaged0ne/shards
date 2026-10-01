package watchers

import (
	"testing"

	"github.com/coroot/coroot/model"
	"github.com/stretchr/testify/assert"
)

func TestCheckAlertSeverity(t *testing.T) {
	assert.Equal(t, model.WARNING, checkAlertSeverity(model.WARNING, &model.Check{Status: model.WARNING}))
	assert.Equal(t, model.CRITICAL, checkAlertSeverity(model.WARNING, &model.Check{Status: model.CRITICAL}))
	assert.Equal(t, model.CRITICAL, checkAlertSeverity(model.CRITICAL, &model.Check{Status: model.WARNING}))
}
