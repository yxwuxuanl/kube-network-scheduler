package networkspeed

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"os"
	"slices"
)

const PluginName = "NetworkSpeed"

type Config struct {
	Selector  map[string]string `json:"selector"`
	Namespace string            `json:"namespace"`
	Timeout   int64             `json:"timeout"`
	Port      int               `json:"port"`
}

type NetworkSpeedPlugin struct {
	config *Config
	handle framework.Handle
}

func (n *NetworkSpeedPlugin) Name() string {
	return PluginName
}

func (n *NetworkSpeedPlugin) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	target, module, ok := getProbeConfig(p)
	if !ok {
		return 0, nil
	}

	duration, err := n.doProbe(ctx, nodeName, target, module)
	if err != nil {
		klog.Error("doProbe error", "err", err.Error())
		return 0, nil
	}

	klog.InfoS(
		"doProbe",
		"node", nodeName,
		"target", target,
		"pod", p.Namespace+"/"+p.Name,
		"duration", duration,
	)

	return duration.Microseconds(), nil
}

func (n *NetworkSpeedPlugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if _, err := n.getProber(nodeInfo.Node().Name); err != nil {
		klog.ErrorS(
			err, "Filter error",
			"node", nodeInfo.Node().Name,
			"pod", pod.Namespace+"/"+pod.Name,
		)
		return framework.NewStatus(framework.Unschedulable)
	}

	return nil
}

func (n *NetworkSpeedPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, p *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	slices.SortFunc(scores, func(a, b framework.NodeScore) int {
		return int(a.Score - b.Score)
	})

	for i, score := range scores {
		if score.Score == 0 {
			continue
		}
		scores[i].Score = int64((len(scores) - i) * (100 / len(scores)))
	}

	klog.InfoS("NormalizeScore", "scores", scores)

	return nil
}

func (n *NetworkSpeedPlugin) ScoreExtensions() framework.ScoreExtensions {
	return n
}

func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	var config Config
	if err := frameworkruntime.DecodeInto(configuration, &config); err != nil {
		return nil, err
	}

	if config.Selector == nil {
		return nil, errors.New("prober selector is nil")
	}

	if config.Timeout == 0 {
		config.Timeout = 3000
	}

	if config.Namespace == "" {
		data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return nil, fmt.Errorf("get namesapce error: %w", err)
		}

		config.Namespace = string(data)
	}

	if config.Port == 0 {
		config.Port = 9115
	}

	klog.InfoS("scheduler config", "config", config)

	return &NetworkSpeedPlugin{
		config: &config,
		handle: handle,
	}, nil
}
