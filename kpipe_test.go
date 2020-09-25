package kpipe

import (
	"context"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
)

func TestConnectingToRunningPod(t *testing.T) {
	forward := createForwarder(t)

	conn, err := forward.Dial(context.Background(), "default", "redis")
	require.Nil(t, err)

	verifyPortCommunication(t, conn)
}

func TestBadPort(t *testing.T) {
	forward := createForwarder(t)

	conn, err := forward.Dial(context.Background(), "default", "redis:42")
	require.Nil(t, err)

	bytes := make([]byte, 1, 1)
	_, err = conn.Read(bytes)

	require.Contains(t, err.Error(), "Connection refused")
}

func TestBadService(t *testing.T) {
	forward := createForwarder(t)

	_, err := forward.Dial(context.Background(), "default", "fakaka")
	require.Contains(t, err.Error(), "fakaka")
}

func createForwarder(t *testing.T) *PipeForward {
	forward, err := New()
	require.Nil(t, err)
	return forward
}

func verifyPortCommunication(t *testing.T, conn net.Conn) {
	redisClient := redis.NewConn(conn, 3*time.Second, 3*time.Second)
	answer := rand.New(rand.NewSource(time.Now().UnixNano())).Int()
	value, err := redisClient.Do("SET", "ANSWER", answer)
	require.Nil(t, err)
	value, err = redis.Int(redisClient.Do("GET", "ANSWER"))
	require.Nil(t, err)
	require.Equal(t, answer, value)
}
