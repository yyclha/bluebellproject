package redis

import (
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/go-redis/redis"
)

const (
	oneWeekInSeconds = 7 * 24 * 3600
	scorePerVote     = 432
	scorePerComment  = 128
)

var (
	ErrVoteTimeExpire = errors.New("vote time expired")
	ErrVoteRepeated   = errors.New("vote repeated")
)

func CreatePost(postID, communityID int64) error {
	pipeline := client.TxPipeline()

	pipeline.ZAdd(getRedisKey(KeyPostTimeZSet), redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: postID,
	})

	// Score ranking is now independent from create time.
	pipeline.ZAdd(getRedisKey(KeyPostScoreZSet), redis.Z{
		Score:  0,
		Member: postID,
	})

	cKey := getRedisKey(KeyCommunitySetPF + strconv.Itoa(int(communityID)))
	pipeline.SAdd(cKey, postID)

	_, err := pipeline.Exec()
	return err
}

func DeletePost(postID, communityID int64) error {
	pid := strconv.FormatInt(postID, 10)
	pipeline := client.TxPipeline()
	pipeline.ZRem(getRedisKey(KeyPostTimeZSet), pid)
	pipeline.ZRem(getRedisKey(KeyPostScoreZSet), pid)
	pipeline.Del(getRedisKey(KeyPostVotedZSetPF + pid))
	pipeline.SRem(getRedisKey(KeyCommunitySetPF+strconv.FormatInt(communityID, 10)), postID)
	_, err := pipeline.Exec()
	return err
}

func AddPostScore(postID int64, delta float64) error {
	if delta == 0 {
		return nil
	}
	return client.ZIncrBy(getRedisKey(KeyPostScoreZSet), delta, strconv.FormatInt(postID, 10)).Err()
}

func AddCommentScore(postID int64) error {
	return AddPostScore(postID, scorePerComment)
}

func VoteForPost(userID, postID string, value float64) error {
	postTime := client.ZScore(getRedisKey(KeyPostTimeZSet), postID).Val()
	if float64(time.Now().Unix())-postTime > oneWeekInSeconds {
		return ErrVoteTimeExpire
	}

	ov := client.ZScore(getRedisKey(KeyPostVotedZSetPF+postID), userID).Val()
	if value == ov {
		value = 0
	}

	var op float64
	if value > ov {
		op = 1
	} else {
		op = -1
	}

	diff := math.Abs(ov - value)
	pipeline := client.TxPipeline()
	pipeline.ZIncrBy(getRedisKey(KeyPostScoreZSet), op*diff*scorePerVote, postID)

	if value == 0 {
		pipeline.ZRem(getRedisKey(KeyPostVotedZSetPF+postID), userID)
	} else {
		pipeline.ZAdd(getRedisKey(KeyPostVotedZSetPF+postID), redis.Z{
			Score:  value,
			Member: userID,
		})
	}

	_, err := pipeline.Exec()
	return err
}
