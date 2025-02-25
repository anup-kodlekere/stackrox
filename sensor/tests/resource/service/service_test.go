package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stackrox/rox/generated/internalapi/central"
	"github.com/stackrox/rox/generated/storage"
	"github.com/stackrox/rox/pkg/utils"
	"github.com/stackrox/rox/sensor/tests/resource"
	"github.com/stackrox/rox/sensor/testutils"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/e2e-framework/klient/k8s"
)

const (
	nginxDeploymentName    string = "nginx-deployment"
	nginxPodName           string = "nginx-rogue"
	servicePolicyName      string = "test-service"
	serviceNodePortFmt     string = "nginx-service-node-port-%d"
	serviceLoadBalancerFmt string = "nginx-service-load-balancer-%d"
)

var (
	NginxDeployment       = resource.K8sResourceInfo{Kind: "Deployment", YamlFile: "nginx.yaml"}
	NginxPod              = resource.K8sResourceInfo{Kind: "Pod", YamlFile: "nginx-pod.yaml"}
	NginxServiceClusterIP = resource.K8sResourceInfo{Kind: "Service", YamlFile: "nginx-service-cluster-ip.yaml"}
)

func checkAlert(alert *storage.Alert, result *central.AlertResults) error {
	for _, actualAlert := range result.GetAlerts() {
		if alert.GetPolicy().GetName() == actualAlert.GetPolicy().GetName() &&
			alert.GetState() == actualAlert.GetState() {
			return nil
		}
	}
	return errors.Errorf("Alert '%s' was not found", alert.GetPolicy().GetName())
}

func assertAlertTriggered(alert *storage.Alert) resource.AlertAssertFunc {
	return func(results *central.AlertResults) error {
		return checkAlert(alert, results)
	}
}

func assertAlertNotTriggered(alert *storage.Alert) resource.AlertAssertFunc {
	return func(results *central.AlertResults) error {
		if err := checkAlert(alert, results); err != nil {
			return nil
		}
		return errors.Errorf("alert '%s' should not be triggered", alert.GetPolicy().GetName())
	}
}

func checkPortConfig(deployment *storage.Deployment, ports []*storage.PortConfig) error {
	for _, expectedPort := range ports {
		foundPortConfig := false
		for _, port := range deployment.GetPorts() {
			if expectedPort.GetProtocol() == port.GetProtocol() &&
				expectedPort.GetContainerPort() == port.GetContainerPort() &&
				expectedPort.GetExposure() == port.GetExposure() {
				if len(expectedPort.GetExposureInfos()) != len(port.GetExposureInfos()) {
					continue
				}
				for _, expectedPortInfo := range expectedPort.GetExposureInfos() {
					foundPortInfo := false
					for _, portInfo := range port.GetExposureInfos() {
						if expectedPortInfo.GetServiceName() == portInfo.GetServiceName() {
							if expectedPortInfo.GetNodePort() != portInfo.GetNodePort() {
								return errors.Errorf("expected NodePort '%d' actual NodePort '%d'", expectedPortInfo.GetNodePort(), portInfo.GetNodePort())
							}
							if expectedPortInfo.GetServicePort() != portInfo.GetServicePort() {
								return errors.Errorf("expected ServicePort '%d' actual ServicePort '%d'", expectedPortInfo.GetServicePort(), portInfo.GetServicePort())
							}
							if expectedPortInfo.GetLevel() != portInfo.GetLevel() {
								return errors.Errorf("expected Level '%d' actual Level '%d'", expectedPortInfo.GetLevel(), portInfo.GetLevel())
							}
							foundPortInfo = true
						}
					}
					if !foundPortInfo {
						return errors.Errorf("PortInfo '%+v' not found", expectedPortInfo)
					}
				}
				foundPortConfig = true
			}
		}
		if !foundPortConfig {
			return errors.Errorf("PortConfig '%+v' not found", expectedPort)
		}
	}
	return nil
}

func assertLastDeploymentHasPortExposure(ports []*storage.PortConfig) resource.AssertFunc {
	return func(deployment *storage.Deployment, _ central.ResourceAction) error {
		return checkPortConfig(deployment, ports)
	}
}

