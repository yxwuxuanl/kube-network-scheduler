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
	"log/slog"
	"os"
	"sync"
	"time"
)

const SchedulerName = "random-scheduler"

var (
	nodeInformer cache.Store
	nodeOnce     sync.Once
)

func main() {
	clientset, err := makeKubeClient()
	if err != nil {
		panic(err)
	}

	_, ctr := cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			// 仅获取 `schedulerName` 为 `random-scheduler` 的 Pod
			options.FieldSelector = "spec.schedulerName=" + SchedulerName
			return clientset.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			// 仅监听 `schedulerName` 为 `random-scheduler` 的 Pod
			options.FieldSelector = "spec.schedulerName=" + SchedulerName
			return clientset.CoreV1().Pods(corev1.NamespaceAll).Watch(context.Background(), options)
		},
	}, &corev1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)

			if pod.Spec.NodeName != "" {
				return
			}

			nodes := getNodes(clientset)           // 获取节点
			availableNodes := filterNodes(nodes)   // 过滤节点
			nodename := selectNode(availableNodes) // 选择节点

			if nodename == "" { // 无可用节点
				slog.Error("no available node", "pod", cache.MetaObjectToName(pod))
				return
			}

			binding := &corev1.Binding{ // 创建 `binding` SubResource
				ObjectMeta: pod.ObjectMeta,
				Target:     corev1.ObjectReference{Kind: "Node", Name: nodename},
			}

			// 绑定
			err := clientset.
				CoreV1().
				Pods(pod.Namespace).
				Bind(context.Background(), binding, metav1.CreateOptions{})

			if err != nil {
				slog.Error(
					"bind pod error",
					"error", err,
					"pod", cache.MetaObjectToName(pod),
				)
			}
		},
	})

	ctr.Run(wait.NeverStop)
}

func getNodes(clientset *kubernetes.Clientset) []*corev1.Node {
	nodeOnce.Do(func() {
		var nodeController cache.Controller

		nodeInformer, nodeController = cache.NewInformer(&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return clientset.CoreV1().Nodes().List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return clientset.CoreV1().Nodes().Watch(context.Background(), options)
			},
		}, &corev1.Node{}, 0, cache.ResourceEventHandlerFuncs{})

		go nodeController.Run(wait.NeverStop)

		for !nodeController.HasSynced() {
			time.Sleep(time.Second)
		}

		slog.Info("node informer has synced")
	})

	var nodes []*corev1.Node
	for _, v := range nodeInformer.List() {
		nodes = append(nodes, v.(*corev1.Node))
	}

	return nodes
}

func filterNodes(nodes []*corev1.Node) []*corev1.Node {
	var result []*corev1.Node
	for _, node := range nodes {
		if !node.Spec.Unschedulable {
			result = append(result, node)
		}
	}

	return result
}

func selectNode(nodes []*corev1.Node) string {
	switch len(nodes) {
	case 0:
		return ""
	case 1:
		return nodes[0].Name
	default:
		return nodes[rand.Intn(len(nodes)-1)].Name
	}
}

func makeKubeClient() (*kubernetes.Clientset, error) {
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
