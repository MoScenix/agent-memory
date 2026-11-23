package dal

import (
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal/mysql"
	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
	"gorm.io/gorm"
)

func Init(cfg conf.MySQL) (*gorm.DB, error) {
	return mysql.Init(cfg)
}