func assertLastDeploymentMissingPortExposure(ports []*storage.PortConfig) resource.AssertFunc {
	return func(deployment *storage.Deployment, _ central.ResourceAction) error {
		if err := checkPortConfig(deployment, ports); err != nil {
			return nil
		}
		return errors.Errorf("PortConfig '%+v' should not be present", ports)
	}
}

type DeploymentExposureSuite struct {
	testContext *resource.TestContext
	suite.Suite
}

func Test_DeploymentExposure(t *testing.T) {
	suite.Run(t, new(DeploymentExposureSuite))
}

var _ suite.SetupAllSuite = &DeploymentExposureSuite{}
var _ suite.TearDownTestSuite = &DeploymentExposureSuite{}

func (s *DeploymentExposureSuite) TearDownTest() {
	// Clear any messages received in fake central during the test run
	s.testContext.GetFakeCentral().ClearReceivedBuffer()
}

func (s *DeploymentExposureSuite) SetupSuite() {
	policies, err := testutils.GetPoliciesFromFile("data/policies.json")
	if err != nil {
		log.Fatalln(err)
	}
	config := resource.CentralConfig{
		InitialSystemPolicies: policies,
	}
	if testContext, err := resource.NewContextWithConfig(s.T(), config); err != nil {
		s.Fail("failed to setup test context: %s", err)
	} else {
		s.testContext = testContext
	}
}

func (s *DeploymentExposureSuite) Test_ClusterIpPermutation() {
	s.testContext.RunTest(
		resource.WithResources([]resource.K8sResourceInfo{
			NginxDeployment,
			NginxServiceClusterIP,
		}),
		resource.WithPermutation(),
		resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
			// Test context already takes care of creating and destroying resources
			testC.LastDeploymentState(nginxDeploymentName,
				assertLastDeploymentHasPortExposure([]*storage.PortConfig{
					{
						Protocol:      "TCP",
						ContainerPort: 9376,
						Exposure:      storage.PortConfig_INTERNAL,
						ExposureInfos: []*storage.PortConfig_ExposureInfo{
							{
								ServiceName: "nginx-svc-cluster-ip",
								ServicePort: 80,
								Level:       storage.PortConfig_INTERNAL,
							},
						},
					},
				},
				),
				"'PortConfig' for Cluster IP service test not found",
			)
			testC.LastViolationState(nginxDeploymentName,
				assertAlertNotTriggered(
					&storage.Alert{
						Policy: &storage.Policy{
							Name: servicePolicyName,
						},
						State: storage.ViolationState_ACTIVE,
					},
				),
				fmt.Sprintf("Alert '%s' should not be triggered", servicePolicyName))
			testC.GetFakeCentral().ClearReceivedBuffer()
		}),
	)
}

func (s *DeploymentExposureSuite) Test_NodePortPermutation() {
	// We need to use different ports in each NodePort/LoadBalancer test otherwise k8s could throw an error when the service is being created (provided port is already allocated).
	// Waiting for the resources to get Deleted is not enough, k8s reports that the resource has been deleted but on creation sometimes we still get the same error.
	// Adding retries on creation helped a lot, but it's still not enough.
	cases := []struct {
		orderedResources []resource.K8sResourceInfo
		portConfig       []*storage.PortConfig
		selector         map[string]string
	}{
		{
			orderedResources: []resource.K8sResourceInfo{
				NginxDeployment,
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
		{
			orderedResources: []resource.K8sResourceInfo{
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
				NginxDeployment,
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
	}

	for _, c := range cases {
		setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceNodePortFmt, getPort(s.T()), c.selector, setNodePort, setPortConfigNode)
		s.testContext.RunTest(
			resource.WithResources(c.orderedResources),
			resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
				// Test context already takes care of creating and destroying resources
				testC.LastDeploymentState(nginxDeploymentName,
					assertLastDeploymentHasPortExposure(c.portConfig), "'PortConfig' for Node Port service test not found")
				testC.LastViolationState(nginxDeploymentName,
					assertAlertTriggered(
						&storage.Alert{
							Policy: &storage.Policy{
								Name: servicePolicyName,
							},
							State: storage.ViolationState_ACTIVE,
						},
					),
					fmt.Sprintf("Alert '%s' should be triggered", servicePolicyName))
				testC.GetFakeCentral().ClearReceivedBuffer()
			}),
			resource.WithRetryCallback(func(err error, obj k8s.Object) error {
				// Only checking services
				if _, ok := obj.(*v1.Service); !ok {
					return nil
				}
				// If the error is different from "provided port is already allocated" we fail the test
				if !strings.Contains(err.Error(), "provided port is already allocated") {
					return err
				}
				setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceNodePortFmt, getPort(s.T()), c.selector, setNodePort, setPortConfigNode)
				return nil
			}),
		)
	}
}

