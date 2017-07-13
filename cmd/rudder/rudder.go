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

package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"k8s.io/kubernetes/federation/apis/federation"
	fedclient "k8s.io/kubernetes/federation/client/clientset_generated/federation_internalclientset"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	rest "k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
	clientcmdapi "k8s.io/kubernetes/pkg/client/unversioned/clientcmd/api"

	//todo: change to k8s.com/helm after rudder is merged
	"github.com/nebril/helm/pkg/kube"
	rudderAPI "github.com/nebril/helm/pkg/proto/hapi/rudder"
	"github.com/nebril/helm/pkg/rudder"
	"github.com/nebril/helm/pkg/version"

	"github.com/nebril/rudder-appcontroller/pkg/releaseutil"
)

var kubeClient *kube.Client
var clientset internalclientset.Interface

var grpcAddr = fmt.Sprintf("127.0.0.1:%d", rudder.GrpcPort)

func main() {
	var err error
	kubeClient = kube.New(nil)
	clientset, err = kubeClient.ClientSet()
	if err != nil {
		grpclog.Fatalf("Cannot initialize Kubernetes connection: %s", err)
	}

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	rudderAPI.RegisterReleaseModuleServiceServer(grpcServer, &ReleaseModuleServiceServer{})

	grpclog.Print("Federated Server starting")
	grpcServer.Serve(lis)
}

// ReleaseModuleServiceServer provides implementation for rudderAPI.ReleaseModuleServiceServer
type ReleaseModuleServiceServer struct{}

// Version is not yet implemented
func (r *ReleaseModuleServiceServer) Version(ctx context.Context, in *rudderAPI.VersionReleaseRequest) (*rudderAPI.VersionReleaseResponse, error) {
	grpclog.Print("version")
	return &rudderAPI.VersionReleaseResponse{
		Name:    "helm-rudder-native",
		Version: version.Version,
	}, nil
}

