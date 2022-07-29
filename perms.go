/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"

	"github.com/clbanning/mxj"
	"github.com/karrick/godirwalk"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	log "github.com/sirupsen/logrus"
	vmSchema "kubevirt.io/api/core/v1"
	"kubevirt.io/kubevirt/pkg/hooks"
	hooksInfo "kubevirt.io/kubevirt/pkg/hooks/info"
	hooksV1alpha1 "kubevirt.io/kubevirt/pkg/hooks/v1alpha1"
	hooksV1alpha2 "kubevirt.io/kubevirt/pkg/hooks/v1alpha2"

	// vendor hack
	_ "kubevirt.io/kubevirt/pkg/cloud-init"
)

const (
	uidQemu      = 107
	gidQemu      = 107
	insecureMode = 0666
	hookName     = "disk-permission"
)

type infoServer struct {
	Version string
}

func (s infoServer) Info(ctx context.Context, params *hooksInfo.InfoParams) (*hooksInfo.InfoResult, error) {
	log.Info("permission info method has been called")

	return &hooksInfo.InfoResult{
		Name: "permission-hook",
		Versions: []string{
			s.Version,
		},
		HookPoints: []*hooksInfo.HookPoint{
			&hooksInfo.HookPoint{
				Name:     hooksInfo.OnDefineDomainHookPointName,
				Priority: 1,
			},
		},
	}, nil
}

type v1alpha1Server struct{}
type v1alpha2Server struct{}

func (s v1alpha2Server) OnDefineDomain(ctx context.Context, params *hooksV1alpha2.OnDefineDomainParams) (*hooksV1alpha2.OnDefineDomainResult, error) {
	log.WithField("hook", hookName).Info("OnDefineDomain called")
	newDomainXML, err := onDefineDomain(params.GetVmi(), params.GetDomainXML())
	if err != nil {
		return nil, err
	}
	return &hooksV1alpha2.OnDefineDomainResult{
		DomainXML: newDomainXML,
	}, nil
}
func (s v1alpha2Server) PreCloudInitIso(_ context.Context, params *hooksV1alpha2.PreCloudInitIsoParams) (*hooksV1alpha2.PreCloudInitIsoResult, error) {
	return &hooksV1alpha2.PreCloudInitIsoResult{
		CloudInitData: params.GetCloudInitData(),
	}, nil
}

func (s v1alpha1Server) OnDefineDomain(ctx context.Context, params *hooksV1alpha1.OnDefineDomainParams) (*hooksV1alpha1.OnDefineDomainResult, error) {
	log.WithField("hook", hookName).Info("OnDefineDomain called")
	newDomainXML, err := onDefineDomain(params.GetVmi(), params.GetDomainXML())
	if err != nil {
		return nil, err
	}
	return &hooksV1alpha1.OnDefineDomainResult{
		DomainXML: newDomainXML,
	}, nil
}

func onDefineDomain(vmiJSON []byte, domainXML []byte) ([]byte, error) {
	vmiSpec := vmSchema.VirtualMachineInstance{}
	err := json.Unmarshal(vmiJSON, &vmiSpec)
	if err != nil {
		log.WithError(err).Fatalf("Failed to unmarshal given VMI spec: %s", vmiJSON)
	}

	disk_root := "/var/run/kubevirt-private/vmi-disks"
	l := log.WithField("hook", hookName)
	l.Info("looking for disk images")
	_ = godirwalk.Walk(disk_root, &godirwalk.Options{
		Unsorted: true,
		Callback: func(path string, de *godirwalk.Dirent) error {
			if de.IsDir() || de.IsSymlink() {
				return nil
			}
			if !strings.HasSuffix(path, "img") {
				return nil
			}
			l.WithField("file", path).Info("is a candidate")

			if err := os.Chmod(path, insecureMode); err != nil {
				l.WithError(err).WithField("path", path).Error("failed to change file permissions")
				return err
			}
			if err := os.Chown(path, uidQemu, gidQemu); err != nil {
				l.WithError(err).WithField("path", path).Error("failed to change file owner")
			}
			l.WithFields(log.Fields{
				"uid":  uidQemu,
				"gid":  gidQemu,
				"path": path,
				"mode": insecureMode,
			}).Info("set ownership/permission on file")

			return nil
		},
		ErrorCallback: func(s string, e error) godirwalk.ErrorAction {
			l.WithError(e).WithField("path", s).Error("Failed walking path")
			return godirwalk.SkipNode
		},
	})

	m, merr := mxj.NewMapXml(domainXML)
	if merr != nil || m == nil {
		log.WithError(merr).Fatalf("Failed to unmarshal given domain spec: %s", domainXML)
	}

	for k, v := range m {
		log.WithFields(log.Fields{
			"key":   k,
			"value": v,
		}).Info()
	}

	return domainXML, nil
}

func ensureFinalPathExists(path string, m mxj.Map) ([]byte, error) {
	pathSplitted := strings.Split(path, ".")
	var vp = pathSplitted[0]
	for _, p := range pathSplitted[1:] {
		if !m.Exists(vp) {
			err := m.SetValueForPath(map[string]interface{}{}, vp)
			if err != nil {
				return nil, err
			}
		}
		vp = vp + "." + p
	}
	return nil, nil
}

func main() {
	log.Info("starting permission-hook-sidecar")

	var version string
	pflag.StringVar(&version, "version", "v1alpha2", "hook version to use")
	pflag.Parse()

	socketPath := hooks.HookSocketsSharedDirectory + "/permission-hook.sock"
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Error("Check whether given directory exists and socket name is not already taken by other file")
		log.WithError(err).Fatalf("Failed to initialized socket on path: %s", socket)
	}
	defer os.Remove(socketPath)

	server := grpc.NewServer([]grpc.ServerOption{}...)

	//hooksV1alpha1.Version,
	hooksInfo.RegisterInfoServer(server, infoServer{Version: version})
	hooksV1alpha1.RegisterCallbacksServer(server, v1alpha1Server{})
	hooksV1alpha2.RegisterCallbacksServer(server, v1alpha2Server{})
	log.Infof("Starting hook server exposing 'info' and 'v1alpha1' services on socket %s", socketPath)
	server.Serve(socket)
}