func (s *DeploymentExposureSuite) Test_LoadBalancerPermutation() {
	// We need to use different ports in each NodePort/LoadBalancer test otherwise k8s could throw an error when the service is being created (provided port is already allocated).
	// Waiting for the resources to get Deleted is not enough, k8s reports that the resource has been deleted but on creation sometimes we still get the same error.
	// Adding retries on creation helped a lot, but it's still not enough.
	cases := []struct {
		orderedResources []resource.K8sResourceInfo
		portConfig       []*storage.PortConfig
		selector         map[string]string
	}{
		{
			orderedResources: []resource.K8sResourceInfo{
				NginxDeployment,
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
		{
			orderedResources: []resource.K8sResourceInfo{
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
				NginxDeployment,
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
	}

	for _, c := range cases {
		setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceLoadBalancerFmt, getPort(s.T()), c.selector, setLoadBalancer, setPortConfigExternal)
		s.testContext.RunTest(
			resource.WithResources(c.orderedResources),
			resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
				// Test context already takes care of creating and destroying resources
				testC.LastDeploymentState(nginxDeploymentName,
					assertLastDeploymentHasPortExposure(c.portConfig), "'PortConfig' for Node Port service test not found")
				testC.LastViolationState(nginxDeploymentName,
					assertAlertTriggered(
						&storage.Alert{
							Policy: &storage.Policy{
								Name: servicePolicyName,
							},
							State: storage.ViolationState_ACTIVE,
						},
					),
					fmt.Sprintf("Alert '%s' should be triggered", servicePolicyName))
				testC.GetFakeCentral().ClearReceivedBuffer()
			}),
			resource.WithRetryCallback(func(err error, obj k8s.Object) error {
				// Only checking services
				if _, ok := obj.(*v1.Service); !ok {
					return nil
				}
				// If the error is different from "provided port is already allocated" we fail the test
				if !strings.Contains(err.Error(), "provided port is already allocated") {
					return err
				}
				setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceLoadBalancerFmt, getPort(s.T()), c.selector, setLoadBalancer, setPortConfigExternal)
				return nil
			}),
		)
	}
}

func (s *DeploymentExposureSuite) Test_NoExposure() {
	s.testContext.RunTest(
		resource.WithResources([]resource.K8sResourceInfo{
			NginxDeployment,
		}),
		resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
			// Test context already takes care of creating and destroying resources
			testC.LastDeploymentState(nginxDeploymentName,
				assertLastDeploymentHasPortExposure([]*storage.PortConfig{
					{
						Protocol:      "TCP",
						ContainerPort: 80,
						Exposure:      0,
					},
				},
				),
				"PortConfig",
			)
			testC.LastViolationState(nginxDeploymentName,
				assertAlertNotTriggered(
					&storage.Alert{
						Policy: &storage.Policy{
							Name: servicePolicyName,
						},
						State: storage.ViolationState_ACTIVE,
					},
				),
				fmt.Sprintf("Alert '%s' should not be triggered", servicePolicyName))
			testC.GetFakeCentral().ClearReceivedBuffer()
		}),
	)
}

