package main

import (
	"bluebell/internal/setting"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis"
	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql"
)

const (
	scorePerVote    = 432
	scorePerComment = 128
)

type postRow struct {
	PostID int64 `db:"post_id"`
}

type aiScoreRow struct {
	PostID     int64   `db:"post_id"`
	ScoreDelta float64 `db:"score_delta"`
}

type commentCountRow struct {
	PostID int64 `db:"post_id"`
	Count  int64 `db:"cnt"`
}

// main 程序入口，负责执行当前命令行任务。
func main() {
	configPath := "./conf/config.yaml"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	if err := setting.Init(configPath); err != nil {
		fmt.Printf("load config failed, err:%v\n", err)
		os.Exit(1)
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Local",
		setting.Conf.MySQLConfig.User,
		setting.Conf.MySQLConfig.Password,
		setting.Conf.MySQLConfig.Host,
		setting.Conf.MySQLConfig.Port,
		setting.Conf.MySQLConfig.DB,
	)
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		fmt.Printf("connect mysql failed, err:%v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", setting.Conf.RedisConfig.Host, setting.Conf.RedisConfig.Port),
		Password:     setting.Conf.RedisConfig.Password,
		DB:           setting.Conf.RedisConfig.DB,
		PoolSize:     setting.Conf.RedisConfig.PoolSize,
		MinIdleConns: setting.Conf.RedisConfig.MinIdleConns,
	})
	defer rdb.Close()

	if _, err := rdb.Ping().Result(); err != nil {
		fmt.Printf("connect redis failed, err:%v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var posts []postRow
	if err := db.Select(&posts, `select post_id from post`); err != nil {
		fmt.Printf("load posts failed, err:%v\n", err)
		os.Exit(1)
	}

	var aiRows []aiScoreRow
	if err := db.Select(&aiRows, `select post_id, score_delta from post_ai_score`); err != nil {
		fmt.Printf("load ai scores failed, err:%v\n", err)
		os.Exit(1)
	}
	aiScoreMap := make(map[int64]float64, len(aiRows))
	for _, row := range aiRows {
		aiScoreMap[row.PostID] = row.ScoreDelta
	}

	var commentRows []commentCountRow
	if err := db.Select(&commentRows, `select post_id, count(*) as cnt from comment group by post_id`); err != nil {
		fmt.Printf("load comment counts failed, err:%v\n", err)
		os.Exit(1)
	}
	commentMap := make(map[int64]int64, len(commentRows))
	for _, row := range commentRows {
		commentMap[row.PostID] = row.Count
	}

	pipeline := rdb.TxPipeline()
	pipeline.Del("bluebell:post:score")

	for _, post := range posts {
		postIDStr := strconv.FormatInt(post.PostID, 10)

		upVotes, err := rdb.ZCount("bluebell:post:voted:"+postIDStr, "1", "1").Result()
		if err != nil && err != redis.Nil {
			fmt.Printf("load up votes failed for %s, err:%v\n", postIDStr, err)
			os.Exit(1)
		}
		downVotes, err := rdb.ZCount("bluebell:post:voted:"+postIDStr, "-1", "-1").Result()
		if err != nil && err != redis.Nil {
			fmt.Printf("load down votes failed for %s, err:%v\n", postIDStr, err)
			os.Exit(1)
		}

		score := aiScoreMap[post.PostID]
		score += float64(commentMap[post.PostID] * scorePerComment)
		score += float64((upVotes - downVotes) * scorePerVote)

		pipeline.ZAdd("bluebell:post:score", redis.Z{
			Score:  score,
			Member: post.PostID,
		})
	}

	if _, err := pipeline.Exec(); err != nil {
		fmt.Printf("rebuild redis post score failed, err:%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("rebuilt post score for %d posts\n", len(posts))
	_ = ctx
}
