|![](https://upload.wikimedia.org/wikipedia/commons/thumb/1/17/Warning.svg/156px-Warning.svg.png) | Federation rudder is no longer supported.
|---|---|

# Federation rudder (helm release plugin) which enables federation support for your Helm charts.

## Usage
You need two things - Tiller with `--experimental-release` flag enabled, and this rudder available to Tiller. The easiest way to do it is to add container with federated rudder to Tiller deployment - check out [ruddered-tiller.yaml](manifests/ruddered-tiller.yaml).

## Federation DNS
If you want your deployments distributed across federation to be able to reach pods in other clusters (and you probably do), you need to take a few steps:
- You need to have dns configured in your federation so that proper dns entries are created for federated deployments.
- You need to override hostnames in your charts so they may be expended to federation dns name instead of local cluster name. You can do this by either:
  - Changing your charts/overriding the hostname if the chart provides this option
  - Using additional replacement logic provided by this rudder. Refer to examples/wp-values.yaml file. You need to provide a regular expression which will match the context of the hostname (this can be tricky, as usual with regexes). The `to` part of the `replace` is being rendered by go template with Federation Controller Deployment object retrieved using data in `fed-namespace` and `fed-controller-name`. You may avoid it if you know your federation name ahead of time.

## Test Environment
To setup federation with two clusters:
- `git clone https://github.com/kubernetes/kubernetes $GOPATH/src/k8s.io/kubernetes`
- `cd $GOPATH/src/k8s.io/kubernetes`
- `git checkout release-1.7`
- `make quick-release`
- `git clone https://github.com/lukaszo/kubernetes-dind-federation dind`
- `dind/dind-up-cluster.sh`
- `CLUSTER_NAME=dind2 IP_RANGE=172.128.0.0/16 APISERVER_ADDRESS=172.128.0.1 dind/dind-up-cluster.sh`
- `kubectl config use-context dind`
- `dind/dind-deploy-federation.sh`
- `kubefed join dind2 --host-cluster-context=dind --context=federation`

Populate configmap manifest with generated tls data:
- `git clone https://github.com/kubernetes-helm/rudder-federation.git $GOPATH/src/github.com/kubernetes-helm/rudder-federation`
- `cd $GOPATH/src/github.com/kubernetes-helm/rudder-federation`
- `python utils/populate-configmap.py > manifests/fed-credentials`

Create modified tiller deployment and configmap with tls data:
- `kubectl create -f manifests/`

Run Helm install:
- `helm install stable/wordpress`
