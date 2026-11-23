package main

import (
	"log"
	"os"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal/mysql"
	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := conf.Load(os.Getenv("GO_ENV"))
	if err != nil {
		return err
	}
	db, err := mysql.Init(cfg.MySQL)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	return mysql.Migrate(db)
}
