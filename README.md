## Network Scheduler

```shell
# create namespace
$ kubectl create ns network-scheduler

# install scheduler & prober
$ kubectl apply -k ./deploy

# test scheduler
$ kubectl create -f test-pod.yaml
```