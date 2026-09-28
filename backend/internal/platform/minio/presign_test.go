package minio_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/minio/minio-go/v7"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

// В контейнерном стенде backend ходит в MinIO по внутреннему адресу, а браузеру нужен публичный:
// presigned-ссылка обязана быть подписана под публичный Host, иначе браузер её не откроет.
func TestPresigner_UsesPublicEndpoint(t *testing.T) {
	c := dbtest.NewMinioPublic(t, "files.example.test:9000")
	ctx := context.Background()

	get, err := c.Presigner().PresignedGetObject(ctx, c.BucketName(), "a/b.pdf", time.Minute, nil)
	require.NoError(t, err)
	require.Equal(t, "files.example.test:9000", get.Host, "браузер получает публичный адрес, а не внутренний")
	require.NotEqual(t, c.Raw().EndpointURL().Host, get.Host)
	require.NotEmpty(t, get.Query().Get("X-Amz-Signature"))

	policy := minio.NewPostPolicy()
	require.NoError(t, policy.SetBucket(c.BucketName()))
	require.NoError(t, policy.SetKey("a/b.pdf"))
	require.NoError(t, policy.SetExpires(time.Now().Add(time.Minute)))
	post, _, err := c.Presigner().PresignedPostPolicy(ctx, policy)
	require.NoError(t, err)
	require.Equal(t, "files.example.test:9000", post.Host)

	// серверные операции по-прежнему идут на реальный адрес
	exists, err := c.Raw().BucketExists(ctx, c.BucketName())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestPresigner_DefaultsToServerEndpoint(t *testing.T) {
	c := dbtest.NewMinio(t)
	u, err := c.Presigner().PresignedGetObject(context.Background(), c.BucketName(), "k", time.Minute, url.Values{})
	require.NoError(t, err)
	require.Equal(t, c.Raw().EndpointURL().Host, u.Host)
}
