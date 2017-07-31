#!/usr/bin/env bash

git clone https://github.com/kubernetes/kubernetes.git $GOPATH/src/k8s.io/kubernetes
cd $GOPATH/src/k8s.io/kubernetes
git checkout release-1.7
mkdir -p _output/bin
mkdir -p _output/dockerized/bin/linux/amd64

curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.7.2/kubernetes-client-linux-amd64.tar.gz
tar -xzvf kubernetes-client-linux-amd64.tar.gz
chmod +x kubernetes/client/bin/kubefed
chmod +x kubernetes/client/bin/kubectl
sudo cp kubernetes/client/bin/kubefed /usr/local/bin
sudo cp kubernetes/client/bin/kubectl /usr/local/bin
cp kubernetes/client/bin/kubefed _output/bin/kubefed
cp kubernetes/client/bin/kubectl _output/bin/kubectl

curl -LO https://dl.k8s.io/v1.7.2/kubernetes-server-linux-amd64.tar.gz
tar -xzvf kubernetes-server-linux-amd64.tar.gz
chmod +x kubernetes/server/bin/hyperkube
cp kubernetes/server/bin/hyperkube _output/bin/hyperkube
cp kubernetes/server/bin/hyperkube _output/dockerized/bin/linux/amd64/hyperkube

git clone https://github.com/nebril/kubernetes-dind-federation dind
pushd dind
git checkout cat-instead-of-mount
popd
dind/dind-up-cluster.sh
CLUSTER_NAME=dind2 IP_RANGE=172.128.0.0/16 APISERVER_ADDRESS=172.128.0.1 dind/dind-up-cluster.sh
kubectl config use-context dind
dind/dind-deploy-federation.sh
kubefed join dind2 --host-cluster-context=dind --context=federation

cd $GOPATH/src/github.com/kubernetes-helm/rudder-federation
python utils/populate-configmap.py > manifests/fed-credentials.yaml
