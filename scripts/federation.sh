#!/usr/bin/env bash

git clone https://github.com/kubernetes/kubernetes $GOPATH/src/k8s.io/kubernetes
cd $GOPATH/src/k8s.io/kubernetes
git checkout release-1.7
make WHAT=cmd/hyperkube
make WHAT=cmd/kubectl
make WHAT=federation/cmd/kubefed
git clone https://github.com/lukaszo/kubernetes-dind-federation dind
dind/dind-up-cluster.sh
CLUSTER_NAME=dind2 IP_RANGE=172.128.0.0/16 APISERVER_ADDRESS=172.128.0.1 dind/dind-up-cluster.sh
kubectl config use-context dind
dind/dind-deploy-federation.sh
kubefed join dind2 --host-cluster-context=dind --context=federation
git clone https://github.com/kubernetes-helm/rudder-federation.git $GOPATH/src/github.com/kubernetes-helm/rudder-federation
cd $GOPATH/src/github.com/kubernetes-helm/rudder-federation
python utils/populate-configmap.py > manifests/fed-credentials.yaml
