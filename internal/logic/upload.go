package logic

import (
	"bluebell/internal/setting"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

var (
	ErrCOSDisabled      = errors.New("cos upload is disabled")
	ErrCOSConfigInvalid = errors.New("cos config is invalid")
	ErrImageTooLarge    = errors.New("image is too large")
	ErrImageTypeInvalid = errors.New("image type is invalid")
)

type UploadedImage struct {
	URL string `json:"url"`
}

// UploadImageToCOS 校验并上传帖子图片到腾讯云 COS。
func UploadImageToCOS(ctx context.Context, file multipart.File, header *multipart.FileHeader) (*UploadedImage, error) {
	cfg := setting.Conf.COSConfig
	if cfg == nil || !cfg.Enabled {
		return nil, ErrCOSDisabled
	}

	secretID := firstNonEmpty(os.Getenv("TENCENT_COS_SECRET_ID"), cfg.SecretID)
	secretKey := firstNonEmpty(os.Getenv("TENCENT_COS_SECRET_KEY"), cfg.SecretKey)
	if cfg.BucketURL == "" || secretID == "" || secretKey == "" {
		return nil, ErrCOSConfigInvalid
	}

	maxBytes := cfg.MaxImageMB * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	if header.Size > maxBytes {
		return nil, ErrImageTooLarge
	}

	limited := io.LimitReader(file, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrImageTooLarge
	}

	contentType := http.DetectContentType(data)
	ext, ok := imageExt(contentType)
	if !ok {
		return nil, ErrImageTypeInvalid
	}

	bucketURL, err := url.Parse(cfg.BucketURL)
	if err != nil {
		return nil, ErrCOSConfigInvalid
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  secretID,
			SecretKey: secretKey,
		},
		Timeout: 30 * time.Second,
	})

	key := buildImageKey(cfg.UploadPrefix, ext)
	_, err = client.Object.Put(ctx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return nil, err
	}

	return &UploadedImage{URL: buildPublicURL(cfg, key)}, nil
}

func imageExt(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

func buildImageKey(prefix, ext string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		prefix = "bluebell/posts"
	}
	name := randomHex(16) + ext
	return path.Join(prefix, time.Now().Format("20060102"), name)
}

func buildPublicURL(cfg *setting.COSConfig, key string) string {
	base := strings.TrimRight(cfg.PublicBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(cfg.BucketURL, "/")
	}
	return base + "/" + strings.TrimLeft(key, "/")
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
