package networkspeed

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (n *NetworkSpeed) getProber(nodeName string) *v1.Pod {
	pods, err := n.handle.
		SharedInformerFactory().
		Core().
		V1().
		Pods().
		Lister().
		List(labels.SelectorFromSet(n.config.Prober.Selector))

	if err != nil {
		klog.ErrorS(err, "list prober error")
		return nil
	}

	for _, pod := range pods {
		if pod.Namespace == n.config.Prober.Namespace &&
			pod.Spec.NodeName == nodeName &&
			pod.Status.Phase == v1.PodRunning {
			return pod
		}
	}

	return nil
}

func (n *NetworkSpeed) doProbe(ctx context.Context, proberAddr string) (duration float64, err error) {
	proberConf := n.config.Prober
	probeUrl := fmt.Sprintf(
		"http://%s:%d/probe?module=%s&target=%s",
		proberAddr,
		proberConf.Port,
		proberConf.Module,
		proberConf.Target,
	)

	r, err := http.NewRequest(http.MethodGet, probeUrl, nil)
	if err != nil {
		return 0, err
	}

	timeout, cancel := context.WithTimeout(ctx, time.Microsecond*time.Duration(proberConf.Timeout))
	defer cancel()

	res, err := http.DefaultClient.Do(r.WithContext(timeout))
	if err != nil {
		return 0, err
	}

	if res.StatusCode != http.StatusOK {
		return 0, errors.New("bad status: " + res.Status)
	}

	rl := bufio.NewScanner(res.Body)
	defer res.Body.Close()

	for rl.Scan() {
		if line := rl.Text(); strings.HasPrefix(line, "probe_duration_seconds") {
			items := strings.Fields(line)
			if len(items) >= 2 {
				return strconv.ParseFloat(items[1], 64)
			}
			break
		}
	}

	return 0, err
}
