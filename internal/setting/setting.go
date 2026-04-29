package setting

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var Conf = new(AppConfig)

type AppConfig struct {
	Name      string `mapstructure:"name"`
	Mode      string `mapstructure:"mode"`
	Version   string `mapstructure:"version"`
	StartTime string `mapstructure:"start_time"`
	MachineID int64  `mapstructure:"machine_id"`
	Port      int    `mapstructure:"port"`

	*LogConfig       `mapstructure:"log"`
	*MySQLConfig     `mapstructure:"mysql"`
	*RedisConfig     `mapstructure:"redis"`
	*MilvusConfig    `mapstructure:"milvus"`
	*EmbeddingConfig `mapstructure:"embedding"`
	*PostScoreConfig `mapstructure:"post_score"`
	*RAGChatConfig   `mapstructure:"rag_chat"`
	*COSConfig       `mapstructure:"cos"`
}

type MySQLConfig struct {
	Host         string `mapstructure:"host"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DB           string `mapstructure:"dbname"`
	Port         int    `mapstructure:"port"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Password     string `mapstructure:"password"`
	Port         int    `mapstructure:"port"`
	DB           int    `mapstructure:"db"`
	PoolSize     int    `mapstructure:"pool_size"`
	MinIdleConns int    `mapstructure:"min_idle_conns"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxAge     int    `mapstructure:"max_age"`
	MaxBackups int    `mapstructure:"max_backups"`
}

type MilvusConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	Address            string `mapstructure:"address"`
	Collection         string `mapstructure:"collection"`
	Dimension          int    `mapstructure:"dimension"`
	ChunkSize          int    `mapstructure:"chunk_size"`
	ChunkOverlap       int    `mapstructure:"chunk_overlap"`
	TopK               int    `mapstructure:"top_k"`
	MetricType         string `mapstructure:"metric_type"`
	IndexType          string `mapstructure:"index_type"`
	HNSWM              int    `mapstructure:"hnsw_m"`
	HNSWEFConstruction int    `mapstructure:"hnsw_ef_construction"`
	SearchEf           int    `mapstructure:"search_ef"`
}

type EmbeddingConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	Model          string `mapstructure:"model"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type PostScoreConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	BaseURL        string  `mapstructure:"base_url"`
	APIKey         string  `mapstructure:"api_key"`
	Model          string  `mapstructure:"model"`
	TimeoutSeconds int     `mapstructure:"timeout_seconds"`
	ScoreWeight    float64 `mapstructure:"score_weight"`
}

type RAGChatConfig struct {
	Enabled         bool    `mapstructure:"enabled"`
	BaseURL         string  `mapstructure:"base_url"`
	APIKey          string  `mapstructure:"api_key"`
	Model           string  `mapstructure:"model"`
	TimeoutSeconds  int     `mapstructure:"timeout_seconds"`
	TopK            int     `mapstructure:"top_k"`
	MaxContextChars int     `mapstructure:"max_context_chars"`
	Temperature     float64 `mapstructure:"temperature"`
}

type COSConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	BucketURL     string `mapstructure:"bucket_url"`
	SecretID      string `mapstructure:"secret_id"`
	SecretKey     string `mapstructure:"secret_key"`
	PublicBaseURL string `mapstructure:"public_base_url"`
	UploadPrefix  string `mapstructure:"upload_prefix"`
	MaxImageMB    int64  `mapstructure:"max_image_mb"`
}

// Init 初始化当前模块。
func Init(filePath string) (err error) {
	// 方式1：直接指定配置文件路径（相对路径或者绝对路径）
	// 相对路径：相对执行的可执行文件的相对路径
	//viper.SetConfigFile("./conf/config.yaml")
	// 绝对路径：系统中实际的文件路径
	//viper.SetConfigFile("/Users/liwenzhou/Desktop/bluebell/conf/config.yaml")

	// 方式2：指定配置文件名和配置文件的位置，viper自行查找可用的配置文件
	// 配置文件名不需要带后缀
	// 配置文件位置可配置多个
	//viper.SetConfigName("config") // 指定配置文件名（不带后缀）
	//viper.AddConfigPath(".") // 指定查找配置文件的路径（这里使用相对路径）
	//viper.AddConfigPath("./conf")      // 指定查找配置文件的路径（这里使用相对路径）

	// 基本上是配合远程配置中心使用的，告诉viper当前的数据使用什么格式去解析
	//viper.SetConfigType("json")

	viper.SetConfigFile(filePath)

	err = viper.ReadInConfig() // 读取配置信息
	if err != nil {
		// 读取配置信息失败
		fmt.Printf("viper.ReadInConfig failed, err:%v\n", err)
		return
	}

	// 把读取到的配置信息反序列化到 Conf 变量中
	if err := viper.Unmarshal(Conf); err != nil {
		fmt.Printf("viper.Unmarshal failed, err:%v\n", err)
	}

	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		fmt.Println("配置文件修改了...")
		if err := viper.Unmarshal(Conf); err != nil {
			fmt.Printf("viper.Unmarshal failed, err:%v\n", err)
		}
	})
	return
}
