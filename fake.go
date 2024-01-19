package main

import (
	"context"
	"errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"os"
	"slices"
	"time"
)

const SchedulerName = "random-scheduler"

func main() {
	kc, err := makeKubeClient()
	if err != nil {
		panic(err)
	}

	nodeInformer, nodeController := cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return kc.CoreV1().Nodes().List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return kc.CoreV1().Nodes().Watch(context.Background(), options)
		},
	}, &corev1.Node{}, 0, cache.ResourceEventHandlerFuncs{})

	go nodeController.Run(wait.NeverStop)

	_, podController := cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = "spec.schedulerName=" + SchedulerName
			return kc.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = "spec.schedulerName=" + SchedulerName
			return kc.CoreV1().Pods(corev1.NamespaceAll).Watch(context.Background(), options)
		},
	}, &corev1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName != "" {
				return
			}

			nodeName := selectNode(pod, nodeInformer)
			if nodeName == "" {
				klog.Errorf("no available node pod=%s/%s", pod.Namespace, pod.Name)
				return
			}

			binding := &corev1.Binding{
				ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID},
				Target:     corev1.ObjectReference{Kind: "Node", Name: nodeName},
			}

			err = kc.CoreV1().Pods(pod.Namespace).Bind(context.Background(), binding, metav1.CreateOptions{})

			if err != nil {
				klog.ErrorS(err, "bind pod error", "pod", pod.Name)
			}
		},
	})

	for !nodeController.HasSynced() {
		time.Sleep(time.Second)
	}

	klog.Infoln("node controller has synced")

	podController.Run(wait.NeverStop)
}

func selectNode(pod *corev1.Pod, nodeInformer cache.Store) string {
	var nodes []*corev1.Node
	for _, v := range nodeInformer.List() {
		if node := v.(*corev1.Node); filterNode(node) {
			nodes = append(nodes, node)
		}
	}

	switch len(nodes) {
	case 0:
		return ""
	case 1:
		return nodes[0].Name
	default:
		sortNodes(pod, nodes)
		return nodes[0].Name
	}
}

func filterNode(node *corev1.Node) bool {
	return !node.Spec.Unschedulable
}

func sortNodes(pod *corev1.Pod, nodes []*corev1.Node) {
	slices.SortFunc(nodes, func(a, b *corev1.Node) int {
		return getNodeScore(pod, a) - getNodeScore(pod, b)
	})
}

func getNodeScore(pod *corev1.Pod, node *corev1.Node) int {
	var score int

	for _, container := range pod.Spec.Containers {
		if hasImage(container.Image, node) {
			score += 100
		}
	}

	if score > 0 {
		return score
	}

	return rand.Intn(50)
}

func hasImage(image string, node *corev1.Node) bool {
	return slices.ContainsFunc(node.Status.Images, func(containerImage corev1.ContainerImage) bool {
		return slices.Contains(containerImage.Names, image)
	})
}

func makeKubeClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()

	if err != nil {
		if errors.Is(err, rest.ErrNotInCluster) {
			kubeconfPath := os.Getenv("KUBECONFIG")
			if kubeconfPath == "" {
				kubeconfPath = os.Getenv("HOME") + "/.kube/config"
			}

			config, err = clientcmd.BuildConfigFromFlags("", kubeconfPath)
		}
	}

	return kubernetes.NewForConfig(config)
}
