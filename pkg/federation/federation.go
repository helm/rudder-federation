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
	//TODO: don't hardcode this xD
	Host: "https://172.28.0.7:32480",
	TLSClientConfig: rest.TLSClientConfig{
		CAData: []byte(`-----BEGIN CERTIFICATE-----
MIICyDCCAbCgAwIBAgIBADANBgkqhkiG9w0BAQsFADAVMRMwEQYDVQQDEwpmZWRl
cmF0aW9uMB4XDTE3MDcxMzE1NTM1N1oXDTI3MDcxMTE1NTM1N1owFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMW0
cD9boZJ7mpJtJG3wBeja1Zc/EGMKwtqy1ATNdJnDlWZ2keWVkl/sEznCXsu5vZA7
tiGswmVT/T7doupf8Vnp74Ld1UjjwdXiY9O0q7WCliBgwNHvI1ZOk2698rnMHvMY
uGzKl9ytfP93k/Ci5rE9ddbdHGxlFTTMJU4DazXm7rjE2RcT75h2qIu00fZkMdwZ
8Fz8n3wi3iOuZ+by/T5QrlRkAQCe7dOGw/xAjJV4AHSQPdR/QNgr8V1QhvRCBJEg
FtwcdcEwcKO9loIwL0IAMv3fyNhn7w6XrVdYHOmt4bB6i1yNbrunCHrWwy8JBYYf
h1KRKgfgubC/JjK5KusCAwEAAaMjMCEwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB
/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJkHHwmgqh+JE21Bin71xATO5lMp
sMGCQHlkroIJZqQ2C55W1sl+AVUsFYcxK5Q/4A9OFNc6zdcKzYjLxuq3YlrqhJ0k
GmXHC9/+vBjFTih9Fkr4NzgMhOy/38BjtX3W9QBQa/ZcCgDG07D03kH1yn4kMJOY
O6GUHLJ5hiwBtG4FkrIwX14RIGVlAqylQWB5dTqRiaDC8exGDd7L/nk725Xa+5WC
fgWUJzEsy3ysLvJKfA10k6EHKYHHYiqxvzJ1DO6+vK+J97nDeljUTJ8gH8sLqTF1
y5oCcDYbOsN6mu5cOTsAyjKXYHUlZQ/Nxv6M9QjN+4RLqQAn0t0C74491Gg=
-----END CERTIFICATE-----`),
		CertData: []byte(`-----BEGIN CERTIFICATE-----
MIICzjCCAbagAwIBAgIIcbkizghMCRMwDQYJKoZIhvcNAQELBQAwFTETMBEGA1UE
AxMKZmVkZXJhdGlvbjAeFw0xNzA3MTMxNTUzNTdaFw0xODA3MTMxNTUzNThaMBAx
DjAMBgNVBAMTBWFkbWluMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
x939de+ywBSDBkkdPh9io4LgTmDWs3T9xauiSLz4Kmt/llfJuJGJ2i7t0wp9Y3r/
qj8W9IaM64DklZqjb9K0TxyDkrxqmyoySvK+jHcxa5vTj77QEEQDCPu1DJp3NSe/
AmO08Npln6aFdC88QczFQ9CKzdtkDA/9O3uKTrhEZ9YmNmtOuSjDKZHUL+kJwn7o
2lM3OZim4OoDP8u1xphhfl3lAEgXpPxEqUoPvnFYbvCuEHA9bPFiDxIr2MwA9AL4
hgPiln6QMlJ+NcEGkYJs9SSp1NsURqXpSQosdbIgZzeSU0O9xk/XJAlIUyYOKQ8y
GyIV+EvLbyyTgE4bSr8Y8QIDAQABoycwJTAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0l
BAwwCgYIKwYBBQUHAwIwDQYJKoZIhvcNAQELBQADggEBABRa7/flJ5UWKDm1te/w
Sf98Dfmo1uxZ/TI6tHPZCVBgGTaHnUrhqBmBY0cOQmaiv5uZkxhUzfeEl9FjDsVE
7DmCTZw3smEaeVgdzDMT++FYgaKyeFUvrZ3B/CocnrYGb0MAcCYgexTXzXwnUdbm
/0yVoRDGQrCBp7+1vS50WbbYXDjSvqPw+FK9HNf3ZN7JfAKyC5CfU1w6tFmzZVdw
VYrcy2GOA3GSX2z9ve14CzP3z+raFjmyQM0L3wwLZpo5iI1sKM8/TJ/bORy6SvKi
vqWtWEsNidCVmI8isOPTWTMfR+c1FhjKKcZwrCQ2FAFkLsjkcVDBB7OfHi2ZAQCj
TFM=
-----END CERTIFICATE-----`),
		KeyData: []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAx939de+ywBSDBkkdPh9io4LgTmDWs3T9xauiSLz4Kmt/llfJ
uJGJ2i7t0wp9Y3r/qj8W9IaM64DklZqjb9K0TxyDkrxqmyoySvK+jHcxa5vTj77Q
EEQDCPu1DJp3NSe/AmO08Npln6aFdC88QczFQ9CKzdtkDA/9O3uKTrhEZ9YmNmtO
uSjDKZHUL+kJwn7o2lM3OZim4OoDP8u1xphhfl3lAEgXpPxEqUoPvnFYbvCuEHA9
bPFiDxIr2MwA9AL4hgPiln6QMlJ+NcEGkYJs9SSp1NsURqXpSQosdbIgZzeSU0O9
xk/XJAlIUyYOKQ8yGyIV+EvLbyyTgE4bSr8Y8QIDAQABAoIBAEnQhqdj21RtPua3
YgFrffZ9g3Hu+dxVPNQgS1Mp101zXi+cRHfR6GOYKWVj3mM7LekmW8f7WVgwxjsq
XWoTG1yMX1t2ErkzoFdlx1IBQ1NGvZ+9DQK025oZvAfnlFchUr4DLPQ29Ik96tO6
DjsO8VvWTS0x9YUu8othWPcxvrHKEkJD0Q1BKNJOSQwunxPk46pi/RzHl3keIDc3
hAM8m5VsKMwc99b8UnVDcC8n9XFW/dAuDKFvGrFZZ5j3s1tdyES+aO8ziFmtwE9p
e9N011q1gf0ajnvYSyrkzddVCpafKJHTu8wswqGKu0fbDyPfPAMsywT3VHJAAFsH
ETXoSUECgYEA5TQ/mqUgFCQ/6racoKPoABNTk07efmMzBjtQSfbiZkx1M0pppXX0
bxrU/PiNPiIGbgwN4u/lH7/LpBGXqaPaA8WO1iCto4RUSGxXUfdU5NgFf/zCvKBf
l1LCf6qbWo3UKmTRqFoj5AjOCQyRneNSZKK05BPhwmrG82mfa+jEhXUCgYEA3zu6
ig5XZSiHZRx69Mpw7BW/Cu/hle6St5PSPk1bM+hBTdIRriz0sjabmYkty5h4iaPi
HLnC5bHZlGch7x7nC6fOYihDKIgFd0QKxsjLDa6QdFG43BZmvOFsQ0cv97DBSzyg
OTcc29AqD9J26zpfxUeYfn3hHVmkFdbjsXzeyg0CgYAbhwXojdJneN8QUnRHOsg/
UhLki0FfjoxvQCppZ7RTMvWUfmhnzd3YhjF0XGmiP7Xj+6CjU8qB4KgVgWNkpWAm
udBo2S3hiKASvqhSGNFiVqt6bqH4w44Xf4IKkTPtUUFdAhTIEmNjHMeaAJ9whf+8
RGpTRiwEDIzuaQ4TiLYpjQKBgCnhI7rYu+6fbt86O5sHC65O2htsK28cZewI0G2d
x5lyXiYCXgzGJFX2xrRENxI2FY8E7tuiwfyjpAUiYAxjSMc4ARELKqZE9nmMi1UF
wIpdkH4yArNPhJC03cG1bjtSrsC1q/1v6HsYj3uOaX7x4Zu6NdKtPPlrosvyF59p
pMZVAoGANG0Wd9clNol7Mpw+/0+l4G9HLn2mNW9Qaulrbyb0XbgL4jJZjayuFYLx
7RWQVdezvnr1Vcwy76btJsUWnvp25b6Zo839FYd6p4L/2unqYv7HrO2JwSVHQMBI
yDxLguFrcPuK37DCxDVRU42jFTXLDji0tbrXbEXfKTNKpxvQaKY=
-----END RSA PRIVATE KEY-----`),
	},
}

func GetFederationClient() (*fedclient.Clientset, error) {
	return fedclient.NewForConfig(federationConfig)
}
