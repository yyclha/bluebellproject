package main

import (
	"bluebell/controller"
	"bluebell/dao/milvus"
	"bluebell/dao/mysql"
	"bluebell/dao/redis"
	"bluebell/logger"
	"bluebell/pkg/embedder"
	"bluebell/pkg/snowflake"
	"bluebell/router"
	"bluebell/setting"
	"fmt"
	"os"
)

func main() {
	configPath := "./conf/config.yaml"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	if err := setting.Init(configPath); err != nil {
		fmt.Printf("load config failed, err:%v\n", err)
		return
	}
	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		fmt.Printf("init logger failed, err:%v\n", err)
		return
	}
	if err := mysql.Init(setting.Conf.MySQLConfig); err != nil {
		fmt.Printf("init mysql failed, err:%v\n", err)
		return
	}
	if err := mysql.InitCommentTable(); err != nil {
		fmt.Printf("init comment table failed, err:%v\n", err)
		return
	}
	defer mysql.Close()

	if err := redis.Init(setting.Conf.RedisConfig); err != nil {
		fmt.Printf("init redis failed, err:%v\n", err)
		return
	}
	defer redis.Close()

	if err := snowflake.Init(setting.Conf.StartTime, setting.Conf.MachineID); err != nil {
		fmt.Printf("init snowflake failed, err:%v\n", err)
		return
	}
	if err := controller.InitTrans("zh"); err != nil {
		fmt.Printf("init validator trans failed, err:%v\n", err)
		return
	}

	if err := embedder.Init(setting.Conf.EmbeddingConfig); err != nil {
		fmt.Printf("init embedding failed, err:%v\n", err)
		return
	}
	if err := milvus.Init(setting.Conf.MilvusConfig); err != nil {
		fmt.Printf("init milvus failed, rag disabled, err:%v\n", err)
	}
	defer milvus.Close()

	r := router.SetupRouter(setting.Conf.Mode)
	if err := r.Run(fmt.Sprintf(":%d", setting.Conf.Port)); err != nil {
		fmt.Printf("run server failed, err:%v\n", err)
		return
	}
}