func (s *DeploymentExposureSuite) Test_MultipleDeploymentUpdates() {
	// We need to use different ports in each NodePort/LoadBalancer test otherwise k8s could throw an error when the service is being created (provided port is already allocated).
	// Waiting for the resources to get Deleted is not enough, k8s reports that the resource has been deleted but on creation sometimes we still get the same error.
	// Adding retries on creation helped a lot, but it's still not enough.
	s.testContext.RunTest(
		resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
			deleteDep, err := testC.ApplyResourceNoObject(context.Background(), resource.DefaultNamespace, NginxDeployment, nil)
			defer utils.IgnoreError(deleteDep)
			require.NoError(t, err)

			port := getPort(t)
			svc := &v1.Service{}
			sel := map[string]string{
				"app": "nginx",
			}
			nginxServiceNodePort := resource.K8sResourceInfo{
				Kind: "Service",
				Obj:  svc,
			}
			setDynamicFields(svc, serviceNodePortFmt, port, sel, setNodePort)

			deleteService, err := testC.ApplyResourceNoObject(context.Background(), resource.DefaultNamespace, nginxServiceNodePort,
				func(err error, obj k8s.Object) error {
					// Only checking services
					if _, ok := obj.(*v1.Service); !ok {
						return nil
					}
					// If the error is different from "provided port is already allocated" we fail the test
					if !strings.Contains(err.Error(), "provided port is already allocated") {
						return err
					}
					port = getPort(t)
					setDynamicFields(svc, serviceNodePortFmt, port, sel, setNodePort)
					return nil
				})
			require.NoError(t, err)

			testC.LastDeploymentState(nginxDeploymentName,
				assertLastDeploymentHasPortExposure([]*storage.PortConfig{
					{
						Protocol:      "TCP",
						ContainerPort: 80,
						Exposure:      storage.PortConfig_NODE,
						ExposureInfos: []*storage.PortConfig_ExposureInfo{
							{
								ServiceName: fmt.Sprintf(serviceNodePortFmt, port),
								ServicePort: 80,
								NodePort:    port,
								Level:       storage.PortConfig_NODE,
							},
						},
					},
				},
				),
				"'PortConfig' for Multiple Deployment Updates test not found",
			)
			testC.LastViolationState(nginxDeploymentName,
				assertAlertTriggered(
					&storage.Alert{
						Policy: &storage.Policy{
							Name: servicePolicyName,
						},
						State: storage.ViolationState_ACTIVE,
					},
				),
				fmt.Sprintf("Alert '%s' should be triggered", servicePolicyName))
			testC.GetFakeCentral().ClearReceivedBuffer()

			require.NoError(t, deleteService())

			testC.LastDeploymentState(nginxDeploymentName,
				assertLastDeploymentMissingPortExposure([]*storage.PortConfig{
					{
						Protocol:      "TCP",
						ContainerPort: 80,
						Exposure:      storage.PortConfig_NODE,
						ExposureInfos: []*storage.PortConfig_ExposureInfo{
							{
								ServiceName: fmt.Sprintf(serviceNodePortFmt, port),
								ServicePort: 80,
								NodePort:    port,
								Level:       storage.PortConfig_NODE,
							},
						},
					},
				},
				),
				"'PortConfig' for Multiple Deployment Updates test found",
			)
			testC.LastViolationState(nginxDeploymentName,
				assertAlertNotTriggered(
					&storage.Alert{
						Policy: &storage.Policy{
							Name: servicePolicyName,
						},
						State: storage.ViolationState_RESOLVED,
					},
				),
				fmt.Sprintf("Alert '%s' should not be triggered", servicePolicyName))
			testC.GetFakeCentral().ClearReceivedBuffer()
		}),
	)
}

