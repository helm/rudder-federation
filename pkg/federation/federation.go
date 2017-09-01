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
	"os"
	"regexp"
	"strings"
	"text/template"

	"google.golang.org/grpc/grpclog"

	"github.com/ghodss/yaml"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientrest "k8s.io/client-go/rest"
	rest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubernetes/federation/apis/federation"
	fedclient "k8s.io/kubernetes/federation/client/clientset_generated/federation_internalclientset"
	"k8s.io/kubernetes/pkg/apis/extensions"

	//"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/kube"
	rudderAPI "k8s.io/helm/pkg/proto/hapi/rudder"

	"github.com/kubernetes-helm/rudder-federation/pkg/releaseutil"
)

func GetFederatedClusterClients(fed *fedclient.Clientset) (clients []*kube.Client, err error) {
	clusters, err := fed.Federation().Clusters().List(v1.ListOptions{})
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

	c := kube.New(clientconfig)
	c.Log = grpclog.Infof

	return c
}

func makeFedClient() *kube.Client {
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
				Username:              federationConfig.Username,
				Password:              federationConfig.Password,
			},
		},
	}

	clientconfig := clientcmd.NewDefaultClientConfig(config, &clientcmd.ConfigOverrides{})

	c := kube.New(clientconfig)
	c.Log = grpclog.Infof

	return c
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

	client := makeFedClient()

	return client.Create(req.Release.Namespace, bytes.NewBufferString(manifest), 500, false)
}

var federationConfig = &rest.Config{
	Host: "http://example.host",
}

// GetFederationClient uses federationConfig, but it can be overwritten by federation-auth secret within the same namespace
func GetFederationClient() (*fedclient.Clientset, error) {
	return fedclient.NewForConfig(federationConfig)
}

// GetAllClients returns federation clientset, helm federation client and helm clients for all federated clusters
func GetAllClients() (*fedclient.Clientset, *kube.Client, []*kube.Client, error) {
	fedClientset, err := GetFederationClient()
	if err != nil {
		return nil, nil, nil, err
	}

	fedClient := makeFedClient()

	clients, err := GetFederatedClusterClients(fedClientset)

	return fedClientset, fedClient, clients, err
}

func populateFederationConfig() error {
	kubeconfig, err := clientrest.InClusterConfig()

	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(kubeconfig)

	if err != nil {
		return err
	}

	namespace := os.Getenv("RUDDER_NAMESPACE")
	if namespace == "" {
		namespace = "kube-system"
	}

	grpclog.Infof("Taking federations credentials from %s namespace", namespace)

	cm, err := clientset.Core().ConfigMaps(namespace).Get("federation-credentials", v1.GetOptions{})

	if err != nil {
		return err
	}

	if cm.Data["type"] == "basic" {
		federationConfig.Username = cm.Data["username"]
		federationConfig.Password = cm.Data["password"]
		federationConfig.Host = cm.Data["host"]
	} else if cm.Data["type"] == "tls" {
		federationConfig.CAData = []byte(cm.Data["cadata"])
		federationConfig.CertData = []byte(cm.Data["certdata"])
		federationConfig.KeyData = []byte(cm.Data["keydata"])

		federationConfig.Host = cm.Data["host"]
	}

	return err
}

func init() {
	populateFederationConfig()
}

type Replace struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type ReplaceExtract struct {
	Replace []Replace `json:"replace"`
}

func GetReplacements(req *rudderAPI.InstallReleaseRequest) []Replace {
	raw := req.Release.Config.Raw
	extractor := ReplaceExtract{}
	err := yaml.Unmarshal([]byte(raw), &extractor)
	if err != nil {
		grpclog.Warningln("Error while unmarshalling raw config: ", err)
	}

	return extractor.Replace
}

type DeploymentExtractor struct {
	Namespace string `json:"fed-namespace"`
	Name      string `json:"fed-controller-name"`
}

func GetFederationControllerDeployment(req *rudderAPI.InstallReleaseRequest) (*extensions.Deployment, error) {
	raw := req.Release.Config.Raw
	extractor := DeploymentExtractor{
		Namespace: "federation-system",
		Name:      "federation-controller-manager",
	}
	err := yaml.Unmarshal([]byte(raw), &extractor)
	if err != nil {
		grpclog.Warningln("Error while unmarshalling raw config: ", err)
	}

	clientset, err := kube.New(nil).ClientSet()
	if err != nil {
		grpclog.Errorf("Cannot initialize Kubernetes connection: %s", err)
		return nil, err
	}

	dep, err := clientset.Extensions().Deployments(extractor.Namespace).Get(extractor.Name, v1.GetOptions{})
	if err != nil {
		grpclog.Errorf("Cannot get deployment %s from ns %s: %v", extractor.Name, extractor.Namespace, err)
	}
	return dep, err
}

func ReplaceWithFederationDeployment(manifest string, replacements []Replace, controller *extensions.Deployment) (string, error) {
	for _, rep := range replacements {
		var tpl bytes.Buffer
		t, err := template.New("").Parse(rep.To)
		if err != nil {
			grpclog.Errorf("Could not parse template %s: %v", rep.To, err)
			return manifest, err
		}
		err = t.Execute(&tpl, controller)
		if err != nil {
			grpclog.Errorf("Could not execute template %s: %v", rep.To, err)
			return manifest, err
		}

		reg, err := regexp.Compile(rep.From)
		if err != nil {
			grpclog.Errorf("Could not compile regex %s: %v", rep.From, err)
			return manifest, err
		}

		manifest = reg.ReplaceAllString(manifest, tpl.String())
	}
	return manifest, nil
}
