/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"regexp"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	pullInterval = 10 * time.Second
	pullTimeout  = 5 * time.Minute
)

// PodIPRE tests if there's a valid IP in a easy way
var PodIPRE = regexp.MustCompile(`\d{0,3}\.\d{0,3}\.\d{0,3}\.\d{0,3}`)

// getPodList is a wrapper around listing pods
func getPodList(cs clientset.Interface, ns string) (*v1.PodList, error) {
	var pods *v1.PodList
	var err error
	if wait.PollImmediate(poll, singleCallTimeout, func() (bool, error) {
		pods, err = cs.CoreV1().Pods(ns).List(metav1.ListOptions{})
		if err != nil {
			if IsRetryableAPIError(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}) != nil {
		return pods, err
	}
	return pods, nil
}

// LogPodStatus logs the rate of pending
func LogPodStatus(cs clientset.Interface, ns string) error {
	pods, err := getPodList(cs, ns)
	if err != nil {
		return err
	}
	pendingPodCount := 0
	for _, p := range pods.Items {
		if p.Status.Phase == v1.PodPending {
			pendingPodCount++
		}
	}
	Logf("%d of %d pods in namespace %s are pending", pendingPodCount, len(pods.Items), ns)
	return nil
}

// DeletePodsInNamespace deletes all pods in the namespace
func DeletePodsInNamespace(cs clientset.Interface, ns string) error {
	Logf("Deleting all pods in namespace %s", ns)
	pods, err := getPodList(cs, ns)
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		err = DeletePod(cs, ns, p.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeletePod deletes a single pod
func DeletePod(cs clientset.Interface, ns string, podName string) error {
	err := cs.CoreV1().Pods(ns).Delete(podName, nil)
	Logf("Deleting pod %s in namespace %s", podName, ns)
	if err != nil {
		return err
	}
	return wait.PollImmediate(poll, deletionTimeout, func() (bool, error) {
		if _, err := cs.CoreV1().Pods(ns).Get(podName, metav1.GetOptions{}); err != nil {
			return apierrs.IsNotFound(err), nil
		}
		return false, nil
	})
}

// CreatePod creates a new pod
func CreatePod(cs clientset.Interface, ns string, manifest *v1.Pod) error {
	Logf("creating pod %s in namespace %s", manifest.Name, ns)
	_, err := cs.CoreV1().Pods(ns).Create(manifest)
	if err != nil {
		return err
	}
	return nil
}

// GetPodLogs gets the log of the given pods
func GetPodLogs(cs clientset.Interface, ns, podName string, opts *v1.PodLogOptions) ([]byte, error) {
	Logf("getting the log of pod %s", podName)
	return cs.CoreV1().Pods(ns).GetLogs(podName, opts).Do().Raw()
}

// GetPodOutboundIP returns the outbound IP of the given pod
func GetPodOutboundIP(cs clientset.Interface, podTemplate *v1.Pod, nsName string) (string, error) {
	var log []byte
	err := wait.PollImmediate(pullInterval, pullTimeout, func() (bool, error) {
		pod, err := cs.CoreV1().Pods(nsName).Get(podTemplate.Name, metav1.GetOptions{})
		if err != nil {
			if IsRetryableAPIError(err) {
				return false, nil
			}
			return false, err
		}
		if pod.Status.Phase != v1.PodSucceeded {
			Logf("waiting for the pod to succeed, current status: %s", pod.Status.Phase)
			return false, nil
		}
		if pod.Status.ContainerStatuses[0].State.Terminated == nil || pod.Status.ContainerStatuses[0].State.Terminated.Reason != "Completed" {
			Logf("waiting for the container to be completed")
			return false, nil
		}
		log, err = GetPodLogs(cs, nsName, podTemplate.Name, &v1.PodLogOptions{})
		if err != nil {
			Logf("retrying getting pod's log")
			return false, nil
		}
		return PodIPRE.MatchString(string(log)), nil
	})
	if err != nil {
		return "", err
	}
	Logf("Got pod outbound IP %s", string(log))
	return string(log), nil
}

// WaitPodTo returns True if pod is in the specific phase during
// a short period of time
func WaitPodTo(phase v1.PodPhase, cs clientset.Interface, podTemplate *v1.Pod, nsName string) (result bool, err error) {
	if err := wait.PollImmediate(pullInterval, pullTimeout, func() (result bool, err error) {
		pod, err := cs.CoreV1().Pods(nsName).Get(podTemplate.Name, metav1.GetOptions{})
		if err != nil {
			if IsRetryableAPIError(err) {
				return false, nil
			}
			return false, err
		}
		if pod.Status.Phase != phase {
			Logf("waiting for the pod status to be %s, current status: %s", phase, pod.Status.Phase)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return false, err
	}
	return true, err
}
