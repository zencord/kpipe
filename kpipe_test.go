package kpipe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
)

func TestName(t *testing.T) {
	forward, err := New(filepath.Join(os.Getenv("HOME"), ".kube", "config"))
	require.NotNil(t, forward)

	conn, err := forward.Dial(context.Background(), "default", "redis-76747cff6-bq4kz:6379")
	require.Nil(t, err)

	redisClient := redis.NewConn(conn, 3*time.Second, 3*time.Second)
	value, err := redisClient.Do("SET", "ANSWER", "42")
	value, err = redis.Int(redisClient.Do("GET", "ANSWER"))

	require.Nil(t, err)
	require.Equal(t, 42, value)
}
