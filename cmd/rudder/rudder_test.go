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
	"testing"

	"k8s.io/kubernetes/pkg/api"
)

func TestGetFederationClient(t *testing.T) {
	c, err := getFederationClient()

	if err != nil {
		t.Fatalf("error: ", err)
	}

	clusters, err := c.Federation().Clusters().List(api.ListOptions{})

	if err != nil {
		t.Fatalf("error: ", err)
	}

	fmt.Printf("%v", clusters)
}

func TestGetFederatedClients(t *testing.T) {
	c, err := getFederationClient()

	if err != nil {
		t.Fatalf("error: ", err)
	}

	clients, err := getFederatedClusterClients(c)

	if err != nil {
		t.Fatalf("error: ", err)
	}

	if len(clients) != 1 {
		t.Fatalf("Client count wrong")
	}

	service := []byte(`apiVersion: v1
kind: Service
metadata:
  creationTimestamp: 2017-06-15T13:42:07Z
  labels:
    app: federated-cluster
  name: federation-apiserver
  namespace: federation-system
  resourceVersion: "104431"
  selfLink: /api/v1/namespaces/federation-system/services/federation-apiserver
  uid: 6cb6e809-51d0-11e7-8a74-0242ac1c0003
spec:
  clusterIP: 10.0.0.22
  ports:
  - name: https
    nodePort: 30494
    port: 443
    protocol: TCP
    targetPort: 443
  selector:
    app: federated-cluster
    module: federation-apiserver
  sessionAffinity: None
  type: NodePort
status:
  loadBalancer: {}`)

	s, err := clients[0].Get("federation-system", bytes.NewReader(service))
	if err != nil {
		t.Fatalf("error: ", err)
	}
	fmt.Println(s)
}
