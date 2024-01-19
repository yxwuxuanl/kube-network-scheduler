package main

import (
	"k8s-network-scheduler/plugins/networkspeed"
	"k8s.io/component-base/cli"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	"os"
)

func main() {
	cmd := app.NewSchedulerCommand(
		app.WithPlugin(networkspeed.PluginName, networkspeed.New),
	)

	code := cli.Run(cmd)
	os.Exit(code)
}
