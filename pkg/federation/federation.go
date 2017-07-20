/*
Copyright 2017 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package federation

import (
	"bytes"
	"strings"

	"k8s.io/kubernetes/federation/apis/federation"
	fedclient "k8s.io/kubernetes/federation/client/clientset_generated/federation_internalclientset"
	"k8s.io/kubernetes/pkg/api"
	rest "k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
	clientcmdapi "k8s.io/kubernetes/pkg/client/unversioned/clientcmd/api"

	"k8s.io/client-go/kubernetes"
	clientrest "k8s.io/client-go/rest"

	"github.com/nebril/helm/pkg/kube"
	rudderAPI "github.com/nebril/helm/pkg/proto/hapi/rudder"

	"github.com/kubernetes-helm/rudder-federation/pkg/releaseutil"
)

func GetFederatedClusterClients(fed *fedclient.Clientset) (clients []*kube.Client, err error) {
	clusters, err := fed.Federation().Clusters().List(api.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters.Items {
		clients = append(clients, makeClient(cluster))
	}

	return clients, nil
}

func makeClient(cluster federation.Cluster) *kube.Client {
	config := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			cluster.Name: &clientcmdapi.Cluster{
				Server:                cluster.Spec.ServerAddressByClientCIDRs[0].ServerAddress,
				InsecureSkipTLSVerify: true,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			cluster.Name: &clientcmdapi.Context{
				Cluster: cluster.Name,
			},
		},
		CurrentContext: cluster.Name,
	}

	clientconfig := clientcmd.NewDefaultClientConfig(config, &clientcmd.ConfigOverrides{})

	return kube.New(clientconfig)
}

//Map all object kinds supported by Federation API. Source: https://kubernetes.io/docs/reference/federation/
var federationKinds = map[string]bool{
	"Cluster":            true,
	"ClusterList":        true,
	"ConfigMap":          true,
	"ConfigMapList":      true,
	"DaemonSet":          true,
	"DaemonSetList":      true,
	"Deployment":         true,
	"DeploymentList":     true,
	"DeploymentRollback": true,
	"Event":              true,
	"EventList":          true,
	"Ingress":            true,
	"IngressList":        true,
	"Namespace":          true,
	"NamespaceList":      true,
	"ReplicaSet":         true,
	"ReplicaSetList":     true,
	"Scale":              true,
	"Secret":             true,
	"SecretList":         true,
	"Service":            true,
	"ServiceList":        true,
}

func SplitManifestForFed(manifest string) (fed string, local string, err error) {

	objects, err := releaseutil.SplitManifestsWithHeads(manifest)
	if err != nil {
		return
	}

	fed = "---"
	local = "---"

	for _, o := range objects {
		if federationKinds[o.Kind] {
			fed += "\n" + strings.Trim(o.Content, "- \t\n") + "\n---"
		} else {
			local += "\n" + strings.Trim(o.Content, "- \t\n") + "\n---"
		}
	}

	return
}

func CreateInFederation(manifest string, req *rudderAPI.InstallReleaseRequest) error {
	config := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"federation": &clientcmdapi.Cluster{
				Server: federationConfig.Host,
				CertificateAuthorityData: federationConfig.CAData,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"federation": &clientcmdapi.Context{
				Cluster:  "federation",
				AuthInfo: "federation",
			},
		},
		CurrentContext: "federation",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"federation": &clientcmdapi.AuthInfo{
				ClientCertificateData: federationConfig.CertData,
				ClientKeyData:         federationConfig.KeyData,
			},
		},
	}

	clientconfig := clientcmd.NewDefaultClientConfig(config, &clientcmd.ConfigOverrides{})

	client := kube.New(clientconfig)

	return client.Create(req.Release.Namespace, bytes.NewBufferString(manifest), 500, false)
}

var federationConfig = &rest.Config{
	Host: "http://example.host",
}

// GetFederationClient uses federationConfig, but it can be overwritten by federation-auth secret within the same namespace
func GetFederationClient() (*fedclient.Clientset, error) {
	kubeconfig, err := clientrest.InClusterConfig()

	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(kubeconfig)

	if err != nil {
		return nil, err
	}

	cm, err := clientset.Core().ConfigMaps("kube-system").Get("federation-credentials")

	if err != nil {
		grpclog.Println("error getting federation-credentails config map, using default config")
		return fedclient.NewForConfig(federationConfig)
	}

	if cm.Data["type"] == "basic" {
		federationConfig.Username = cm.Data["username"]
		federationConfig.Password = cm.Data["password"]
		federationConfig.Host = cm.Data["host"]
		federationConfig.Insecure = true
	} else if cm.Data["type"] == "tls" {
		federationConfig.CAData = []byte(cm.Data["cadata"])
		federationConfig.CertData = []byte(cm.Data["certdata"])
		federationConfig.KeyData = []byte(cm.Data["keydata"])

		federationConfig.Host = cm.Data["host"]
	}

	return fedclient.NewForConfig(federationConfig)
}
