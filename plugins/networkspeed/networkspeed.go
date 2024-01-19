package networkspeed

import (
	"context"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"math"
	"sync/atomic"
)

const PluginName = "NetworkSpeed"

type Config struct {
	Prober struct {
		Selector  map[string]string `json:"selector"`
		Namespace string            `json:"namespace"`
		Port      int               `json:"port"`
		Module    string            `json:"module"`
		Target    string            `json:"target"`
		Timeout   int64             `json:"timeout"`
	} `json:"prober"`
	MaxPods int32 `json:"maxPods"`
}

type NetworkSpeed struct {
	config        *Config
	remainingPods *int32
	handle        framework.Handle
}

func (n *NetworkSpeed) Name() string {
	return PluginName
}

func (n *NetworkSpeed) PreEnqueue(ctx context.Context, p *v1.Pod) *framework.Status {
	if atomic.LoadInt32(n.remainingPods) < 0 {
		return framework.NewStatus(framework.Unschedulable)
	}

	return nil
}

func (n *NetworkSpeed) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if n.getProber(nodeInfo.Node().Name) != nil {
		return framework.NewStatus(framework.Success)
	}

	return framework.NewStatus(framework.Unschedulable, "no available prober pod")
}

func (n *NetworkSpeed) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	prober := n.getProber(nodeName)
	if prober == nil {
		return 0, nil
	}

	duration, err := n.doProbe(ctx, prober.Status.PodIP)
	if err != nil {
		klog.ErrorS(err, "doProbe error")
		return 0, nil
	}

	var score int64

	if duration > 0 {
		score = int64(math.Ceil(1-duration) * 100)
	}

	klog.InfoS(
		"doProbe",
		"nodeName", nodeName, "duration", duration, "score", score,
	)

	return score, nil
}

func (n *NetworkSpeed) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func (n *NetworkSpeed) PostBind(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) {
	atomic.AddInt32(n.remainingPods, -1)
}

func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	var config Config
	if err := frameworkruntime.DecodeInto(configuration, &config); err != nil {
		return nil, err
	}

	if config.Prober.Timeout == 0 {
		config.Prober.Timeout = 500
	}

	remainingPods := config.MaxPods

	return &NetworkSpeed{
		config:        &config,
		remainingPods: &remainingPods,
		handle:        handle,
	}, nil
}
