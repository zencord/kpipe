package kpipe

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
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

func New() (*PipeForward, error) {
	return NewFromFile(filepath.Join(os.Getenv("HOME"), ".kube", "config"))
}
func NewFromFile(configFile string) (*PipeForward, error) {
	config, err := clientcmd.BuildConfigFromFlags("", configFile)
	if err != nil {
		return &PipeForward{}, err
	}

	return &PipeForward{
		config: config,
	}, nil
}

func getPodsForSvc(svc *v1.Service, namespace string, k8sClient *kubernetes.Clientset) (*v1.PodList, error) {
	set := labels.Set(svc.Spec.Selector)
	listOptions := metav1.ListOptions{LabelSelector: set.AsSelector().String()}
	return k8sClient.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
}

func (p *PipeForward) getPodNameFromService(namespace, service string) (string, []int32, error) {
	clientset, err := kubernetes.NewForConfig(p.config)

	if err != nil {
		return "", nil, errors.Wrap(err, "failed to connect to kube cluster.")
	}

	services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		return "", nil, errors.Wrap(err, "failed to get services form kube cluster.")
	}

	for _, srv := range services.Items {
		if strings.EqualFold(srv.Name, service) {
			pods, err := getPodsForSvc(&srv, namespace, clientset)
			if err != nil {
				return "", nil, errors.Wrap(err, "failed to get pods for service:"+service)
			}
			for _, pod := range pods.Items {
				ports := getPorts(pod)
				return pod.Name, ports, nil
			}
		}
	}

	return "", nil, errors.New("failed to find any active pod for " + service + " in namespace " + namespace)
}

func getPorts(pod v1.Pod) []int32 {
	ports := make([]int32, 0)
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			ports = append(ports, p.ContainerPort)
		}
	}

	return ports
}

func (p *PipeForward) Dial(ctx context.Context, namespace, service string) (net.Conn, error) {

	service, port, err := splitServicePort(service)

	if err != nil {
		return nil, err
	}

	podName, availablePorts, err := p.getPodNameFromService(namespace, service)

	if err != nil {
		return nil, errors.Wrap(err, "failed to get a pod for service "+service)
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := strings.TrimLeft(p.config.Host, "https:/")

	transport, upgrader, err := spdy.RoundTripperFor(p.config)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost,
		&url.URL{Scheme: "https", Path: path, Host: hostIP})

	if err != nil {
		return nil, err
	}

	if len(availablePorts) == 0 {
		return nil, errors.Errorf("Service %s does not expose any ports", service)
	}

	if port == "" {
		if len(availablePorts) == 1 {
			port = strconv.FormatInt(int64(availablePorts[0]), 10)
		} else {
			return nil, errors.Errorf("No port specified for service %s and more than one is exposed %v", service, availablePorts)
		}
	}

	target, _, err := dialer.Dial(portForwardProtocolV1Name)

	if err != nil {
		return nil, err
	}
	portInt, err := strconv.Atoi(port)

	if err != nil {
		errors.Errorf("invalid port number %s for service %s", port, service)
	}

	return createConnection(target, p.getNextRequestId(), service, portInt)
}

func splitServicePort(service string) (string, string, error) {

	if !strings.Contains(service, ":") {
		return service, "", nil
	}

	service, port, err := net.SplitHostPort(service)
	return service, port, err
}

func (p *PipeForward) getNextRequestId() int {
	p.mux.Lock()
	p.requestId++
	p.mux.Unlock()
	return p.requestId
}

// handleConnection copies data between the local connection and the stream to
// the remote server.
func createConnection(target httpstream.Connection, requestID int, service string, port int) (*Connection, error) {
	// create error stream
	headers := http.Header{}
	headers.Set(v1.StreamType, v1.StreamTypeError)
	headers.Set(v1.PortHeader, fmt.Sprintf("%d", port))
	headers.Set(v1.PortForwardRequestIDHeader, strconv.Itoa(requestID))
	errorStream, err := target.CreateStream(headers)

	if err != nil {
		return nil, errors.Wrap(err, "create error stream")
	}

	errorChan := make(chan error)

	go func() {
		message, err := ioutil.ReadAll(errorStream)
		switch {
		case err != nil:
			errorChan <- fmt.Errorf("error reading from error stream for  %s:%d: %s", service, port, err.Error())
		case len(message) > 0:
			errorChan <- errors.New(string(message))
		}
		close(errorChan)
	}()

	// create data stream
	headers.Set(v1.StreamType, v1.StreamTypeData)
	dataStream, err := target.CreateStream(headers)
	if err != nil {
		return nil, errors.Wrap(err, "create data stream")
	}

	return &Connection{
		target:       dataStream,
		errorChannel: errorChan,
		service:      service,
		port:         port,
	}, nil
}
