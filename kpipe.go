package kpipe

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"google.golang.org/grpc/test/bufconn"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport/spdy"
)

const portForwardProtocolV1Name = "portforward.k8s.io"

type PipeForward struct {
	config    *rest.Config
	mux       sync.Mutex
	requestId int
}

func New(configFile string) (*PipeForward, error) {
	config, err := clientcmd.BuildConfigFromFlags("", configFile)
	if err != nil {
		return &PipeForward{}, err
	}

	return &PipeForward{
		config: config,
	}, nil
}

func (pf *PipeForward) Dial(ctx context.Context, namespace, service string) (net.Conn, error) {

	service, port, err := net.SplitHostPort(service)

	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, service)
	hostIP := strings.TrimLeft(pf.config.Host, "https:/")

	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost,
		&url.URL{Scheme: "https", Path: path, Host: hostIP})

	if err != nil {
		return nil, err
	}

	target, _, err := dialer.Dial(portForwardProtocolV1Name)

	if err != nil {
		return nil, err
	}

	listener := bufconn.Listen(1024 * 10)
	go pf.forward(ctx, listener, target, port)
	c, e := listener.Dial()
	return c, e
}

func (pf *PipeForward) getNextRequestId() int {
	pf.mux.Lock()
	pf.requestId++
	pf.mux.Unlock()
	return pf.requestId
}

// forward waits for new connections to listener and handles them in
// the background.
func (pf *PipeForward) forward(ctx context.Context, listener *bufconn.Listener, target httpstream.Connection, port string) error {
	for {
		source, err := listener.Accept()

		if err != nil {
			return err
		}
		return handleConnection(ctx, source, target, pf.getNextRequestId(), port)
	}
}

// handleConnection copies data between the local connection and the stream to
// the remote server.
func handleConnection(ctx context.Context, source net.Conn, target httpstream.Connection, requestID int, port string) error {
	defer source.Close()

	// create error stream
	headers := http.Header{}
	headers.Set(v1.StreamType, v1.StreamTypeError)
	headers.Set(v1.PortHeader, fmt.Sprintf("%s", port))
	headers.Set(v1.PortForwardRequestIDHeader, strconv.Itoa(requestID))
	errorStream, err := target.CreateStream(headers)
	if err != nil {
		return errors.Wrap(err, "create error stream")
	}
	// we're not writing to this stream
	errorStream.Close()

	errorChan := make(chan error)
	go func() {
		message, err := ioutil.ReadAll(errorStream)
		switch {
		case err != nil:
			errorChan <- fmt.Errorf("error reading from error stream for servicePort %s: %v", port, err)
		case len(message) > 0:
			errorChan <- fmt.Errorf("an error occurred forwarding %s: %v", port, string(message))
		}
		close(errorChan)
	}()

	// create data stream
	headers.Set(v1.StreamType, v1.StreamTypeData)
	dataStream, err := target.CreateStream(headers)
	if err != nil {
		return errors.Wrap(err, "create data stream")
	}

	localError := make(chan struct{})
	remoteDone := make(chan struct{})

	go func() {
		// Copy from the remote side to the local servicePort.
		if _, err := io.Copy(source, dataStream); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			panic("failed incoming data copy:" + err.Error())
		}

		// inform the select below that the remote copy is done
		close(remoteDone)
	}()

	go func() {
		// inform server we're not sending any more data after copy unblocks
		defer dataStream.Close()

		// Copy from the local servicePort to the remote side.
		if _, err := io.Copy(dataStream, source); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			close(localError)
			panic("failed outgoing data copy:" + err.Error())
		}
	}()

	// wait for either a local->remote error or for copying from remote->local to finish / or cancel
	select {
	case <-remoteDone:
	case <-localError:
	case <-ctx.Done():

	}

	// always expect something on errorChan (it may be nil)
	return <-errorChan
}
