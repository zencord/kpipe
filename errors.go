package kpipe

import "fmt"

type NoActivePodsForService struct {
	Namespace   string
	ServiceName string
}

func (p *NoActivePodsForService) Error() string {
	return fmt.Sprintf("No pod for service '%s' in namespace '%s' is in running state.", p.ServiceName, p.Namespace)
}
