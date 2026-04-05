package main

import (
	"bluebell/dao/milvus"
	"bluebell/dao/mysql"
	"bluebell/logger"
	"bluebell/logic"
	"bluebell/pkg/embedder"
	"bluebell/setting"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	configPath := "./conf/config.yaml"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	limit := 0
	if len(os.Args) >= 3 && os.Args[2] != "" {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Printf("invalid limit: %v\n", err)
			os.Exit(1)
		}
		limit = n
	}

	if err := setting.Init(configPath); err != nil {
		fmt.Printf("load config failed, err:%v\n", err)
		os.Exit(1)
	}
	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		fmt.Printf("init logger failed, err:%v\n", err)
		os.Exit(1)
	}
	if err := mysql.Init(setting.Conf.MySQLConfig); err != nil {
		fmt.Printf("init mysql failed, err:%v\n", err)
		os.Exit(1)
	}
	defer mysql.Close()

	if err := embedder.Init(setting.Conf.EmbeddingConfig); err != nil {
		fmt.Printf("init embedding failed, err:%v\n", err)
		os.Exit(1)
	}
	if err := milvus.Init(setting.Conf.MilvusConfig); err != nil {
		fmt.Printf("init milvus failed, err:%v\n", err)
		os.Exit(1)
	}
	defer milvus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Printf("rebuilding milvus collection %q...\n", setting.Conf.MilvusConfig.Collection)
	if err := milvus.RebuildCollection(ctx); err != nil {
		fmt.Printf("rebuild milvus collection failed, err:%v\n", err)
		os.Exit(1)
	}

	fmt.Println("reindexing posts into milvus...")
	n, err := logic.ReindexPostToRAG(ctx, limit)
	if err != nil {
		fmt.Printf("reindex posts failed, err:%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("milvus rebuild complete, indexed posts: %d\n", n)
}
