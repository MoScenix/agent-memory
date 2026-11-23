package mysql

import (
	"fmt"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
	sqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Init(cfg conf.MySQL) (*gorm.DB, error) {
	if cfg.Address == "" || cfg.Database == "" || cfg.Username == "" {
		return nil, fmt.Errorf("mysql address, database and username are required")
	}

	dsn := sqldriver.NewConfig()
	dsn.Net = "tcp"
	dsn.Addr = cfg.Address
	dsn.DBName = cfg.Database
	dsn.User = cfg.Username
	dsn.Passwd = cfg.Password
	dsn.ParseTime = true
	dsn.Loc = time.UTC
	dsn.Params = map[string]string{"time_zone": "'+00:00'"}

	datetimePrecision := 6
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{
		DSNConfig:                dsn,
		DefaultDatetimePrecision: &datetimePrecision,
	}), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	return db, nil
}
