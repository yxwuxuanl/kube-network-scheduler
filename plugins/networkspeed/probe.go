package networkspeed

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"net/http"
	"strings"
	"time"
)

const (
	ProbeTargetAnnotation = "network-scheduler/probe-target"
	ProbeModuleAnnotation = "network-scheduler/probe-module"
	DefaultProbeModule    = "http_2xx"
	ProbeDurationMetric   = "probe_duration_seconds"
)

func (n *NetworkSpeedPlugin) getProber(nodeName string) (*v1.Pod, error) {
	pods, err := n.handle.
		SharedInformerFactory().
		Core().
		V1().
		Pods().
		Lister().
		List(labels.SelectorFromSet(n.config.Selector))

	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		if pod.Spec.NodeName == nodeName &&
			pod.Namespace == n.config.Namespace &&
			pod.Status.Phase == v1.PodRunning {
			return pod, nil
		}
	}

	return nil, errors.New("no available prober")
}

func (n *NetworkSpeedPlugin) doProbe(ctx context.Context, nodename, target, module string) (duration time.Duration, err error) {
	prober, err := n.getProber(nodename)
	if err != nil {
		return 0, fmt.Errorf("getProber error: %w", err)
	}

	probeUrl := fmt.Sprintf(
		"http://%s:%d/probe?module=%s&target=%s",
		prober.Status.PodIP, n.config.Port, module, target,
	)

	r, err := http.NewRequest(http.MethodGet, probeUrl, nil)
	if err != nil {
		return 0, err
	}

	timeoutCtx, cancel := context.WithTimeout(
		ctx,
		time.Millisecond*time.Duration(n.config.Timeout),
	)
	defer cancel()

	res, err := http.DefaultClient.Do(r.WithContext(timeoutCtx))
	if err != nil {
		return 0, err
	}

	if res.StatusCode != http.StatusOK {
		return 0, errors.New("bad status: " + res.Status)
	}

	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)

	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, ProbeDurationMetric) {
			items := strings.Fields(line)
			if len(items) >= 2 {
				return time.ParseDuration(items[1] + "s")
			}
			break
		}
	}

	return 0, fmt.Errorf("can't get %s metric", ProbeDurationMetric)
}

func getProbeConfig(pod *v1.Pod) (target, module string, ok bool) {
	if target = pod.Annotations[ProbeTargetAnnotation]; target == "" {
		return "", "", false
	}

	if module = pod.Annotations[ProbeModuleAnnotation]; module == "" {
		module = DefaultProbeModule
	}

	return target, module, true
}
