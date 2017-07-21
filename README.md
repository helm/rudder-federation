# Federation rudder (helm release plugin) which enables federation support for your Helm charts.

## Usage
You need two things - Tiller with `--experimental-release` flag enabled, and this rudder available to Tiller. The easiest way to do it is to add container with federated rudder to Tiller deployment - check out [ruddered-tiller.yaml](manifests/ruddered-tiller.yaml).


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
