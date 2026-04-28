package mysql

import (
	"bluebell/internal/models"
	"bluebell/internal/setting"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := setting.Init(findConfigPath()); err != nil {
		panic(err)
	}
	if err := Init(setting.Conf.MySQLConfig); err != nil {
		panic(err)
	}
	code := m.Run()
	Close()
	os.Exit(code)
}

func TestCreatePost(t *testing.T) {
	postID := time.Now().UnixNano()
	post := models.Post{
		ID:          postID,
		AuthorID:    123,
		CommunityID: 1,
		Title:       "test",
		Content:     "just a test",
	}
	t.Cleanup(func() {
		_ = DeletePostByID(postID)
	})

	err := CreatePost(&post)
	if err != nil {
		t.Fatalf("CreatePost insert record into mysql failed, err:%v\n", err)
	}
	t.Logf("CreatePost insert record into mysql success")
}

func findConfigPath() string {
	if path := os.Getenv("BLUEBELL_CONFIG"); path != "" {
		return path
	}

	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("conf", "config.yaml")
	}
	for {
		path := filepath.Join(wd, "conf", "config.yaml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return filepath.Join("conf", "config.yaml")
		}
		wd = parent
	}
}
