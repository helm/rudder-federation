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
	"k8s.io/helm/pkg/tiller"
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

	manifest := in.Release.Manifest
	replacements := fedlocal.GetReplacements(in)

	if len(replacements) > 0 {
		fedController, err := fedlocal.GetFederationControllerDeployment(in)
		if err != nil {
			grpclog.Infof("error getting federation controller")
			return &rudderAPI.InstallReleaseResponse{}, err
		}
		manifest, err = fedlocal.ReplaceWithFederationDeployment(manifest, replacements, fedController)
		if err != nil {
			grpclog.Infof("error replacing replacements")
			return &rudderAPI.InstallReleaseResponse{}, err
		}
	}

	federated, local, err := fedlocal.SplitManifestForFed(manifest)

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

// DeleteRelease deletes a release in federation and federated clusters
func (r *ReleaseModuleServiceServer) DeleteRelease(ctx context.Context, in *rudderAPI.DeleteReleaseRequest) (*rudderAPI.DeleteReleaseResponse, error) {
	grpclog.Info("delete")
	resp := &rudderAPI.DeleteReleaseResponse{
		Release: &releaseAPI.Release{},
	}

	federated, local, err := fedlocal.SplitManifestForFed(in.Release.Manifest)

	if err != nil {
		grpclog.Infof("error splitting manifests to delete: %v", err)
		return resp, err
	}

	_, fedClient, clients, err := fedlocal.GetAllClients()

	if err != nil {
		grpclog.Infof("Error getting clients: %v", err)
		return resp, err
	}

	errChan := make(chan error)
	doneChan := make(chan bool)

	deleter := func(client *kube.Client, manifest string) {
		errs := make([]error, 0)
		host := ""

		defer func() {
			grpclog.Infof("deletion in %v complete", host)
			doneChan <- true
			for _, err := range errs {
				go func(e error) {
					grpclog.Infof("error during deletion in %v: %s", host, err)
					errChan <- e
				}(err)
			}
		}()

		config, err := client.ClientConfig()
		if err != nil {
			errs = append(errs, err)
			return
		}
		host = config.Host
		grpclog.Infof("Deleting in %v", host)

		clientset, err := client.ClientSet()
		if err != nil {
			errs = append(errs, err)
			return
		}

		versionset, err := tiller.GetVersionSet(clientset.Discovery())
		if err != nil {
			errs = append(errs, err)
			return
		}

		release := *in.Release
		release.Manifest = manifest
		_, errs = tiller.DeleteRelease(&release, versionset, client)
	}

	go deleter(fedClient, federated)

	for _, client := range clients {
		go deleter(client, local)
	}

	//Waiting for all upgraders to finish (successful or not)
	grpclog.Infof("Waiting for deletions to finish")
	for i := 0; i < len(clients)+1; i++ {
		<-doneChan
	}

	select {
	case err = <-errChan:
		grpclog.Infof("Error while deleting: %v", err)
		return resp, err
	default:
	}
	grpclog.Infof("Finished deletion")
	return resp, err
}

// RollbackRelease rolls back the release
func (r *ReleaseModuleServiceServer) RollbackRelease(ctx context.Context, in *rudderAPI.RollbackReleaseRequest) (*rudderAPI.RollbackReleaseResponse, error) {
	grpclog.Info("rollback")

	err := updateRelease(in.Current.Manifest, in.Target.Manifest, in.Target.Namespace, in.Force, in.Recreate, in.Wait, in.Timeout)
	if err != nil {
		grpclog.Warningf("Error rolling back release: %v", err)
	}
	return &rudderAPI.RollbackReleaseResponse{}, err
}

// UpgradeRelease upgrades manifests using kubernetes client
func (r *ReleaseModuleServiceServer) UpgradeRelease(ctx context.Context, in *rudderAPI.UpgradeReleaseRequest) (*rudderAPI.UpgradeReleaseResponse, error) {
	grpclog.Info("upgrade")

	err := updateRelease(in.Current.Manifest, in.Target.Manifest, in.Target.Namespace, in.Force, in.Recreate, in.Wait, in.Timeout)
	if err != nil {
		grpclog.Warningf("Error updating release: %v", err)
	}
	return &rudderAPI.UpgradeReleaseResponse{}, err
}

func updateRelease(current, target, namespace string, force, recreate, wait bool, timeout int64) error {
	federatedCurrent, localCurrent, err := fedlocal.SplitManifestForFed(current)

	if err != nil {
		grpclog.Warningf("Error splitting manifest: %v", err)
		return err
	}

	federatedTarget, localTarget, err := fedlocal.SplitManifestForFed(target)

	if err != nil {
		grpclog.Warningf("Error splitting manifest: %v", err)
		return err
	}

	_, fedClient, clients, err := fedlocal.GetAllClients()

	if err != nil {
		grpclog.Warningf("Error getting clients: %v", err)
		return err
	}

	//We don't want errors to block goroutine
	errChan := make(chan error, len(clients))
	doneChan := make(chan bool)

	upgrader := func(client *kube.Client, current, target string) {
		config, _ := client.ClientConfig()
		grpclog.Infof("Updating in %v", config.Host)
		err := client.Update(namespace, bytes.NewBufferString(current), bytes.NewBufferString(target), force, recreate, timeout, wait)
		doneChan <- true
		if err != nil {
			grpclog.Warningf("Error updating in %s: %v", config.Host, err)
			errChan <- err
		}
	}

	go upgrader(fedClient, federatedCurrent, federatedTarget)

	for _, client := range clients {
		go upgrader(client, localCurrent, localTarget)
	}

	//Waiting for all upgraders to finish (successful or not)
	for i := 0; i < len(clients)+1; i++ {
		<-doneChan
	}

	select {
	case err = <-errChan:
		return err
	default:
	}

	return nil
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