func (s *DeploymentExposureSuite) Test_NodePortPermutationWithPod() {
	// We need to use different ports in each NodePort/LoadBalancer test otherwise k8s could throw an error when the service is being created (provided port is already allocated).
	// Waiting for the resources to get Deleted is not enough, k8s reports that the resource has been deleted but on creation sometimes we still get the same error.
	// Adding retries on creation helped a lot, but it's still not enough.
	cases := []struct {
		orderedResources []resource.K8sResourceInfo
		portConfig       []*storage.PortConfig
		selector         map[string]string
	}{
		{
			orderedResources: []resource.K8sResourceInfo{
				NginxPod,
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
		{
			orderedResources: []resource.K8sResourceInfo{
				{
					Kind: "Service",
					Obj:  &v1.Service{},
				},
				NginxPod,
			},
			portConfig: []*storage.PortConfig{{}},
			selector: map[string]string{
				"app": "nginx",
			},
		},
	}

	for _, c := range cases {
		setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceNodePortFmt, getPort(s.T()), c.selector, setNodePort, setPortConfigNode)
		s.testContext.RunTest(
			resource.WithResources(c.orderedResources),
			resource.WithTestCase(func(t *testing.T, testC *resource.TestContext, _ map[string]k8s.Object) {
				// Test context already takes care of creating and destroying resources
				testC.LastDeploymentState(nginxPodName,
					assertLastDeploymentHasPortExposure(c.portConfig), "'PortConfig' for Node Port service test not found")
				testC.LastViolationState(nginxPodName,
					assertAlertTriggered(
						&storage.Alert{
							Policy: &storage.Policy{
								Name: servicePolicyName,
							},
							State: storage.ViolationState_ACTIVE,
						},
					),
					fmt.Sprintf("Alert '%s' should be triggered", servicePolicyName))
				testC.GetFakeCentral().ClearReceivedBuffer()
			}),
			resource.WithRetryCallback(func(err error, obj k8s.Object) error {
				// Only checking services
				if _, ok := obj.(*v1.Service); !ok {
					return nil
				}
				// If the error is different from "provided port is already allocated" we fail the test
				if !strings.Contains(err.Error(), "provided port is already allocated") {
					return err
				}
				setDynamicFieldsInSlice(c.orderedResources, c.portConfig, serviceNodePortFmt, getPort(s.T()), c.selector, setNodePort, setPortConfigNode)
				return nil
			}),
		)
	}
}

var nextPort int32 = 30000

const (
	MaxPort = 30100
)

func getPort(t *testing.T) int32 {
	if nextPort > MaxPort {
		// If we reached the maximum usable port we fail the test
		t.Fatalf("Reached maximum usable port:\nMaxPort = %d, current port = %d", MaxPort, nextPort)
	}
	ret := nextPort
	nextPort++
	return ret
}

type serviceFunc func(*v1.Service, string, int32, map[string]string)

type portConfigFunc func([]*storage.PortConfig, string, int32)

func setNodePort(svc *v1.Service, name string, port int32, sel map[string]string) {
	svc.ObjectMeta = metav1.ObjectMeta{
		Name:      fmt.Sprintf(name, port),
		Namespace: resource.DefaultNamespace,
	}
	svc.Spec = v1.ServiceSpec{
		Type:     v1.ServiceTypeNodePort,
		Selector: sel,
		Ports: []v1.ServicePort{
			{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 80,
				},
				NodePort: port,
			},
		},
	}
}

func setLoadBalancer(svc *v1.Service, name string, port int32, sel map[string]string) {
	svc.ObjectMeta = metav1.ObjectMeta{
		Name:      fmt.Sprintf(name, port),
		Namespace: resource.DefaultNamespace,
	}
	svc.Spec = v1.ServiceSpec{
		Type:     v1.ServiceTypeLoadBalancer,
		Selector: sel,
		Ports: []v1.ServicePort{
			{
				Port: 80,
				TargetPort: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 80,
				},
				NodePort: port,
			},
		},
	}
}

func setPortConfigExternal(portConfig []*storage.PortConfig, name string, port int32) {
	if len(portConfig) != 1 {
		return
	}
	portConfig[0] = &storage.PortConfig{
		Protocol:      "TCP",
		ContainerPort: 80,
		Exposure:      storage.PortConfig_EXTERNAL,
		ExposureInfos: []*storage.PortConfig_ExposureInfo{
			{
				ServiceName: fmt.Sprintf(name, port),
				ServicePort: 80,
				NodePort:    port,
				Level:       storage.PortConfig_EXTERNAL,
			},
		},
	}
}

func setPortConfigNode(portConfig []*storage.PortConfig, name string, port int32) {
	if len(portConfig) != 1 {
		return
	}
	portConfig[0] = &storage.PortConfig{
		Protocol:      "TCP",
		ContainerPort: 80,
		Exposure:      storage.PortConfig_NODE,
		ExposureInfos: []*storage.PortConfig_ExposureInfo{
			{
				ServiceName: fmt.Sprintf(name, port),
				ServicePort: 80,
				NodePort:    port,
				Level:       storage.PortConfig_NODE,
			},
		},
	}
}

func setDynamicFields(svc *v1.Service, name string, port int32, sel map[string]string, serviceFn func(*v1.Service, string, int32, map[string]string)) {
	serviceFn(svc, name, port, sel)
}

func setDynamicFieldsInSlice(resources []resource.K8sResourceInfo, portConfig []*storage.PortConfig, name string, port int32, sel map[string]string, serviceFn serviceFunc, portConfigFn portConfigFunc) {
	for i := range resources {
		if resources[i].Kind == "Service" {
			setDynamicFields(resources[i].Obj.(*v1.Service), name, port, sel, serviceFn)
		}
	}
	portConfigFn(portConfig, name, port)
}
