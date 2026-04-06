package main

import (
	"bluebell/models"
	"bluebell/pkg/postscore"
	"bluebell/setting"
	"context"
	"fmt"
	"os"
	"time"
)

func main() {
	configPath := "./conf/config.yaml"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	if err := setting.Init(configPath); err != nil {
		fmt.Printf("load config failed, err:%v\n", err)
		os.Exit(1)
	}
	if err := postscore.Init(setting.Conf.PostScoreConfig); err != nil {
		fmt.Printf("init post score failed, err:%v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := postscore.ScorePost(ctx, &models.Post{
		Title:   "LOL教程",
		Content: "发一篇关于对线、团战和视野处理的实战经验总结，希望能帮助新手更快理解节奏。",
	})
	if err != nil {
		fmt.Printf("score post failed, err:%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("model=%s score=%.2f response=%q\n", result.Model, result.Score, result.ResponseText)
}
