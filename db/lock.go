package db

import (
	"context"

	"k8s.io/klog"
)

func (db *DB) GetPrimaryLock(ctx context.Context) bool {
	if db.typ != TypePostgres {
		return true
	}
	db.primaryLockMu.Lock() // shards fork: called by the watchers and the probes scheduler
	defer db.primaryLockMu.Unlock()

	if db.primaryLockConn == nil {
		c, err := db.db.Conn(ctx)
		if err != nil {
			klog.Errorln(err)
			return false
		}
		db.primaryLockConn = c
	} else {
		_, err := db.primaryLockConn.ExecContext(ctx, "SELECT pg_advisory_unlock(1)")
		if err != nil {
			klog.Errorln(err)
			_ = db.primaryLockConn.Close()
			db.primaryLockConn = nil
			return false
		}
	}

	var acquired bool
	err := db.primaryLockConn.QueryRowContext(context.TODO(), "SELECT pg_try_advisory_lock(1)").Scan(&acquired)
	if err != nil {
		klog.Errorln(err)
		_ = db.primaryLockConn.Close()
		db.primaryLockConn = nil
		return false
	}
	if !acquired {
		_ = db.primaryLockConn.Close()
		db.primaryLockConn = nil
		return false
	}

	return true
}
