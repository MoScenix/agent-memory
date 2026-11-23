package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal/milvus"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/router"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/service"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"
	"github.com/MoScenix/agent-memory/app/agent_memory/conf"

	"github.com/cloudwego/hertz/pkg/app/server"
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
	db, err := dal.Init(cfg.MySQL)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()

	deps := service.Dependencies{DB: db}
	if cfg.Embedding.APIKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		embedder, err := utils.NewQwenEmbedder(ctx, cfg.Embedding)
		if err != nil {
			cancel()
			return err
		}
		index, err := milvus.Init(ctx, cfg.Milvus, cfg.Embedding.Dimensions, embedder)
		cancel()
		if err != nil {
			return err
		}
		defer index.Close(context.Background())
		deps.Index = index
	}
	h := server.Default(server.WithHostPorts(cfg.Hertz.Address))
	h.Use(deps.Middleware)
	router.GeneratedRegister(h)
	h.Spin()
	return nil
}
