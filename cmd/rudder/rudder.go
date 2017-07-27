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
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"

	"k8s.io/helm/pkg/kube"
	releaseAPI "k8s.io/helm/pkg/proto/hapi/release"
	rudderAPI "k8s.io/helm/pkg/proto/hapi/rudder"
	"k8s.io/helm/pkg/rudder"
	//"k8s.io/helm/pkg/tiller"
	"k8s.io/helm/pkg/version"

	fedlocal "github.com/kubernetes-helm/rudder-federation/pkg/federation"
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

	grpclog.Info("Federation Rudder started")
	grpcServer.Serve(lis)
}

// ReleaseModuleServiceServer provides implementation for rudderAPI.ReleaseModuleServiceServer
type ReleaseModuleServiceServer struct{}

// Version is not yet implemented
func (r *ReleaseModuleServiceServer) Version(ctx context.Context, in *rudderAPI.VersionReleaseRequest) (*rudderAPI.VersionReleaseResponse, error) {
	grpclog.Info("version")
	return &rudderAPI.VersionReleaseResponse{
		Name:    "helm-rudder-native",
		Version: version.Version,
	}, nil
}

// InstallRelease creates a release in federation and federated clusters
func (r *ReleaseModuleServiceServer) InstallRelease(ctx context.Context, in *rudderAPI.InstallReleaseRequest) (*rudderAPI.InstallReleaseResponse, error) {
	grpclog.Info("install")

	federated, local, err := fedlocal.SplitManifestForFed(in.Release.Manifest)

	if err != nil {
		grpclog.Infof("error splitting manifests: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	_, _, clients, err := fedlocal.GetAllClients()

	if err != nil {
		grpclog.Infof("error getting clients: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	err = fedlocal.CreateInFederation(federated, in)
	if err != nil {
		grpclog.Infof("error creating federated objects: %v", err)
		return &rudderAPI.InstallReleaseResponse{}, err
	}

	for _, c := range clients {
		config, _ := c.ClientConfig()
		grpclog.Infof("installing in %s", config.Host)
		err := c.Create(in.Release.Namespace, bytes.NewBufferString(local), 500, false)
		if err != nil {
			grpclog.Infof("error when creating release: %v", err)
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
	grpclog.Info("rollback")
	c := bytes.NewBufferString(in.Current.Manifest)
	t := bytes.NewBufferString(in.Target.Manifest)
	err := kubeClient.Update(in.Target.Namespace, c, t, in.Force, in.Recreate, in.Timeout, in.Wait)
	return &rudderAPI.RollbackReleaseResponse{}, err
}

// UpgradeRelease upgrades manifests using kubernetes client
func (r *ReleaseModuleServiceServer) UpgradeRelease(ctx context.Context, in *rudderAPI.UpgradeReleaseRequest) (*rudderAPI.UpgradeReleaseResponse, error) {
	grpclog.Info("upgrade")
	federatedCurrent, localCurrent, err := fedlocal.SplitManifestForFed(in.Current.Manifest)

	if err != nil {
		grpclog.Infof("error splitting current manifests: %v", err)
		return &rudderAPI.UpgradeReleaseResponse{}, err
	}

	federatedTarget, localTarget, err := fedlocal.SplitManifestForFed(in.Target.Manifest)

	if err != nil {
		grpclog.Infof("error splitting target manifests: %v", err)
		return &rudderAPI.UpgradeReleaseResponse{}, err
	}

	_, fedClient, clients, err := fedlocal.GetAllClients()

	if err != nil {
		grpclog.Infof("Error getting clients: %v", err)
		return &rudderAPI.UpgradeReleaseResponse{}, err
	}

	errChan := make(chan error)
	doneChan := make(chan bool)

	upgrader := func(client *kube.Client, current, target *bytes.Buffer) {
		config, _ := client.ClientConfig()
		grpclog.Infof("Upgrading in %v", config.Host)
		err := kubeClient.Update(in.Target.Namespace, current, target, in.Force, in.Recreate, in.Timeout, in.Wait)
		doneChan <- true
		if err != nil {
			errChan <- err
		}
	}

	go upgrader(fedClient, bytes.NewBufferString(federatedCurrent), bytes.NewBufferString(federatedTarget))

	c := bytes.NewBufferString(localCurrent)
	t := bytes.NewBufferString(localTarget)

	for _, client := range clients {
		go upgrader(client, c, t)
	}

	//Waiting for all upgraders to finish (successful or not)
	for i := 0; i < len(clients)+1; i++ {
		<-doneChan
	}

	select {
	case err = <-errChan:
		return &rudderAPI.UpgradeReleaseResponse{}, err
	default:
	}

	return &rudderAPI.UpgradeReleaseResponse{}, err
}

func (r *ReleaseModuleServiceServer) ReleaseStatus(ctx context.Context, in *rudderAPI.ReleaseStatusRequest) (*rudderAPI.ReleaseStatusResponse, error) {
	grpclog.Info("status")

	federated, local, err := fedlocal.SplitManifestForFed(in.Release.Manifest)

	if err != nil {
		grpclog.Infof("error splitting manifests: %v", err)
		return &rudderAPI.ReleaseStatusResponse{}, err
	}

	_, fedClient, clients, err := fedlocal.GetAllClients()
	if err != nil {
		grpclog.Infof("Error getting clients: %v", err)
		return &rudderAPI.ReleaseStatusResponse{}, err
	}

	responses := make([]string, 0, len(clients)+1)
	resps := make(chan string)

	fedResponse, err := fedClient.Get(in.Release.Namespace, bytes.NewBufferString(federated))
	if err != nil {
		grpclog.Infof("Error getting response from federation: %v", err)
		return &rudderAPI.ReleaseStatusResponse{}, err
	}
	fedResponse = "Federation resources:\n" + fedResponse
	go func() { resps <- fedResponse }()

	errchan := make(chan error)
	for _, client := range clients {
		go func(client *kube.Client) {
			var resp string
			resp, err := client.Get(in.Release.Namespace, bytes.NewBufferString(local))
			config, _ := client.ClientConfig()
			resp = config.Host + " resources:\n" + resp
			resps <- resp
			if err != nil {
				errchan <- err
			}
		}(client)
	}

	for i := 0; i < len(clients)+1; i++ {
		responses = append(responses, <-resps)
	}

	select {
	case err = <-errchan:
		grpclog.Infof("Error getting response from federated cluster: %v", err)
		return &rudderAPI.ReleaseStatusResponse{}, err
	default:
	}

	separator := "#########\n"
	finalResponse := strings.Join(responses, separator)

	in.Release.Info.Status.Resources = finalResponse
	return &rudderAPI.ReleaseStatusResponse{
		Release: in.Release,
		Info:    in.Release.Info,
	}, err
}
