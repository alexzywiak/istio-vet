/*
Portions Copyright 2017 Istio Authors
Portions Copyright 2017 Aspen Mesh Authors.

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

// Package util provides common constants and helper functions for vetters.
package util

import (
	"errors"
	"fmt"
	"strings"

	apiv1 "github.com/aspenmesh/istio-vet/api/v1"
	"github.com/cnf/structhash"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	meshv1alpha1 "istio.io/api/mesh/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/listers/core/v1"
)

// Constants related to Istio
const (
	IstioNamespace                = "istio-system"
	IstioProxyContainerName       = "istio-proxy"
	IstioMixerDeploymentName      = "istio-mixer"
	IstioMixerContainerName       = "mixer"
	IstioPilotDeploymentName      = "istio-pilot"
	IstioPilotContainerName       = "discovery"
	IstioInitContainerName        = "istio-init"
	IstioConfigMap                = "istio"
	IstioConfigMapKey             = "mesh"
	IstioAuthPolicy               = "authPolicy: MUTUAL_TLS"
	IstioInitializerPodAnnotation = "sidecar.istio.io/status"
	IstioInitializerConfigMap     = "istio-inject"
	IstioInitializerConfigMapKey  = "config"
	IstioAppLabel                 = "app"
	ServiceProtocolUDP            = "UDP"
	initializerDisabled           = "configmaps \"" +
		IstioInitializerConfigMap + "\" not found"
	initializerDisabledSummary = "Istio initializer is not configured." +
		" Enable initializer and automatic sidecar injection to use "
	kubernetesServiceName = "kubernetes"
)

// Following types are taken from
// https://github.com/istio/istio/blob/master/pilot/platform/kube/inject/inject.go

// InjectionPolicy determines the policy for injecting the
// sidecar proxy into the watched namespace(s).
type InjectionPolicy string

// Params describes configurable parameters for injecting istio proxy
// into kubernetes resource.
type Params struct {
	InitImage       string                   `json:"initImage"`
	ProxyImage      string                   `json:"proxyImage"`
	Verbosity       int                      `json:"verbosity"`
	SidecarProxyUID int64                    `json:"sidecarProxyUID"`
	Version         string                   `json:"version"`
	EnableCoreDump  bool                     `json:"enableCoreDump"`
	DebugMode       bool                     `json:"debugMode"`
	Mesh            *meshv1alpha1.MeshConfig `json:"-"`
	ImagePullPolicy string                   `json:"imagePullPolicy"`
	// Comma separated list of IP ranges in CIDR form. If set, only
	// redirect outbound traffic to Envoy for these IP
	// ranges. Otherwise all outbound traffic is redirected to Envoy.
	IncludeIPRanges string `json:"includeIPRanges"`
}

// IstioInjectConfig describes the configuration for Istio Inject initializer
type IstioInjectConfig struct {
	Policy InjectionPolicy `json:"policy"`

	// deprecate if InitializerConfiguration becomes namespace aware
	IncludeNamespaces []string `json:"namespaces"`

	// deprecate if InitializerConfiguration becomes namespace aware
	ExcludeNamespaces []string `json:"excludeNamespaces"`

	// Params specifies the parameters of the injected sidcar template
	Params Params `json:"params"`

	// InitializerName specifies the name of the initializer.
	InitializerName string `json:"initializerName"`
}

var istioSupportedServicePrefix = []string{
	"http", "http-",
	"http2", "http2-",
	"grpc", "grpc-",
	"mongo", "mongo-",
	"redis", "redis-",
	"tcp", "tcp-"}

var defaultExemptedNamespaces = map[string]bool{
	"kube-system":  true,
	"kube-public":  true,
	"istio-system": true}

// DefaultExemptedNamespaces returns list of default Namsepaces which are
// exempted from automatic sidecar injection.
// List includes "kube-system", "kube-public" and "istio-system"
func DefaultExemptedNamespaces() []string {
	s := make([]string, len(defaultExemptedNamespaces))
	i := 0
	for k := range defaultExemptedNamespaces {
		s[i] = k
		i++
	}
	return s
}

// ExemptedNamespace checks if a Namespace is by default exempted from automatic
// sidecar injection.
func ExemptedNamespace(ns string) bool {
	return defaultExemptedNamespaces[ns]
}

// GetInitializerConfig retrieves the Istio Initializer config.
// Istio Initializer config is stored as "istio-inject" configmap in
// "istio-system" Namespace.
func GetInitializerConfig(cmLister v1.ConfigMapLister) (*IstioInjectConfig, error) {
	cm, err := cmLister.ConfigMaps(IstioNamespace).Get(IstioInitializerConfigMap)
	if err != nil {
		glog.V(2).Infof("Failed to retrieve configmap: %s error: %s", IstioInitializerConfigMap, err)
		return nil, err
	}
	d, e := cm.Data[IstioInitializerConfigMapKey]
	if !e {
		errStr := fmt.Sprintf("Missing configuration map key: %s in configmap: %s", IstioInitializerConfigMapKey, IstioInitializerConfigMap)
		glog.Errorf(errStr)
		return nil, errors.New(errStr)
	}
	var cfg IstioInjectConfig
	if err := yaml.Unmarshal([]byte(d), &cfg); err != nil {
		glog.Errorf("Failed to parse yaml initializer config: %s", err)
		return nil, err
	}
	return &cfg, nil
}

// IstioInitializerDisabledNote generates an INFO note if the error string
// contains "istio-inject configmap not found".
func IstioInitializerDisabledNote(e, vetterID, vetterType string) *apiv1.Note {
	if strings.Contains(e, initializerDisabled) {
		return &apiv1.Note{
			Type:    vetterType,
			Summary: initializerDisabledSummary + "\"" + vetterID + "\" vetter.",
			Level:   apiv1.NoteLevel_INFO}
	}
	return nil
}

// ServicePortPrefixed checks if the Service port name is prefixed with Istio
// supported protocols.
func ServicePortPrefixed(n string) bool {
	i := 0
	for i < len(istioSupportedServicePrefix) {
		if n == istioSupportedServicePrefix[i] || strings.HasPrefix(n, istioSupportedServicePrefix[i+1]) {
			return true
		}
		i += 2
	}
	return false
}

// SidecarInjected checks if sidecar is injected in a Pod.
// Sidecar is considered injected if initializer annotation and proxy container
// are both present in the Pod Spec.
func SidecarInjected(p *corev1.Pod) bool {
	if _, ok := p.Annotations[IstioInitializerPodAnnotation]; !ok {
		return false
	}
	cList := p.Spec.Containers
	for _, c := range cList {
		if c.Name == IstioProxyContainerName {
			return true
		}
	}
	return false
}

func imageFromContainers(n string, cList []corev1.Container) (string, error) {
	for _, c := range cList {
		if c.Name == n {
			return c.Image, nil
		}
	}
	errStr := fmt.Sprintf("Failed to find container %s", n)
	glog.Error(errStr)
	return "", errors.New(errStr)
}

// Image returns the image for the container named n if present
// in the pod spec, or an error otherwise.
func Image(n string, s corev1.PodSpec) (string, error) {
	return imageFromContainers(n, s.Containers)
}

// InitImage returns the image for the init container named n if present
// in the pod spec, or an error otherwise.
func InitImage(n string, s corev1.PodSpec) (string, error) {
	return imageFromContainers(n, s.InitContainers)
}

func existsInStringSlice(e string, list []string) bool {
	for i := range list {
		if e == list[i] {
			return true
		}
	}
	return false
}

// ListNamespacesInMesh returns the list of Namespaces in the mesh.
// Inspects the Istio Initializer(istio-inject) configmap to enumerate
// Namespaces included/excluded from the mesh.
func ListNamespacesInMesh(nsLister v1.NamespaceLister, cmLister v1.ConfigMapLister) ([]*corev1.Namespace, error) {
	namespaces := []*corev1.Namespace{}
	ns, err := nsLister.List(labels.Everything())
	if err != nil {
		glog.Error("Failed to retrieve namespaces: ", err)
		return nil, err
	}
	cfg, err := GetInitializerConfig(cmLister)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		if ExemptedNamespace(n.Name) == true {
			continue
		}
		if cfg.ExcludeNamespaces != nil && len(cfg.ExcludeNamespaces) > 0 {
			excluded := existsInStringSlice(n.Name, cfg.ExcludeNamespaces)
			if excluded == true {
				continue
			}
		}
		if cfg.IncludeNamespaces != nil && len(cfg.IncludeNamespaces) > 0 {
			included := existsInStringSlice(corev1.NamespaceAll, cfg.IncludeNamespaces) ||
				existsInStringSlice(n.Name, cfg.IncludeNamespaces)
			if included == false {
				continue
			}
		}
		namespaces = append(namespaces, n)
	}
	return namespaces, nil
}

// ListPodsInMesh returns the list of Pods in the mesh.
// Pods in Namespaces returned by ListNamespacesInMesh with sidecar
// injected as determined by SidecarInjected are considered in the mesh.
func ListPodsInMesh(nsLister v1.NamespaceLister, cmLister v1.ConfigMapLister, podLister v1.PodLister) ([]*corev1.Pod, error) {
	pods := []*corev1.Pod{}
	ns, err := ListNamespacesInMesh(nsLister, cmLister)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		podList, err := podLister.Pods(n.Name).List(labels.Everything())
		if err != nil {
			glog.Errorf("Failed to retrieve pods for namespace: %s error: %s", n.Name, err)
			return nil, err
		}
		for _, p := range podList {
			if SidecarInjected(p) == true {
				pods = append(pods, p)
			}
		}
	}
	return pods, nil
}

// ListServicesInMesh returns the list of Services in the mesh.
// Services in Namespaces returned by ListNamespacesInMesh are considered in the mesh.
func ListServicesInMesh(nsLister v1.NamespaceLister, cmLister v1.ConfigMapLister, svcLister v1.ServiceLister) ([]*corev1.Service, error) {
	services := []*corev1.Service{}
	ns, err := ListNamespacesInMesh(nsLister, cmLister)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		serviceList, err := svcLister.Services(n.Name).List(labels.Everything())
		if err != nil {
			glog.Errorf("Failed to retrieve services for namespace: %s error: %s", n.Name, err)
			return nil, err
		}
		for _, s := range serviceList {
			if s.Name != "kubernetes" {
				services = append(services, s)
			}
		}
	}
	return services, nil
}

// ListEndpointsInMesh returns the list of Endpoints in the mesh.
// Endpoints in Namespaces returned by ListNamespacesInMesh are considered in the mesh.
func ListEndpointsInMesh(nsLister v1.NamespaceLister, cmLister v1.ConfigMapLister, epLister v1.EndpointsLister) ([]*corev1.Endpoints, error) {
	endpoints := []*corev1.Endpoints{}
	ns, err := ListNamespacesInMesh(nsLister, cmLister)
	if err != nil {
		return nil, err
	}
	for _, n := range ns {
		endpointList, err := epLister.Endpoints(n.Name).List(labels.Everything())
		if err != nil {
			glog.Errorf("Failed to retrieve endpoints for namespace: %s error: %s", n.Name, err)
			return nil, err
		}
		for _, s := range endpointList {
			if s.Name != kubernetesServiceName {
				endpoints = append(endpoints, s)
			}
		}
	}
	return endpoints, nil
}

// ComputeID returns MD5 checksum of the Note struct which can be used as
// ID for the note.
func ComputeID(n *apiv1.Note) string {
	return fmt.Sprintf("%x", structhash.Md5(n, 1))
}
