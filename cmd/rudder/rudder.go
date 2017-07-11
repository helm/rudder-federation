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
	_, local, err := SplitManifestForFed(in.Release.Manifest)

	if err != nil {
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	for _, c := range clients {
		config, _ := c.ClientConfig()
		grpclog.Printf("installing in %s", config.Host)
		err := c.Create(in.Release.Namespace, bytes.NewBufferString(local), 500, false)
		if err != nil {
			grpclog.Printf("error when creating release: %v", err)
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

var federationConfig = &rest.Config{
	//TODO: don't hardcode this xD
	Host: "https://172.28.0.4:31969",
	TLSClientConfig: rest.TLSClientConfig{
		CAData: []byte(`-----BEGIN CERTIFICATE-----
MIICyDCCAbCgAwIBAgIBADANBgkqhkiG9w0BAQsFADAVMRMwEQYDVQQDEwpmZWRl
cmF0aW9uMB4XDTE3MDcwNjExMjgyNFoXDTI3MDcwNDExMjgyNFowFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAJck
ny1esJxPTTr2naRuoBg3g0bT1fN/hrKMN127nmuKirbN0BmyBKIHdV6cNvtoZ/tM
mxYu346VhfJW3ecI4vY/A0Dfg5nU6AeA8qR2Yft2+Ib53lhbdbhDLcG8+mNV6lKl
LYohlmn032s6e25gqvKMBDe91mm1nwZZFw7zFbR5ieZEj8mMgHJpqxlYhcwLZJxG
6fvTZmVysE/rdG9bZoR4bEUILJAEouMh1Gjn5UjhxWEswmOcrIhEew7be5sTXT3G
sQN89vNloOgv4ZRO9Vqlya7tf4fKjxbYf1OUTUOFPORmA+usox2q00u0OgaSwrT4
cCDYWa7lLEk6IPpEnjcCAwEAAaMjMCEwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB
/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAFWlrrSltyT+/m8V1tvqcP0QVNaP
tpP0WC48L2NRXGncbVAPnDhP3Y0k8B67j1sEgy9R97u1flTn/2DP5Bd7hMvsMIR7
meadejLumR5Y2qOjn8LdNwlvl3EJEPwx+GwpaQ0KhY03fasPMJJWBYWos+ok1BKE
DKw8c9Skos+XlsEk9CSIGL/2qhJePC1Ka4ZOpBZiY1ISULA+p2IPZGMl4boP7Hma
4FFE1fO2+YN1FZxRIzXeha0Ppm9c1NzhY4Mjp5qcX7xu1u+OC0cCiWD55epliZ9Z
9eUnOcSp691rCg68MewcQQ8+NeKan8fTqPWli9JLP6J510sWEFCXWupF328=
-----END CERTIFICATE-----`),
		CertData: []byte(`-----BEGIN CERTIFICATE-----
MIICzjCCAbagAwIBAgIILeSHXp4lQmMwDQYJKoZIhvcNAQELBQAwFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjAeFw0xNzA3MDYxMTI4MjRaFw0xODA3MDYxMTI4MjVaMBAx
DjAMBgNVBAMTBWFkbWluMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
4STo145Pd7ZzumcyT9EWh7SLUpalz/jVZV90ZyzJhSB98CHWbyvSpU50dXa4C11O
Y3eDR0Iu5JNRA51iPoQZ2hBHoee6+ccYFu+VpwniQo2xayT18DcL973Udrn+rybK
bcZoTfujO7ebJhuzSktS+P6JfRJcojLAHtGxwZZA2pCdM9v8IBBedlF/8ToduyJt
m05QUKZ4hnRzioVZ3iV4pS8znGOtClhCNd8eJ7ULZER1PWfY+E27ROftoGmKAz3s
ID7/z4rxvjrwZGKdevDBMe5HWtQdVT33tLIDhFNF7uYPnO6sac7g9kAqdwytp0nr
Sa4H4GPeKRC6wRtR27Gs4QIDAQABoycwJTAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0l
BAwwCgYIKwYBBQUHAwIwDQYJKoZIhvcNAQELBQADggEBAJSgfK45/pLzZG41P6N0
8MRxEl+MnF2zNiEX+vgplvyb5bAEspa8NFUaEd5D97od1DfliPT1jWy77TkTs3lf
QAnHOHKm15Z8iN4+eN7jpIRH80IEi+kkkR/gBseN/sir/fgO24BlMYJyqsCQdXLu
R95mEVP2QzCcTdxhg7AlwmFmSwEvHewDFEIifu8+7POeUGn+R5D4sEmffr5EoPLB
8Dslmj1tbfJyzY9ztKizFWD8Kc6HvX/4BIsQTiZZ+dYw3dugBEBW+mi68U5FQnz7
3RVkB7QBTUBMhB/7jsl73j2+gRO28HoRHlbHdiZhZXjBy++pz0h8QwcSUbxkNAyS
WK0=
-----END CERTIFICATE-----`),
		KeyData: []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA4STo145Pd7ZzumcyT9EWh7SLUpalz/jVZV90ZyzJhSB98CHW
byvSpU50dXa4C11OY3eDR0Iu5JNRA51iPoQZ2hBHoee6+ccYFu+VpwniQo2xayT1
8DcL973Udrn+rybKbcZoTfujO7ebJhuzSktS+P6JfRJcojLAHtGxwZZA2pCdM9v8
IBBedlF/8ToduyJtm05QUKZ4hnRzioVZ3iV4pS8znGOtClhCNd8eJ7ULZER1PWfY
+E27ROftoGmKAz3sID7/z4rxvjrwZGKdevDBMe5HWtQdVT33tLIDhFNF7uYPnO6s
ac7g9kAqdwytp0nrSa4H4GPeKRC6wRtR27Gs4QIDAQABAoIBAQCJBDlf1Y+vPy42
5s7LnGelts6DOIM4iir0Qp9Imw0ZI6kBFta1WWociB5/zfw7jlFCX11ZZIG9QZow
JPvBKAvDldzCP5CeqfeTHcNpoK496pVqq1exFQ8HelNu3cqNNYJERFb9/oJcuWSG
UJ1QPX8FYYKyxxXw8AnTt7ICKjrVx8Bhojyp8G9JBRdQH0mSoGG3nqrBO/qPGweO
0bj4BquRrHHxMuDe1GLq+MM2HRj5r5w/2IcpObvXJEl+1vTwNlp07p+G+UXk8jCo
YpfFHj/OTEHd1x2uSRllHpLp9qflflTQJQtuIHRxt6KvIRUlnKa4QTiXQEbyODOs
uuZ+Mk5BAoGBAOvYZkP/14jin5T6PtalPVypWj62zZegNYNe8xx2zkJ2Q1pRAydm
PUYauUjGlf3tDFKH+vS/K3+OlAYOz5nnLAKdpPH1TWOUpkVNQyfFXVRgXjLhJsg5
OfzNx9La/EMWfj32kGHuJRplmgZd5hrEkQJ08Hvl+c38bPqfzHSxebLdAoGBAPRi
ZpugR3X7SfxJiK6D//8Z86SJWL0W44UfHGU9PKQlD+yDmUz3b8a7pA+Qnfef4kSN
FfbTbnQwviQxFpArZvRZcE3zjOJB+I6th1ZwjMuNyCblZpo3ULkcHMtSKj+kOIME
0A6kJwzsviTgXDtK8vUSaSzTW566haxui8TK9hfVAoGATpV7ddrwqVbBz7UWbRT/
/jkbrdvhY01pp01i+jAICBM53AU0ZNNnRU2wQTSSU9rBiVpv3083ojgS0HXs7J4f
hvuaM1kGIVEtmdflsYHM2EmH+bIV5w9SaA71LyfyeDQtel4Gu+rLCCGkkcyF2JN4
sfXfD5mQg/dBJL1MNfHQ2C0CgYARKR+/ad/avwyQ9LDuYEKHrVDYivR6QrMzU93w
lf4+IIQfvZX0O6PTtrVsimEtVELVQXr7XBlze0C+1duZwBJ4shcawjFwaeWET1cj
kL+yQ4B8irtLtPqsJPc4p8pjsapuONZLUOeVFsK7YC3Z1Ad/gg10olraqIpec1zJ
Mt9ZCQKBgQCZkKaTyVpW5LnfojVZ9WJE/kfjD3zreQGUFUpkT5TnRzScKrsegpZb
4MFusW7KjU4Zm0ICG1RZXUTEixd+la9YAw7dc8dAs+y8BWm89q4qdmdz1+E1Cu+H
QFrUHjqx0gH44r5/Lc9ID0c79ksd2eA+Cb/+VZdlnslwVBahwjxTRw==
-----END RSA PRIVATE KEY-----`),
	},
}

func getFederationClient() (*fedclient.Clientset, error) {
	return fedclient.NewForConfig(federationConfig)
}
