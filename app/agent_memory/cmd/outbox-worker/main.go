package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/dal/milvus"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/utils"
	"github.com/MoScenix/agent-memory/app/agent_memory/biz/worker"
	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := conf.Load(os.Getenv("GO_ENV"))
	if err != nil {
		return err
	}
	embedder, err := utils.NewQwenEmbedder(ctx, cfg.Embedding)
	if err != nil {
		return err
	}
	db, err := dal.Init(cfg.MySQL)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return fmt.Errorf("get MySQL connection pool: %w", err)
	}
	defer pool.Close()

	initCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	index, err := milvus.Init(initCtx, cfg.Milvus, cfg.Embedding.Dimensions, embedder)
	if err != nil {
		return err
	}
	defer index.Close(context.Background())

	consumer, err := worker.NewOutbox(db, cfg.Outbox, index.Sync)
	if err != nil {
		return err
	}
	log.Print("outbox worker started")
	consumer.Run(ctx)
	return nil
}
