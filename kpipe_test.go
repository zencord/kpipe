package kpipe

import (
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
)

func TestConnectingToRunningPod(t *testing.T) {
	forward := createForwarder(t)

	conn, err := forward.Dial("default", "redis")
	require.Nil(t, err)

	verifyPortCommunication(t, conn)
}

func TestBadPort(t *testing.T) {
	forward := createForwarder(t)

	conn, err := forward.Dial("default", "redis:42")
	require.Nil(t, err)

	bytes := make([]byte, 1, 1)
	_, err = conn.Read(bytes)

	require.Contains(t, err.Error(), "Connection refused")
}

func TestNoExistentService(t *testing.T) {
	forward := createForwarder(t)

	_, err := forward.Dial("default", "fakaka")
	require.Contains(t, err.Error(), "fakaka")
}

func TestFailureAndRecoveryForUnavailablePod(t *testing.T) {
	forward := createForwarder(t)
	forward.SetResolver(func(kube *kubernetes.Clientset, ns, service string) bool {

		err := PatchDeploymentObject(kube, "default", "broken", func(deployment *v1.Deployment) {
			replicas := int32(1)
			deployment.Spec.Replicas = &replicas

		})

		require.Nil(t, err)

		return WaitForServiceRunning(kube, "default", "broken", 30*time.Second) == nil
	})

	conn, err := forward.Dial("default", "broken")
	require.Nil(t, err)

	verifyPortCommunication(t, conn)
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
