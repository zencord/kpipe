package kpipe

import (
	"context"
	"errors"
	"time"

	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
)

func getPodsForSvc(svc *v1.Service, namespace string, k8sClient *kubernetes.Clientset) (*v1.PodList, error) {
	set := labels.Set(svc.Spec.Selector)
	listOptions := metav1.ListOptions{LabelSelector: set.AsSelector().String()}
	return k8sClient.CoreV1().Pods(namespace).List(context.TODO(), listOptions)
}

func WaitForServiceRunning(kube *kubernetes.Clientset, namespace, service string, duration time.Duration) error {

	then := time.Now()

	dep, err := kube.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})

	for then.Add(duration).After(time.Now()) {
		dep, err = kube.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})

		if err != nil {
			return err
		}

		pods, _ := getPodsForSvc(dep, namespace, kube)
		for _, pod := range pods.Items {
			if pod.Status.Phase == "Running" {
				return nil
			}
		}

		time.Sleep(duration / 15)
	}

	return errors.New("time out waiting for service " + service + " in namespace " + namespace)
}

func PatchDeploymentObject(client kubernetes.Interface, namespace, service string, modifier func(deployment *appv1.Deployment)) error {

	cur, err := client.AppsV1().Deployments(namespace).Get(context.TODO(), service, metav1.GetOptions{})

	if err != nil {
		return err
	}

	mod := *cur
	modifier(cur)

	curJson, err := json.Marshal(cur)
	if err != nil {
		return err
	}

	modJson, err := json.Marshal(mod)
	if err != nil {
		return err
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(curJson, modJson, appv1.Deployment{})
	if err != nil {
		return err
	}

	if len(patch) == 0 || string(patch) == "{}" {
		return nil
	}

	_, err = client.AppsV1().Deployments(cur.Namespace).Patch(context.TODO(), cur.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}