// InstallRelease creates a release using kubeClient.Create
func (r *ReleaseModuleServiceServer) InstallRelease(ctx context.Context, in *rudderAPI.InstallReleaseRequest) (*rudderAPI.InstallReleaseResponse, error) {
	grpclog.Print("install")

	federationClient, err := getFederationClient()

	if err != nil {
		grpclog.Printf("error getting federation client: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	clients, err := getFederatedClusterClients(federationClient)
	if err != nil {
		grpclog.Printf("error getting federated cluster clients: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	//TODO use federated
	federated, local, err := SplitManifestForFed(in.Release.Manifest)

	if err != nil {
		grpclog.Printf("error splitting manifests: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	err = createInFederation(federated, in)
	if err != nil {
		grpclog.Printf("error creating federated objects: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	for _, c := range clients {
		config, _ := c.ClientConfig()
		grpclog.Printf("installing in %s", config.Host)
		err := c.Create(in.Release.Namespace, bytes.NewBufferString(local), 500, false)
		if err != nil {
			grpclog.Printf("error when creating release: %v", err)
			return &rudderAPI.InstallReleaseResponse{}, err
		}
	}

	return &rudderAPI.InstallReleaseResponse{}, err
}

// DeleteRelease is not implemented
func (r *ReleaseModuleServiceServer) DeleteRelease(ctx context.Context, in *rudderAPI.DeleteReleaseRequest) (*rudderAPI.DeleteReleaseResponse, error) {
	grpclog.Print("delete")

	return &rudderAPI.DeleteReleaseResponse{}, nil
}

// RollbackRelease rolls back the release
func (r *ReleaseModuleServiceServer) RollbackRelease(ctx context.Context, in *rudderAPI.RollbackReleaseRequest) (*rudderAPI.RollbackReleaseResponse, error) {
	grpclog.Print("rollback")
	c := bytes.NewBufferString(in.Current.Manifest)
	t := bytes.NewBufferString(in.Target.Manifest)
	err := kubeClient.Update(in.Target.Namespace, c, t, in.Recreate, in.Timeout, in.Wait)
	return &rudderAPI.RollbackReleaseResponse{}, err
}

// UpgradeRelease upgrades manifests using kubernetes client
func (r *ReleaseModuleServiceServer) UpgradeRelease(ctx context.Context, in *rudderAPI.UpgradeReleaseRequest) (*rudderAPI.UpgradeReleaseResponse, error) {
	grpclog.Print("upgrade")
	c := bytes.NewBufferString(in.Current.Manifest)
	t := bytes.NewBufferString(in.Target.Manifest)
	err := kubeClient.Update(in.Target.Namespace, c, t, in.Recreate, in.Timeout, in.Wait)
	// upgrade response object should be changed to include status
	return &rudderAPI.UpgradeReleaseResponse{}, err
}

func (r *ReleaseModuleServiceServer) ReleaseStatus(ctx context.Context, in *rudderAPI.ReleaseStatusRequest) (*rudderAPI.ReleaseStatusResponse, error) {
	grpclog.Print("status")

	resp, err := kubeClient.Get(in.Release.Namespace, bytes.NewBufferString(in.Release.Manifest))
	in.Release.Info.Status.Resources = resp
	return &rudderAPI.ReleaseStatusResponse{
		Release: in.Release,
		Info:    in.Release.Info,
	}, err
}

func getFederatedClusterClients(fed *fedclient.Clientset) (clients []*kube.Client, err error) {
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

func SplitManifestForFed(manifest string) (fed string, local string, err error) {
	localKinds := map[string]bool{
		"PersistentVolumeClaim": true,
	}

	objects, err := releaseutil.SplitManifestsWithHeads(manifest)
	if err != nil {
		return
	}

	local = "---"
	fed = "---"

	for _, o := range objects {
		if localKinds[o.Kind] {
			local += "\n" + strings.Trim(o.Content, "- \t\n") + "\n---"
		} else {
			fed += "\n" + strings.Trim(o.Content, "- \t\n") + "\n---"
		}
	}

	return
}

func createInFederation(manifest string, req *rudderAPI.InstallReleaseRequest) error {
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
	//TODO: don't hardcode this xD
	Host: "https://172.28.0.5:31180",
	TLSClientConfig: rest.TLSClientConfig{
		CAData: []byte(`-----BEGIN CERTIFICATE-----
MIICyDCCAbCgAwIBAgIBADANBgkqhkiG9w0BAQsFADAVMRMwEQYDVQQDEwpmZWRl
cmF0aW9uMB4XDTE3MDcxMzEzMTE0NFoXDTI3MDcxMTEzMTE0NFowFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAPq2
2Rv8QI3pechpdgOqTDvCb6yQBHUt92+5xCnpKar8IB31Yao539In6NYoU45QwoDa
02IyXGzk9jYT+TSMxFel+tijiFthQ3SH9KCrjbEDf9xkh+P/MiSs9k7CAxHkGreN
cS/7a51IW561S18rCmTM+AhJpv9cpx3Rp5cYi3ygwTewvriuMkMC4I9HaVO1RTta
bw0HylFMHqSXMeITCpc4JWIDwX0wi2HjT4DX8z6kL4/IXlke+KrbJr2xXV3ks2Y6
e4uh2Q4W6kewnY7p1nkHQTRU8+IODrBi2bxfAFL4wAbQWoDDq21f5OvfsD79Zn9G
IS/swr362bsS5xD07HcCAwEAAaMjMCEwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB
/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBADRs+J2oSXMZkddT27lAu0qn4rky
SA5RircX5yjuCWvFuNXeM9OgrYomg8zxmh+XK9aonGG4AFmhNKfItbVF7sk9ZcI4
91SJ7E0Qqy0DKBAXeMtVAb4O3bb7TaFomSEzuHnGAdaCxj4Ano1NY57gnI7uPobM
eXdd8ZDeN9IIwfv1B/d8k2FD2kXZnE+gStB4UKpFL0flZyAkH7bRtmDaPILDa63q
g2tsLaQYTu7y3NsCTQmUxoQmkURAX1jmuzgDCiOSg6fBSotUG3a/53WXz9d029Mq
KY3fibRnXfLBOf8WlBzVmf7NLQUkJd0YI0yRK7Kw9koVKYbNXW07qt/EuRY=
-----END CERTIFICATE-----`),
		CertData: []byte(`-----BEGIN CERTIFICATE-----
MIICzjCCAbagAwIBAgIIb9FlqWhu10AwDQYJKoZIhvcNAQELBQAwFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjAeFw0xNzA3MTMxMzExNDRaFw0xODA3MTMxMzExNDVaMBAx
DjAMBgNVBAMTBWFkbWluMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
tt/y3g4iT3+hp4t4cwn0bBUTMun2IyjpVi3gq8LVpvygWKvHkpDzYAERDmoIFtSF
m4jI5vbdg9ekqLbSiw3dAnPZ3aA5Fvs/e1BqbsiyKbYB2chpoTCqat0UMAnD/w1S
pyNeLmo2Wx3837QB7ojvPyucExKtHPzJopEvE7XZiAbEXcsUngZXEfoL2JZlDB1z
RiGZhH4mDFNT6plVJuyG9BUp5BXroTjAxaAwUib6G3/I11jrGYaSheYOGNwBuEEh
68biKlaJyjuM78oeZJO5KShCQoyM3Hy0vWi14pVf7MKAQD6hdoHVG4rMphzJUYTU
LZjo6udY6jDKzE3Fs8uDswIDAQABoycwJTAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0l
BAwwCgYIKwYBBQUHAwIwDQYJKoZIhvcNAQELBQADggEBAK/59nXMFNj3QVvKnzum
6zymCWQxHrRKxSxzZNuPFBElHflnhquorgUk0rzs0QJ1e0CS1KIEDltkcr5C8Pey
YXls5jRNX4QxY4wNmfofjg6TIkVAzGEWqw0sN+Vw/Kbb1najObFD9nCiNiXd8EJ2
Pqaje0nJyU6k6vEluABnU7fzhjWId+X8jJaTU45+oRSiGrB3ceocWSnjnMJ9jM4M
EfCPjfnFsEiZvqL+JWbJSQa1l4Z5zJcKQG3C+UHhQixyM9gwxNfshVqt2lEJqO/9
w9L2TCQS3xhDDL2t8UgMI/50wLrE0ZzBiDCa2uFjtyHU7gcniuWKOCz+hgTAkSqs
1Pk=
-----END CERTIFICATE-----`),
		KeyData: []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAtt/y3g4iT3+hp4t4cwn0bBUTMun2IyjpVi3gq8LVpvygWKvH
kpDzYAERDmoIFtSFm4jI5vbdg9ekqLbSiw3dAnPZ3aA5Fvs/e1BqbsiyKbYB2chp
oTCqat0UMAnD/w1SpyNeLmo2Wx3837QB7ojvPyucExKtHPzJopEvE7XZiAbEXcsU
ngZXEfoL2JZlDB1zRiGZhH4mDFNT6plVJuyG9BUp5BXroTjAxaAwUib6G3/I11jr
GYaSheYOGNwBuEEh68biKlaJyjuM78oeZJO5KShCQoyM3Hy0vWi14pVf7MKAQD6h
doHVG4rMphzJUYTULZjo6udY6jDKzE3Fs8uDswIDAQABAoIBAA43lOMMiEBT9NZY
snGHGZh5fvebVsZe/Nz1Th0sVX3Y8AJUwHw1hqY1DwVm2uAjf4ua87t4/7mrPyLa
q72hw0fYh9yCA041FDdbBhs8wRUbEEPFH+knJmiObW5apAElIQLbbgv/t+AXkbw2
e1v3C1qG4mhdMFYrlOVtkhJfNd0sP9z6XzyZoUMwWFZfDqcmfKJZm/z4Vn2z+TX4
OF1yEX7zjd9fxxx0FyeiVRl9z1z7z5ig4EPVkG8E6JZcUY5g0BIFiPXZbWw7yAtZ
6smAZD2h/2rzIleWM5cT7eFisRyT+0HjnIz6WJ1oKcu77fau0JjBZR4rjmlbFqbi
IaMPyHkCgYEA2m2p1X/SO+2DnJFIIYIyKx5tES7w0MjZqtMJNiR/SmGArcKlkBhu
NIOzkNBXFfC276XhkZutheMycT9sUnyhnRhE4rTNYBnZJ8X6mVsoAn8NtnOGrSDR
3G4r/O7wMBXS3gusEfbcSDHES+N39+S/tOHeoVYh/Y6+msA+ROpCNK8CgYEA1lS1
j1x21rempM3XyLqMrkMGxGYEI97Pe9/E3Kb1WqslwMT0bZxtT+08hsuTp4gH+khn
k/EEzLG1MKhZ7rU/oNI4o/5IvBIZ7WwJMcJdaBRcUJJzF7hsoeE5W646SkFM/2q+
WjV9X+fgkfMLO7aXb2iRozNC1yqIPrhc2tEz6j0CgYEArT9f/ow0pv27bxq4eIN4
4URvw7pUnXVBWEGsw7ntEIUHeEqz4PfPqW1wpoLpH+jeYHRU1pYA6voKj1J7y205
Do4qTRqU7w1xdR+Npcdsk5ZMvRMilf07Fzh3QVYPQkR9DUt6voDrtYNrq7mO9RsF
hyXD3Hmh2ig3PC0Q9r5LptcCgYAOwK/qmUO4zdVTnLOQpn6OdCCgHiGE0o5XiXSE
d52FyygDF8t3TAAeM0cqRBL6whtCd/9hKILbEBRXsA7YpnMlv7KUXylkgJ52QCx1
11oUkuozxZDUfiZEEjufeuOaPtps7k0B6pKhqlVD1oXca1oLGhiEMkAUjWHpZ0lE
6od3RQKBgQC5eJBOe9gl4EyxpkSXrlfTHYzQN4awi2Mo+utMIKgTfdx/XmxNDVBJ
xhMGDhmKYW2ySns8gidjhfBGZjXrFIk6hMEHa72nQoFCTpCsLmYvULNOa6w92VkL
cHwYzbz0QU4FHloUiKdzJJkwNbFrTU15o3LDJbm8SfSd+g1WM9SQfg==
-----END RSA PRIVATE KEY-----`),
	},
}

func getFederationClient() (*fedclient.Clientset, error) {
	return fedclient.NewForConfig(federationConfig)
}
